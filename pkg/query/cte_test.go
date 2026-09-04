package query

import (
	"context"
	"sort"
	"testing"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

// seedUsersAndTest creates the two physical tables the multi-CTE fixtures
// below join against: USERS (id, name) and TEST (user_id), with data chosen
// so every branch of the final CASE — no tests, one test, several tests — is
// exercised by at least one user.
func seedUsersAndTest(t *testing.T, executor *Executor, repo *metadata.Repository, executionContext ExecutionContext) {
	t.Helper()
	ctx := context.Background()

	database, err := repo.GetDatabaseByName(ctx, executionContext.Database)
	if err != nil {
		t.Fatalf("GetDatabaseByName() error = %v", err)
	}
	schema, err := repo.GetSchemaByName(ctx, database.ID, executionContext.Schema)
	if err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}

	if _, err := repo.CreateTable(ctx, schema.ID, "USERS",
		[]metadata.ColumnDef{{Name: "ID", Type: "INTEGER"}, {Name: "NAME", Type: "TEXT"}}, ""); err != nil {
		t.Fatalf("CreateTable(USERS) error = %v", err)
	}
	if _, err := repo.CreateTable(ctx, schema.ID, "TEST",
		[]metadata.ColumnDef{{Name: "USER_ID", Type: "INTEGER", Nullable: true}}, ""); err != nil {
		t.Fatalf("CreateTable(TEST) error = %v", err)
	}

	for _, statement := range []string{
		"INSERT INTO users VALUES (1, 'Alice')", // no tests
		"INSERT INTO users VALUES (2, 'Bob')",   // one test
		"INSERT INTO users VALUES (3, 'Carol')", // several tests
		"INSERT INTO test (user_id) VALUES (2)",
		"INSERT INTO test (user_id) VALUES (3)",
		"INSERT INTO test (user_id) VALUES (3)",
		"INSERT INTO test (user_id) VALUES (3)",
		"INSERT INTO test (user_id) VALUES (NULL)", // must be excluded by WHERE user_id IS NOT NULL
	} {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
			t.Fatalf("seed %q error = %v", statement, err)
		}
	}
}

// TestStandaloneCTEWithTranslatedFunctions runs a bare WITH ... SELECT — no
// CREATE involved at all — whose body uses three functions that need
// translation (IFF, NVL, DATEADD). This is the query that first surfaced the
// bug: the CTE's own alias "test" was being mistaken for the physical table
// of the same name and qualified to TEST_DB.PUBLIC_TEST, which does not
// exist as the CTE's target.
func TestStandaloneCTEWithTranslatedFunctions(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	if _, err := repo.CreateDatabase(ctx, "TEST_DB", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}

	sql := `with test as (
SELECT
    IFF(1 > 0, 'yes', 'no')          AS iff_translates,
    NVL(NULL, 'fallback')            AS nvl_translates,
    DATEADD(day, 30, CURRENT_DATE)   AS dateadd_translates
) select * from test;`

	result, err := executor.QueryWithContext(ctx, executionContext, sql)
	if err != nil {
		t.Fatalf("QueryWithContext() error = %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	row := result.Rows[0]
	if !procedureValuesEqual(row[0], "yes") {
		t.Errorf("iff_translates = %v, want yes", row[0])
	}
	if !procedureValuesEqual(row[1], "fallback") {
		t.Errorf("nvl_translates = %v, want fallback", row[1])
	}
	if row[2] == nil {
		t.Error("dateadd_translates should not be NULL")
	}
}

// TestTrivialCTEWithNoFunctionsExecutes isolates the CTE-aliasing bug from
// function translation: even a CTE with nothing in it worth translating used
// to fail, because the alias itself was qualified as though it were a table.
func TestTrivialCTEWithNoFunctionsExecutes(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	if _, err := repo.CreateDatabase(ctx, "TEST_DB", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}

	result, err := executor.QueryWithContext(ctx, executionContext, "WITH test AS (SELECT 1 AS x) SELECT * FROM test")
	if err != nil {
		t.Fatalf("QueryWithContext() error = %v", err)
	}
	if len(result.Rows) != 1 || !procedureValuesEqual(result.Rows[0][0], 1) {
		t.Fatalf("rows = %#v, want a single row of 1", result.Rows)
	}
}

// TestCTASTranslatesFunctionsInItsBody runs CREATE TABLE ... AS (<query>)
// with the same three functions, and then reads the table back — proving not
// just that creation succeeds, but that the translated values were the ones
// actually stored.
func TestCTASTranslatesFunctionsInItsBody(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	if _, err := repo.CreateDatabase(ctx, "TEST_DB", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}

	createSQL := `create table test as (
SELECT
    IFF(1 > 0, 'yes', 'no')          AS iff_translates,
    NVL(NULL, 'fallback')            AS nvl_translates,
    DATEADD(day, 30, CURRENT_DATE)   AS dateadd_translates
)`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, createSQL); err != nil {
		t.Fatalf("ExecuteWithContext(CTAS) error = %v", err)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM test")
	if err != nil {
		t.Fatalf("QueryWithContext(readback) error = %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	row := result.Rows[0]
	if !procedureValuesEqual(row[0], "yes") {
		t.Errorf("iff_translates = %v, want yes", row[0])
	}
	if !procedureValuesEqual(row[1], "fallback") {
		t.Errorf("nvl_translates = %v, want fallback", row[1])
	}
}

// summaryRow is one row of user_test_summary, read back sorted by user_id so
// the assertions do not depend on DuckDB's row order.
type summaryRow struct {
	userID    int64
	userName  string
	testCount int64
	status    string
}

func readSummaryRows(t *testing.T, executor *Executor, executionContext ExecutionContext) []summaryRow {
	t.Helper()
	result, err := executor.QueryWithContext(context.Background(), executionContext,
		"SELECT user_id, user_name, test_count, test_status FROM user_test_summary")
	if err != nil {
		t.Fatalf("QueryWithContext(readback) error = %v", err)
	}

	rows := make([]summaryRow, 0, len(result.Rows))
	for _, r := range result.Rows {
		rows = append(rows, summaryRow{
			userID:    toInt64(r[0]),
			userName:  fmtString(r[1]),
			testCount: toInt64(r[2]),
			status:    fmtString(r[3]),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].userID < rows[j].userID })
	return rows
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	default:
		return -1
	}
}

func fmtString(v any) string {
	s, _ := v.(string)
	return s
}

func assertSummaryRows(t *testing.T, got []summaryRow) {
	t.Helper()
	want := []summaryRow{
		{userID: 1, userName: "Alice", testCount: 0, status: "NO_TESTS"},
		{userID: 2, userName: "Bob", testCount: 1, status: "ONE_TEST"},
		{userID: 3, userName: "Carol", testCount: 3, status: "MULTIPLE_TESTS"},
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

// TestCreateTemporaryTableWithMultipleCTEsParenthesized runs the user's own
// multi-CTE query — three chained CTEs joining two physical tables, a LEFT
// JOIN, COALESCE and a CASE expression — as a CREATE TEMPORARY TABLE ...
// AS (WITH ... SELECT ...). This is the scenario that exercises every part
// of the fix at once: the CTE aliases (test_counts, user_details,
// user_status) must stay unqualified while the real tables (test, users)
// inside their bodies are qualified, and the TEMPORARY table itself must be
// created with the bare name DuckDB's own TEMP catalog requires.
func TestCreateTemporaryTableWithMultipleCTEsParenthesized(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	if _, err := repo.CreateDatabase(ctx, "TEST_DB", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}
	seedUsersAndTest(t, executor, repo, executionContext)

	createSQL := `CREATE TEMPORARY TABLE user_test_summary AS (
WITH test_counts AS (
    SELECT
        user_id,
        COUNT(*) AS test_count
    FROM test
    WHERE user_id IS NOT NULL
    GROUP BY user_id
),
user_details AS (
    SELECT
        u.id AS user_id,
        u.name AS user_name,
        COALESCE(tc.test_count, 0) AS test_count
    FROM users u
    LEFT JOIN test_counts tc
        ON u.id = tc.user_id
),
user_status AS (
    SELECT
        user_id,
        user_name,
        test_count,
        CASE
            WHEN test_count = 0 THEN 'NO_TESTS'
            WHEN test_count = 1 THEN 'ONE_TEST'
            ELSE 'MULTIPLE_TESTS'
        END AS test_status
    FROM user_details
)
SELECT
    user_id,
    user_name,
    test_count,
    test_status
FROM user_status);`

	if _, err := executor.ExecuteWithContext(ctx, executionContext, createSQL); err != nil {
		t.Fatalf("ExecuteWithContext(CTAS) error = %v", err)
	}

	assertSummaryRows(t, readSummaryRows(t, executor, executionContext))
}

// TestCreateTemporaryTableWithMultipleCTEsUnparenthesized is the same query
// without the outer parentheses around the WITH ... SELECT body — CREATE
// TEMPORARY TABLE ... AS WITH ... rather than ... AS (WITH ...) — which must
// behave identically.
func TestCreateTemporaryTableWithMultipleCTEsUnparenthesized(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	if _, err := repo.CreateDatabase(ctx, "TEST_DB", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}
	seedUsersAndTest(t, executor, repo, executionContext)

	createSQL := `CREATE TEMPORARY TABLE user_test_summary AS
WITH test_counts AS (
    SELECT
        user_id,
        COUNT(*) AS test_count
    FROM test
    WHERE user_id IS NOT NULL
    GROUP BY user_id
),
user_details AS (
    SELECT
        u.id AS user_id,
        u.name AS user_name,
        COALESCE(tc.test_count, 0) AS test_count
    FROM users u
    LEFT JOIN test_counts tc
        ON u.id = tc.user_id
),
user_status AS (
    SELECT
        user_id,
        user_name,
        test_count,
        CASE
            WHEN test_count = 0 THEN 'NO_TESTS'
            WHEN test_count = 1 THEN 'ONE_TEST'
            ELSE 'MULTIPLE_TESTS'
        END AS test_status
    FROM user_details
)
SELECT
    user_id,
    user_name,
    test_count,
    test_status
FROM user_status;`

	if _, err := executor.ExecuteWithContext(ctx, executionContext, createSQL); err != nil {
		t.Fatalf("ExecuteWithContext(CTAS) error = %v", err)
	}

	assertSummaryRows(t, readSummaryRows(t, executor, executionContext))
}

// TestTemporaryTableVisibleAcrossSeparateStatements proves the deeper reason
// the two tests above can pass at all: DuckDB scopes a TEMP table to the
// physical connection that created it, and database/sql pools connections by
// default. CREATE and the later SELECT are issued as two independent
// executor calls here — as two separate /api/v2/statements requests would be
// in the console — rather than sharing a pinned connection, which is exactly
// what silently lost the table under concurrent load before
// db.SetMaxOpenConns(1) was set (see setupTestExecutor and cmd/server/main.go).
func TestTemporaryTableVisibleAcrossSeparateStatements(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	if _, err := repo.CreateDatabase(ctx, "TEST_DB", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"CREATE TEMPORARY TABLE scratch AS SELECT 1 AS x"); err != nil {
		t.Fatalf("create temp table: %v", err)
	}

	// A separate call, not a continuation of the one above.
	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM scratch")
	if err != nil {
		t.Fatalf("read temp table in a later statement: %v", err)
	}
	if len(result.Rows) != 1 || !procedureValuesEqual(result.Rows[0][0], 1) {
		t.Fatalf("rows = %#v, want a single row of 1", result.Rows)
	}
}
