package query

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"github.com/nnnkkk7/snowflake-emulator/pkg/warehouse"
)

func TestRewriteContextualTableReferences(t *testing.T) {
	executionContext := ExecutionContext{Database: "LEARNING_DB", Schema: "PUBLIC"}
	tests := []struct {
		input string
		want  string
	}{
		{"CREATE TABLE users (id INTEGER)", "CREATE TABLE LEARNING_DB.PUBLIC_USERS (id INTEGER)"},
		{"INSERT INTO users VALUES (1)", "INSERT INTO LEARNING_DB.PUBLIC_USERS VALUES (1)"},
		{"SELECT * FROM users", "SELECT * FROM LEARNING_DB.PUBLIC_USERS"},
		{"SELECT * FROM users JOIN roles ON users.id = roles.id", "SELECT * FROM LEARNING_DB.PUBLIC_USERS JOIN LEARNING_DB.PUBLIC_ROLES ON users.id = roles.id"},
		{"SELECT * FROM OTHER_DB.PUBLIC_USERS", "SELECT * FROM OTHER_DB.PUBLIC_USERS"},
	}
	for _, tt := range tests {
		if got := rewriteContextualTableReferences(tt.input, executionContext); got != tt.want {
			t.Errorf("rewriteContextualTableReferences(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExecutorValidatesAllExecutionContextFields(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	database, err := repo.CreateDatabase(ctx, "LEARNING_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}

	warehouseManager := warehouse.NewManager()
	executor.Configure(WithWarehouseValidator(func(ctx context.Context, name string) error {
		_, err := warehouseManager.GetWarehouse(ctx, name)
		return err
	}))

	tests := []struct {
		name             string
		executionContext ExecutionContext
		wantError        string
	}{
		{name: "missing database", executionContext: ExecutionContext{Database: "MISSING_DB"}, wantError: "database MISSING_DB not found"},
		{name: "missing schema", executionContext: ExecutionContext{Database: "LEARNING_DB", Schema: "MISSING_SCHEMA"}, wantError: "schema MISSING_SCHEMA not found"},
		{name: "schema without database", executionContext: ExecutionContext{Schema: "PUBLIC"}, wantError: "requires a database context"},
		{name: "missing warehouse", executionContext: ExecutionContext{Database: "LEARNING_DB", Schema: "PUBLIC", Warehouse: "MISSING_WH"}, wantError: "warehouse MISSING_WH not found"},
		{name: "unsupported role", executionContext: ExecutionContext{Database: "LEARNING_DB", Schema: "PUBLIC", Role: "SYSADMIN"}, wantError: "role SYSADMIN cannot be validated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.QueryWithContext(ctx, tt.executionContext, "SELECT 1")
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("QueryWithContext() error = %v, want error containing %q", err, tt.wantError)
			}
		})
	}

	if _, err := warehouseManager.CreateWarehouse(ctx, "LEARNING_WH", "X-SMALL", ""); err != nil {
		t.Fatalf("CreateWarehouse() error = %v", err)
	}
	validContext := ExecutionContext{Database: "LEARNING_DB", Schema: "PUBLIC", Warehouse: "LEARNING_WH"}
	if _, err := executor.QueryWithContext(ctx, validContext, "SELECT 1"); err != nil {
		t.Fatalf("valid execution context returned error: %v", err)
	}
}

func TestExecutorRejectsMissingSchemaContext(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	if _, err := repo.CreateDatabase(ctx, "LEARNING_DB", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}

	executionContext := ExecutionContext{Database: "LEARNING_DB", Schema: "MISSING_SCHEMA"}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE users (ID INTEGER)"); err == nil {
		t.Fatal("CREATE TABLE with missing schema returned nil error")
	}

	var count int
	err := executor.mgr.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'LEARNING_DB' AND table_name = 'MISSING_SCHEMA_USERS'`).Scan(&count)
	if err != nil {
		t.Fatalf("table existence query error = %v", err)
	}
	if count != 0 {
		t.Fatalf("missing-schema context created %d physical tables, want 0", count)
	}
}

func TestExecutorResolvesQualifiedTableReferences(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	testDatabase, err := repo.CreateDatabase(ctx, "TEST_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase(TEST_DB) error = %v", err)
	}
	otherDatabase, err := repo.CreateDatabase(ctx, "OTHER_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase(OTHER_DB) error = %v", err)
	}
	testPublic, err := repo.GetSchemaByName(ctx, testDatabase.ID, "PUBLIC")
	if err != nil {
		t.Fatalf("GetSchemaByName(TEST_DB.PUBLIC) error = %v", err)
	}
	otherPublic, err := repo.GetSchemaByName(ctx, otherDatabase.ID, "PUBLIC")
	if err != nil {
		t.Fatalf("GetSchemaByName(OTHER_DB.PUBLIC) error = %v", err)
	}
	analytics, err := repo.CreateSchema(ctx, testDatabase.ID, "ANALYTICS", "")
	if err != nil {
		t.Fatalf("CreateSchema(ANALYTICS) error = %v", err)
	}
	columns := []metadata.ColumnDef{{Name: "ID", Type: "INTEGER", Nullable: true}}
	for _, schema := range []*metadata.Schema{testPublic, otherPublic, analytics} {
		if _, err := repo.CreateTable(ctx, schema.ID, "USERS", columns, ""); err != nil {
			t.Fatalf("CreateTable(%s.USERS) error = %v", schema.Name, err)
		}
	}

	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}
	for _, statement := range []string{
		"INSERT INTO users VALUES (1)",
		"INSERT INTO OTHER_DB.PUBLIC.USERS VALUES (2)",
		"INSERT INTO ANALYTICS.USERS VALUES (3)",
	} {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
			t.Fatalf("ExecuteWithContext(%q) error = %v", statement, err)
		}
	}

	tests := []struct {
		name string
		sql  string
		want int
	}{
		{name: "unqualified", sql: "SELECT id FROM users", want: 1},
		{name: "schema qualified", sql: "SELECT id FROM PUBLIC.USERS", want: 1},
		{name: "fully qualified", sql: "SELECT id FROM OTHER_DB.PUBLIC.USERS", want: 2},
		{name: "other schema", sql: "SELECT id FROM ANALYTICS.USERS", want: 3},
		{name: "already physical", sql: "SELECT id FROM TEST_DB.PUBLIC_USERS", want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.QueryWithContext(ctx, executionContext, tt.sql)
			if err != nil {
				t.Fatalf("QueryWithContext(%q) error = %v", tt.sql, err)
			}
			if len(result.Rows) != 1 || !procedureValuesEqual(result.Rows[0][0], tt.want) {
				t.Fatalf("QueryWithContext(%q) rows = %#v, want %d", tt.sql, result.Rows, tt.want)
			}
		})
	}
}

func TestExecutorRejectsMissingQualifiedNamespace(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	database, err := repo.CreateDatabase(ctx, "TEST_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName(PUBLIC) error = %v", err)
	}

	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}
	tests := []struct {
		sql       string
		wantError string
	}{
		{sql: "SELECT * FROM MISSING_SCHEMA.USERS", wantError: "schema MISSING_SCHEMA not found"},
		{sql: "SELECT * FROM MISSING_DB.PUBLIC.USERS", wantError: "database MISSING_DB not found"},
	}
	for _, tt := range tests {
		if _, err := executor.QueryWithContext(ctx, executionContext, tt.sql); err == nil || !strings.Contains(err.Error(), tt.wantError) {
			t.Fatalf("QueryWithContext(%q) error = %v, want containing %q", tt.sql, err, tt.wantError)
		}
	}
}

func TestExecutorResolvesQualifiedDDLReferences(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	database, err := repo.CreateDatabase(ctx, "DDL_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.CreateSchema(ctx, database.ID, "ANALYTICS", ""); err != nil {
		t.Fatalf("CreateSchema() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "DDL_DB", Schema: "PUBLIC"}

	for _, tableName := range []string{"ANALYTICS.EVENTS", "DDL_DB.ANALYTICS.METRICS"} {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE "+tableName+" (id INTEGER)"); err != nil {
			t.Fatalf("CREATE TABLE %s error = %v", tableName, err)
		}
		if _, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM "+tableName); err != nil {
			t.Fatalf("SELECT FROM %s error = %v", tableName, err)
		}
		if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP TABLE "+tableName); err != nil {
			t.Fatalf("DROP TABLE %s error = %v", tableName, err)
		}
	}
}

func TestMergeResolvesFullyQualifiedTables(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	executor.Configure(WithMergeProcessor(NewMergeProcessor(executor)))
	ctx := context.Background()

	targetDatabase, err := repo.CreateDatabase(ctx, "TARGET_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase(TARGET_DB) error = %v", err)
	}
	sourceDatabase, err := repo.CreateDatabase(ctx, "SOURCE_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase(SOURCE_DB) error = %v", err)
	}
	targetSchema, err := repo.GetSchemaByName(ctx, targetDatabase.ID, "PUBLIC")
	if err != nil {
		t.Fatalf("GetSchemaByName(TARGET_DB.PUBLIC) error = %v", err)
	}
	sourceSchema, err := repo.GetSchemaByName(ctx, sourceDatabase.ID, "PUBLIC")
	if err != nil {
		t.Fatalf("GetSchemaByName(SOURCE_DB.PUBLIC) error = %v", err)
	}
	columns := []metadata.ColumnDef{{Name: "ID", Type: "INTEGER", Nullable: true}}
	if _, err := repo.CreateTable(ctx, targetSchema.ID, "TARGET_ROWS", columns, ""); err != nil {
		t.Fatalf("CreateTable(TARGET_ROWS) error = %v", err)
	}
	if _, err := repo.CreateTable(ctx, sourceSchema.ID, "SOURCE_ROWS", columns, ""); err != nil {
		t.Fatalf("CreateTable(SOURCE_ROWS) error = %v", err)
	}
	executionContext := ExecutionContext{Database: "TARGET_DB", Schema: "PUBLIC"}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "INSERT INTO SOURCE_DB.PUBLIC.SOURCE_ROWS VALUES (7)"); err != nil {
		t.Fatalf("INSERT source error = %v", err)
	}

	mergeSQL := `MERGE INTO TARGET_DB.PUBLIC.TARGET_ROWS target
		USING SOURCE_DB.PUBLIC.SOURCE_ROWS source
		ON target.id = source.id
		WHEN NOT MATCHED THEN INSERT (id) VALUES (source.id)`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, mergeSQL); err != nil {
		t.Fatalf("qualified MERGE error = %v", err)
	}
	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT id FROM TARGET_DB.PUBLIC.TARGET_ROWS")
	if err != nil {
		t.Fatalf("SELECT merged target error = %v", err)
	}
	if len(result.Rows) != 1 || !procedureValuesEqual(result.Rows[0][0], 7) {
		t.Fatalf("merged rows = %#v, want 7", result.Rows)
	}
}

func TestCatalogAwarePreviewResolvesQualifiedNames(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	database, err := repo.CreateDatabase(ctx, "PREVIEW_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.CreateSchema(ctx, database.ID, "ANALYTICS", ""); err != nil {
		t.Fatalf("CreateSchema() error = %v", err)
	}

	preview, err := executor.PreviewTranslationWithContext(ctx,
		"SELECT * FROM ANALYTICS.EVENTS",
		ExecutionContext{Database: "PREVIEW_DB", Schema: "PUBLIC"},
	)
	if err != nil {
		t.Fatalf("PreviewTranslationWithContext() error = %v", err)
	}
	if preview.Translated != "select * from PREVIEW_DB.ANALYTICS_EVENTS" {
		t.Fatalf("translated preview = %q", preview.Translated)
	}
}

// TestRewriteContextualTableReferences_StatementForms covers the statement
// shapes whose table references must resolve against the execution context.
func TestRewriteContextualTableReferences_StatementForms(t *testing.T) {
	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}

	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "describe table",
			sql:  "DESCRIBE TABLE users",
			want: "DESCRIBE TABLE TEST_DB.PUBLIC_USERS",
		},
		{
			name: "desc table shorthand",
			sql:  "DESC TABLE users",
			want: "DESC TABLE TEST_DB.PUBLIC_USERS",
		},
		{
			name: "truncate table",
			sql:  "TRUNCATE TABLE users",
			want: "TRUNCATE TABLE TEST_DB.PUBLIC_USERS",
		},
		{
			name: "merge resolves both tables",
			sql:  "MERGE INTO target t USING source s ON t.id = s.id",
			want: "MERGE INTO TEST_DB.PUBLIC_TARGET t USING TEST_DB.PUBLIC_SOURCE s ON t.id = s.id",
		},
		{
			// The UPDATE pattern used to capture SET as though it named a table,
			// producing "UPDATE TEST_DB.PUBLIC_SET name = ...".
			name: "merge update set is not treated as a table",
			sql:  "MERGE INTO target t USING source s ON t.id = s.id WHEN MATCHED THEN UPDATE SET name = s.name",
			want: "MERGE INTO TEST_DB.PUBLIC_TARGET t USING TEST_DB.PUBLIC_SOURCE s ON t.id = s.id WHEN MATCHED THEN UPDATE SET name = s.name",
		},
		{
			// A JOIN's USING (col) list is a column list, not a table reference.
			name: "join using column list is left alone",
			sql:  "SELECT * FROM orders JOIN users USING (id)",
			want: "SELECT * FROM TEST_DB.PUBLIC_ORDERS JOIN TEST_DB.PUBLIC_USERS USING (id)",
		},
		{
			name: "already qualified names are left alone",
			sql:  "SELECT * FROM OTHER_DB.PUBLIC_USERS",
			want: "SELECT * FROM OTHER_DB.PUBLIC_USERS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rewriteContextualTableReferences(tt.sql, executionContext); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestRewriteContextualTableReferences_WithoutContext pins that an incomplete
// namespace leaves the statement untouched.
func TestRewriteContextualTableReferences_WithoutContext(t *testing.T) {
	sql := "SELECT * FROM users"

	for _, executionContext := range []ExecutionContext{
		{},
		{Database: "TEST_DB"},
		{Schema: "PUBLIC"},
	} {
		if got := rewriteContextualTableReferences(sql, executionContext); got != sql {
			t.Errorf("context %+v rewrote to %q, want it unchanged", executionContext, got)
		}
	}
}

// TestPhysicalNameError pins that engine errors name the objects the caller
// wrote, not the DATABASE.SCHEMA_TABLE form the emulator rewrites them into.
func TestPhysicalNameError(t *testing.T) {
	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}

	tests := []struct {
		name             string
		executionContext ExecutionContext
		err              error
		want             string
	}{
		{
			name:             "qualified physical name",
			executionContext: executionContext,
			err:              errors.New(`Table with name TEST_DB.PUBLIC_ORDERS does not exist!`),
			want:             `Table with name ORDERS does not exist!`,
		},
		{
			name:             "bare physical name",
			executionContext: executionContext,
			err:              errors.New(`Table with name PUBLIC_ORDERS does not exist!`),
			want:             `Table with name ORDERS does not exist!`,
		},
		{
			name:             "the quoted statement is cleaned too",
			executionContext: executionContext,
			err:              errors.New("LINE 1: select * from TEST_DB.PUBLIC_ORDERS"),
			want:             "LINE 1: select * from ORDERS",
		},
		{
			name:             "an unrelated message is untouched",
			executionContext: executionContext,
			err:              errors.New("Parser Error: syntax error"),
			want:             "Parser Error: syntax error",
		},
		{
			name:             "without a context nothing is rewritten",
			executionContext: ExecutionContext{},
			err:              errors.New("Table with name PUBLIC_ORDERS does not exist!"),
			want:             "Table with name PUBLIC_ORDERS does not exist!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := physicalNameError(tt.err, tt.executionContext)
			if got.Error() != tt.want {
				t.Errorf("got  %q\nwant %q", got.Error(), tt.want)
			}
		})
	}
}

func TestPhysicalNameErrorPassesNilThrough(t *testing.T) {
	if got := physicalNameError(nil, ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}
