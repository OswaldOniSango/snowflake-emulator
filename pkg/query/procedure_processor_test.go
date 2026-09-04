package query

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestProcedureLifecycle(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "LESSON_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}

	createSQL := `CREATE PROCEDURE LESSON_DB.PUBLIC.GREET(NAME VARCHAR)
		RETURNS VARCHAR
		LANGUAGE SQL
		AS $$
		BEGIN
			RETURN 'Hello, ' || :NAME;
		END
		$$`
	if _, err := executor.Execute(ctx, createSQL); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}

	result, err := executor.Query(ctx, `CALL LESSON_DB.PUBLIC.GREET('Snowflake')`)
	if err != nil {
		t.Fatalf("CALL error = %v", err)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0] != "Hello, Snowflake" {
		t.Fatalf("CALL result = %#v, want Hello, Snowflake", result.Rows)
	}

	shown, err := executor.Query(ctx, "SHOW PROCEDURES")
	if err != nil {
		t.Fatalf("SHOW PROCEDURES error = %v", err)
	}
	if len(shown.Rows) != 1 || shown.Rows[0][1] != "GREET" {
		t.Fatalf("SHOW PROCEDURES result = %#v", shown.Rows)
	}

	if _, err := executor.Execute(ctx, "DROP PROCEDURE LESSON_DB.PUBLIC.GREET"); err != nil {
		t.Fatalf("DROP PROCEDURE error = %v", err)
	}
	if _, err := executor.Query(ctx, `CALL LESSON_DB.PUBLIC.GREET('again')`); err == nil {
		t.Fatal("CALL after DROP returned nil error")
	}
}

func TestProcedureExecutesMultipleStatements(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "LESSON_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	if _, err := executor.Execute(ctx, "CREATE TABLE procedure_log (message VARCHAR)"); err != nil {
		t.Fatalf("CREATE TABLE error = %v", err)
	}

	createSQL := `CREATE PROCEDURE LESSON_DB.PUBLIC.LOG_MESSAGE(MESSAGE VARCHAR)
		RETURNS VARCHAR LANGUAGE SQL AS $$
		BEGIN
			INSERT INTO procedure_log VALUES (:MESSAGE);
			RETURN :MESSAGE;
		END
		$$`
	if _, err := executor.Execute(ctx, createSQL); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}
	if _, err := executor.Query(ctx, `CALL LESSON_DB.PUBLIC.LOG_MESSAGE('saved')`); err != nil {
		t.Fatalf("CALL error = %v", err)
	}

	result, err := executor.Query(ctx, "SELECT message FROM procedure_log")
	if err != nil {
		t.Fatalf("SELECT error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "saved" {
		t.Fatalf("stored rows = %#v, want saved", result.Rows)
	}
}

func TestProcedureCreateAndCallWithTwoArguments(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "MATH_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}

	createSQL := `CREATE PROCEDURE MATH_DB.PUBLIC.ADD_NUMBERS(A INTEGER, B INTEGER)
		RETURNS INTEGER
		LANGUAGE SQL
		AS $$
		BEGIN
			RETURN :A + :B;
		END
		$$`
	if _, err := executor.Execute(ctx, createSQL); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}

	result, err := executor.Query(ctx, "CALL MATH_DB.PUBLIC.ADD_NUMBERS(20, 22)")
	if err != nil {
		t.Fatalf("CALL error = %v", err)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
		t.Fatalf("CALL returned %#v, want one row with one column", result.Rows)
	}
	if result.Rows[0][0] != int32(42) {
		t.Fatalf("CALL returned %#v, want 42", result.Rows[0][0])
	}
}

func TestProcedureSupportsDeclareAssignmentCaseAndIf(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "SCRIPTING_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "SCRIPTING_DB", Schema: "PUBLIC"}
	for _, statement := range []string{
		"CREATE TABLE source_table (id INTEGER)",
		"CREATE TABLE procedure_log (message VARCHAR)",
		"CREATE TABLE working_table (id INTEGER)",
		"INSERT INTO source_table VALUES (1), (2)",
	} {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
			t.Fatalf("setup statement %q error = %v", statement, err)
		}
	}

	createSQL := `CREATE PROCEDURE example(action VARCHAR)
		RETURNS VARCHAR
		LANGUAGE SQL
		AS $$
		DECLARE
			process_name VARCHAR DEFAULT 'example';
			row_count NUMBER;
		BEGIN
			-- Route execution according to the requested action.
			CASE (UPPER(action))
				WHEN 'START' THEN
					row_count := (
						SELECT COUNT(*) FROM source_table
					);
					RETURN 'START';
				WHEN 'END' THEN
					DROP TABLE IF EXISTS working_table;
					RETURN 'END';
				ELSE
					-- Scalar SELECT assignments populate procedure variables.
					row_count := (SELECT COUNT(*) FROM source_table);
					IF (row_count > 0) THEN
						INSERT INTO procedure_log VALUES (:process_name);
						RETURN 'OK';
					ELSE
						RETURN 'OK (no data)';
					END IF;
			END CASE;
		END
		$$`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, createSQL); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}

	tests := []struct {
		action string
		want   string
	}{
		{action: "start", want: "START"},
		{action: "PROCESS", want: "OK"},
		{action: "end", want: "END"},
	}
	for _, tt := range tests {
		result, err := executor.QueryWithContext(ctx, executionContext, "CALL example('"+tt.action+"')")
		if err != nil {
			t.Fatalf("CALL example(%q) error = %v", tt.action, err)
		}
		if len(result.Rows) != 1 || result.Rows[0][0] != tt.want {
			t.Fatalf("CALL example(%q) rows = %#v, want %q", tt.action, result.Rows, tt.want)
		}
	}

	logResult, err := executor.QueryWithContext(ctx, executionContext, "SELECT message FROM procedure_log")
	if err != nil {
		t.Fatalf("procedure_log SELECT error = %v", err)
	}
	if len(logResult.Rows) != 1 || logResult.Rows[0][0] != "example" {
		t.Fatalf("procedure_log rows = %#v", logResult.Rows)
	}
	if _, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM working_table"); err == nil {
		t.Fatal("working_table still exists after END branch")
	}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DELETE FROM source_table"); err != nil {
		t.Fatalf("DELETE source_table error = %v", err)
	}
	result, err := executor.QueryWithContext(ctx, executionContext, "CALL example('process')")
	if err != nil {
		t.Fatalf("CALL example(no data) error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "OK (no data)" {
		t.Fatalf("CALL example(no data) rows = %#v", result.Rows)
	}
}

func TestProcedureSupportsDynamicTemporaryAndTransientTables(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "DYNAMIC_PROCEDURE_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "DYNAMIC_PROCEDURE_DB", Schema: "PUBLIC"}

	tests := []struct {
		name          string
		tableKind     string
		procedureName string
		tablePrefix   string
	}{
		{name: "temporary", tableKind: "TEMPORARY", procedureName: "build_temporary_batch", tablePrefix: "temporary_batch_"},
		{name: "transient", tableKind: transientTableType, procedureName: "build_transient_batch", tablePrefix: "transient_batch_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			createSQL := fmt.Sprintf(`CREATE PROCEDURE %s(start_seq VARCHAR, end_seq VARCHAR)
		RETURNS NUMBER
		LANGUAGE SQL
		AS $$
		DECLARE
			table_name VARCHAR;
			row_count NUMBER;
		BEGIN
			table_name := '%s' || start_seq || end_seq;
			CREATE OR REPLACE %s TABLE IDENTIFIER(:table_name) AS
				SELECT 1 AS id UNION ALL SELECT 2 AS id;
			row_count := (SELECT COUNT(*) FROM identifier ( :table_name ));
			DROP TABLE IF EXISTS IDENTIFIER(:table_name);
			RETURN row_count;
		END
		$$`, tt.procedureName, tt.tablePrefix, tt.tableKind)
			if _, err := executor.ExecuteWithContext(ctx, executionContext, createSQL); err != nil {
				t.Fatalf("CREATE PROCEDURE error = %v", err)
			}

			callSQL := fmt.Sprintf("CALL %s('00', '99')", tt.procedureName)
			result, err := executor.QueryWithContext(ctx, executionContext, callSQL)
			if err != nil {
				t.Fatalf("%s error = %v", callSQL, err)
			}
			if len(result.Rows) != 1 || !procedureValuesEqual(result.Rows[0][0], 2) {
				t.Fatalf("%s rows = %#v, want 2", callSQL, result.Rows)
			}

			tableName := tt.tablePrefix + "0099"
			if _, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM "+tableName); err == nil {
				t.Fatalf("dynamic %s table %s still exists after DROP", tt.name, tableName)
			}
		})
	}
}

func TestProcedureCleansTemporaryTablesOnExit(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "TEMP_CLEANUP_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "TEMP_CLEANUP_DB", Schema: "PUBLIC"}

	tests := []struct {
		name      string
		body      string
		wantError bool
	}{
		{name: "return", body: "BEGIN CREATE TEMPORARY TABLE leftover (id INTEGER); RETURN 'OK'; END"},
		{name: "execution error", body: "BEGIN CREATE TEMPORARY TABLE leftover (id INTEGER); SELECT * FROM missing_table; END", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script, err := parseProcedureScript(tt.body)
			if err != nil {
				t.Fatalf("parseProcedureScript() error = %v", err)
			}
			if err := executor.withPinnedConnection(ctx, func(pinned *Executor) error {
				interpreter := newProcedureInterpreter(pinned, executionContext, "TEMP_CLEANUP")
				_, executeErr := interpreter.execute(ctx, script, nil, nil)
				if (executeErr != nil) != tt.wantError {
					t.Fatalf("execute() error = %v, wantError %v", executeErr, tt.wantError)
				}
				rows, queryErr := pinned.Query(ctx, `
					SELECT COUNT(*)
					FROM duckdb_tables()
					WHERE temporary AND table_name LIKE '__PROC_TEMP_%'`)
				if queryErr != nil {
					return queryErr
				}
				if len(rows.Rows) != 1 || !procedureValuesEqual(rows.Rows[0][0], 0) {
					t.Fatalf("temporary tables after procedure exit = %#v, want 0", rows.Rows)
				}
				return nil
			}); err != nil {
				t.Fatalf("withPinnedConnection() error = %v", err)
			}
		})
	}
}

func TestProcedureDynamicIdentifierValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		variables map[string]any
		want      string
		wantError string
	}{
		{name: "valid", input: "SELECT * FROM IDENTIFIER(:table_name)", variables: map[string]any{"TABLE_NAME": "dynamic_table"}, want: "SELECT * FROM dynamic_table"},
		{name: "case and spaces", input: "SELECT * FROM identifier ( :table_name )", variables: map[string]any{"TABLE_NAME": "dynamic_table"}, want: "SELECT * FROM dynamic_table"},
		{name: "inside string", input: "SELECT 'IDENTIFIER(:table_name)'", variables: map[string]any{"TABLE_NAME": "dynamic_table"}, want: "SELECT 'IDENTIFIER(:table_name)'"},
		{name: "undeclared", input: "SELECT * FROM IDENTIFIER(:missing)", wantError: "variable missing is not declared"},
		{name: "non string", input: "SELECT * FROM IDENTIFIER(:table_name)", variables: map[string]any{"TABLE_NAME": int64(42)}, wantError: "expected VARCHAR"},
		{name: "unsafe name", input: "SELECT * FROM IDENTIFIER(:table_name)", variables: map[string]any{"TABLE_NAME": "users; DROP TABLE users"}, wantError: "invalid object name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interpreter := newProcedureInterpreter(nil, ExecutionContext{}, "TEST")
			interpreter.variables = tt.variables
			got, err := interpreter.bindVariables(tt.input, false)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("bindVariables() error = %v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("bindVariables() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("bindVariables() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProcedureExceptionWhenOther(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "EXCEPTION_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "EXCEPTION_DB", Schema: "PUBLIC"}
	for _, statement := range []string{
		"CREATE TABLE existing_table (id INTEGER)",
		"CREATE TABLE procedure_errors (sql_code BIGINT, sql_state VARCHAR, sql_error VARCHAR)",
	} {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
			t.Fatalf("setup statement %q error = %v", statement, err)
		}
	}

	createSQL := `CREATE PROCEDURE catches_error()
		RETURNS VARCHAR
		LANGUAGE SQL
		AS $$
		BEGIN
			CREATE TABLE existing_table (id INTEGER);
			RETURN 'NO ERROR';
		EXCEPTION WHEN OTHER THEN
			INSERT INTO procedure_errors VALUES (:SQLCODE, :SQLSTATE, :SQLERRM);
			RETURN SQLSTATE;
		END
		$$`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, createSQL); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "CALL catches_error()")
	if err != nil {
		t.Fatalf("CALL catches_error error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "XX000" {
		t.Fatalf("CALL catches_error rows = %#v, want XX000", result.Rows)
	}

	errorResult, err := executor.QueryWithContext(ctx, executionContext,
		"SELECT sql_code, sql_state, sql_error FROM procedure_errors",
	)
	if err != nil {
		t.Fatalf("SELECT procedure_errors error = %v", err)
	}
	if len(errorResult.Rows) != 1 {
		t.Fatalf("procedure_errors rows = %#v, want one row", errorResult.Rows)
	}
	row := errorResult.Rows[0]
	if !procedureValuesEqual(row[0], -1) || row[1] != "XX000" || !strings.Contains(fmt.Sprint(row[2]), "already exists") {
		t.Fatalf("procedure_errors row = %#v", row)
	}
}

// TestCreateProcedureAllowsALeadingComment pins the fix for a statement the
// console's own splitter produces routinely: a comment describing the
// procedure, with no semicolon of its own, sits right above the CREATE and
// is folded into the same statement text. IsCreateProcedure already looks
// past a leading comment to dispatch here, but Create's own regex was
// anchored at the very start of the string — with the comment still there,
// "CREATE" was nowhere near position 0, and a perfectly ordinary CREATE
// PROCEDURE was rejected as unsupported syntax.
func TestCreateProcedureAllowsALeadingComment(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "COMMENT_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "COMMENT_DB", Schema: "PUBLIC"}

	createSQL := `-- Greets whoever is asked about.
-- Kept simple on purpose.
CREATE PROCEDURE greet(name VARCHAR)
RETURNS VARCHAR
LANGUAGE SQL
AS $$
BEGIN
    RETURN 'Hello, ' || :name;
END
$$`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, createSQL); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "CALL greet('World')")
	if err != nil {
		t.Fatalf("CALL error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "Hello, World" {
		t.Fatalf("CALL result = %#v, want Hello, World", result.Rows)
	}
}

// TestDropAndCallProcedureAllowALeadingCommentToo covers DROP PROCEDURE and
// CALL, which parse a statement the same anchored way CREATE PROCEDURE does.
func TestDropAndCallProcedureAllowALeadingCommentToo(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "COMMENT_DB2", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "COMMENT_DB2", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"CREATE PROCEDURE noop() RETURNS VARCHAR LANGUAGE SQL AS $$ BEGIN RETURN 'ok'; END $$"); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}

	if _, err := executor.QueryWithContext(ctx, executionContext,
		"-- calling it\nCALL noop()"); err != nil {
		t.Fatalf("commented CALL error = %v", err)
	}

	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"-- cleaning up\nDROP PROCEDURE noop()"); err != nil {
		t.Fatalf("commented DROP PROCEDURE error = %v", err)
	}
}

// TestSQLROWCOUNTIsReadableFromTheStart pins that SQLROWCOUNT — unlike
// SQLCODE/SQLSTATE/SQLERRM, which exist only once an exception handler is
// running — is a real variable from the moment a procedure starts, the same
// way it is in Snowflake, rather than an undeclared name that happens to
// read as literal SQL text until something sets it.
func TestSQLROWCOUNTIsReadableFromTheStart(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	if _, err := repo.CreateDatabase(ctx, "ROWCOUNT_DB", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "ROWCOUNT_DB", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		`CREATE PROCEDURE before_any_dml()
		RETURNS VARCHAR
		LANGUAGE SQL
		AS $$
		BEGIN
			RETURN 'n=' || SQLROWCOUNT;
		END
		$$`); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "CALL before_any_dml()")
	if err != nil {
		t.Fatalf("CALL error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "n=0" {
		t.Fatalf("CALL result = %#v, want n=0", result.Rows)
	}
}

// TestSQLROWCOUNTTracksTheMostRecentDML runs exactly the shape the user's own
// procedure needs: build a staging table from a CTE, MERGE it into a target,
// and read back how many rows the MERGE touched. It also checks that an
// earlier INSERT's count does not leak into a later statement's read of
// SQLROWCOUNT — it always reflects the MOST RECENT DML, not a running total.
func TestSQLROWCOUNTTracksTheMostRecentDML(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	executor.Configure(WithMergeProcessor(NewMergeProcessor(executor)))
	ctx := context.Background()
	if _, err := repo.CreateDatabase(ctx, "ROWCOUNT_DB2", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "ROWCOUNT_DB2", Schema: "PUBLIC"}

	for _, statement := range []string{
		"CREATE TABLE seed_users (id INTEGER, name VARCHAR)",
		"INSERT INTO seed_users VALUES (1, 'Alice'), (2, 'Bob'), (3, 'Oswaldo')",
		"CREATE TABLE clean_users (id INTEGER, name VARCHAR, updated_at TIMESTAMP)",
		// Pre-seed one row so the MERGE below does one UPDATE and two INSERTs —
		// three rows affected, not the row count of any single branch alone.
		"INSERT INTO clean_users (id, name) VALUES (1, 'stale name')",
	} {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
			t.Fatalf("setup %q error = %v", statement, err)
		}
	}

	createSQL := `CREATE PROCEDURE load_clean_users()
RETURNS VARCHAR
LANGUAGE SQL
AS
$$
DECLARE
    rows_affected INTEGER DEFAULT 0;
BEGIN
    CREATE OR REPLACE TEMPORARY TABLE raw_users AS
    WITH raw AS (
        SELECT id, name FROM seed_users
    )
    SELECT id, name FROM raw;

    MERGE INTO clean_users AS target
    USING raw_users AS source
        ON target.id = source.id
    WHEN MATCHED THEN
        UPDATE SET target.name = source.name
    WHEN NOT MATCHED THEN
        INSERT (id, name) VALUES (source.id, source.name);

    rows_affected := SQLROWCOUNT;

    RETURN 'MERGE completed. Rows affected: ' || rows_affected;
END;
$$`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, createSQL); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "CALL load_clean_users()")
	if err != nil {
		t.Fatalf("CALL error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "MERGE completed. Rows affected: 3" {
		t.Fatalf("CALL result = %#v, want the MERGE's own 3 rows, not the earlier INSERT's", result.Rows)
	}

	rows, err := executor.QueryWithContext(ctx, executionContext,
		"SELECT id, name FROM clean_users ORDER BY id")
	if err != nil {
		t.Fatalf("readback error = %v", err)
	}
	if len(rows.Rows) != 3 {
		t.Fatalf("clean_users rows = %#v, want 3", rows.Rows)
	}
	if rows.Rows[0][1] != "Alice" {
		t.Fatalf("the pre-seeded row was not updated: %#v", rows.Rows[0])
	}
}

// TestSQLROWCOUNTSurvivesIntoAnExceptionHandler mirrors the user's own
// request: add EXCEPTION WHEN OTHER to a procedure built around SQLROWCOUNT,
// and confirm the handler can still read whatever DML most recently ran —
// SQLCODE/SQLSTATE/SQLERRM describe the failure itself, but SQLROWCOUNT is
// not reset by the exception, so a handler can report what was salvaged
// before the failing statement.
func TestSQLROWCOUNTSurvivesIntoAnExceptionHandler(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	if _, err := repo.CreateDatabase(ctx, "ROWCOUNT_DB3", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "ROWCOUNT_DB3", Schema: "PUBLIC"}

	for _, statement := range []string{
		"CREATE TABLE clean_users (id INTEGER, name VARCHAR)",
		"CREATE TABLE procedure_log (message VARCHAR)",
	} {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
			t.Fatalf("setup %q error = %v", statement, err)
		}
	}

	createSQL := `CREATE PROCEDURE load_clean_users()
RETURNS VARCHAR
LANGUAGE SQL
AS
$$
BEGIN
    INSERT INTO clean_users VALUES (1, 'Alice'), (2, 'Bob');

    -- clean_users has two columns; this MERGE names a third that does not
    -- exist, so it fails after the INSERT above has already run.
    MERGE INTO clean_users AS target
    USING clean_users AS source
        ON target.id = source.id
    WHEN MATCHED THEN
        UPDATE SET target.missing_column = source.name;

    RETURN 'unreachable';
EXCEPTION
    WHEN OTHER THEN
        INSERT INTO procedure_log VALUES (
            'failed after ' || :SQLROWCOUNT || ' rows; ' || :SQLSTATE
        );
        RETURN 'handled';
END;
$$`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, createSQL); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "CALL load_clean_users()")
	if err != nil {
		t.Fatalf("CALL error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "handled" {
		t.Fatalf("CALL result = %#v, want the handler's own return value", result.Rows)
	}

	logResult, err := executor.QueryWithContext(ctx, executionContext, "SELECT message FROM procedure_log")
	if err != nil {
		t.Fatalf("SELECT procedure_log error = %v", err)
	}
	if len(logResult.Rows) != 1 || logResult.Rows[0][0] != "failed after 2 rows; XX000" {
		t.Fatalf("procedure_log rows = %#v, want the INSERT's 2 rows to have survived into the handler", logResult.Rows)
	}
}

func TestParseProcedureScriptRejectsMalformedBlocks(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing begin", body: "RETURN 'bad'; END"},
		{name: "missing end if", body: "BEGIN IF (TRUE) THEN RETURN 'bad'; END"},
		{name: "missing end case", body: "BEGIN CASE (1) WHEN 1 THEN RETURN 'bad'; END"},
		{name: "default without expression", body: "DECLARE value VARCHAR DEFAULT; BEGIN RETURN value; END"},
		{name: "invalid exception handler", body: "BEGIN RETURN 'ok'; EXCEPTION WHEN SQLSTATE THEN RETURN 'bad'; END"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseProcedureScript(tt.body); err == nil {
				t.Fatalf("parseProcedureScript(%q) returned nil error", tt.body)
			}
		})
	}
}

func TestCreateProcedureRejectsMalformedBodyAndUnsupportedLanguage(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	database, err := repo.CreateDatabase(ctx, "INVALID_PROCEDURE_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}

	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "malformed SQL body",
			sql:  `CREATE PROCEDURE INVALID_PROCEDURE_DB.PUBLIC.BAD_BODY() RETURNS VARCHAR LANGUAGE SQL AS $$ BEGIN IF (TRUE) THEN RETURN 'bad'; END $$`,
		},
		{
			name: "unsupported language",
			sql:  `CREATE PROCEDURE INVALID_PROCEDURE_DB.PUBLIC.BAD_LANGUAGE() RETURNS VARCHAR LANGUAGE JAVASCRIPT AS $$ BEGIN RETURN 'bad'; END $$`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := executor.Execute(ctx, tt.sql); err == nil {
				t.Fatalf("Execute(%q) returned nil error", tt.sql)
			}
		})
	}
}
