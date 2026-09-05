package query

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/nnnkkk7/snowflake-emulator/pkg/connection"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

func TestExecutor_ViewLifecycle(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	database, err := repo.CreateDatabase(ctx, "VIEW_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	schema, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "VIEW_DB", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE users (id INTEGER, active BOOLEAN)"); err != nil {
		t.Fatalf("CREATE TABLE error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "INSERT INTO users VALUES (1, TRUE), (2, FALSE)"); err != nil {
		t.Fatalf("INSERT error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE VIEW active_users AS SELECT id FROM users WHERE active"); err != nil {
		t.Fatalf("CREATE VIEW error = %v", err)
	}

	view, err := repo.GetTableByName(ctx, schema.ID, "ACTIVE_USERS")
	if err != nil {
		t.Fatalf("view missing from catalog: %v", err)
	}
	if view.TableType != "VIEW" {
		t.Fatalf("table type = %q, want VIEW", view.TableType)
	}
	if tables, err := repo.ListTables(ctx, schema.ID); err != nil {
		t.Fatalf("ListTables() error = %v", err)
	} else if len(tables) != 1 || tables[0].Name != "USERS" {
		t.Fatalf("table-only catalog leaked view: %#v", tables)
	}
	if views, err := repo.ListViews(ctx, schema.ID); err != nil {
		t.Fatalf("ListViews() error = %v", err)
	} else if len(views) != 1 || views[0].Name != "ACTIVE_USERS" {
		t.Fatalf("view catalog = %#v", views)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM active_users")
	if err != nil {
		t.Fatalf("SELECT view error = %v", err)
	}
	if diff := cmp.Diff([][]interface{}{{int32(1)}}, result.Rows); diff != "" {
		t.Errorf("view rows mismatch (-want +got):\n%s", diff)
	}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE OR REPLACE VIEW active_users AS SELECT id FROM users"); err != nil {
		t.Fatalf("CREATE OR REPLACE VIEW error = %v", err)
	}
	result, err = executor.QueryWithContext(ctx, executionContext, "SHOW VIEWS")
	if err != nil {
		t.Fatalf("SHOW VIEWS error = %v", err)
	}
	if diff := cmp.Diff([][]interface{}{{"ACTIVE_USERS", "VIEW_DB", "PUBLIC", "VIEW"}}, result.Rows); diff != "" {
		t.Errorf("SHOW VIEWS mismatch (-want +got):\n%s", diff)
	}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP VIEW active_users"); err != nil {
		t.Fatalf("DROP VIEW error = %v", err)
	}
	if _, err := repo.GetTableByName(ctx, schema.ID, "ACTIVE_USERS"); err == nil {
		t.Fatal("view remains in catalog after DROP VIEW")
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP VIEW IF EXISTS active_users"); err != nil {
		t.Fatalf("DROP VIEW IF EXISTS error = %v", err)
	}
}

func TestExecutor_QualifiedViewAndTranslatedBody(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	for _, name := range []string{"SOURCE_DB", "VIEW_DB"} {
		if _, err := repo.CreateDatabase(ctx, name, ""); err != nil {
			t.Fatalf("CreateDatabase(%s) error = %v", name, err)
		}
	}
	sourceContext := ExecutionContext{Database: "SOURCE_DB", Schema: "PUBLIC"}
	if _, err := executor.ExecuteWithContext(ctx, sourceContext, "CREATE TABLE users (name VARCHAR)"); err != nil {
		t.Fatalf("CREATE TABLE error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, sourceContext, "INSERT INTO users VALUES ('oswaldo')"); err != nil {
		t.Fatalf("INSERT error = %v", err)
	}

	statement := "CREATE VIEW VIEW_DB.PUBLIC.user_names AS SELECT NVL(name, 'unknown') AS name FROM SOURCE_DB.PUBLIC.users"
	if _, err := executor.ExecuteWithContext(ctx, sourceContext, statement); err != nil {
		t.Fatalf("qualified CREATE VIEW error = %v", err)
	}
	result, err := executor.QueryWithContext(ctx, ExecutionContext{Database: "VIEW_DB", Schema: "PUBLIC"}, "SELECT * FROM user_names")
	if err != nil {
		t.Fatalf("SELECT qualified view error = %v", err)
	}
	if diff := cmp.Diff([][]interface{}{{"oswaldo"}}, result.Rows); diff != "" {
		t.Errorf("qualified view rows mismatch (-want +got):\n%s", diff)
	}
}

func TestClassifier_Views(t *testing.T) {
	classifier := NewClassifier()
	tests := []struct {
		name  string
		sql   string
		check func(string) bool
	}{
		{name: "create", sql: "CREATE VIEW v AS SELECT 1", check: classifier.IsCreateView},
		{name: "replace", sql: "CREATE OR REPLACE VIEW v AS SELECT 1", check: classifier.IsCreateView},
		{name: "drop", sql: "DROP VIEW v", check: classifier.IsDropView},
		{name: "drop if exists", sql: "DROP VIEW IF EXISTS v", check: classifier.IsDropView},
		{name: "show", sql: "SHOW VIEWS", check: classifier.IsShowViews},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.check(tt.sql) {
				t.Fatalf("classifier rejected %q", tt.sql)
			}
		})
	}
}

func TestExecutor_ViewColumnList(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	if _, err := repo.CreateDatabase(ctx, "VIEW_DB", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "VIEW_DB", Schema: "PUBLIC"}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE VIEW named_columns (answer) AS SELECT 42"); err != nil {
		t.Fatalf("CREATE VIEW with column list error = %v", err)
	}
	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT answer FROM named_columns")
	if err != nil {
		t.Fatalf("SELECT view column error = %v", err)
	}
	if diff := cmp.Diff([]string{"answer"}, result.Columns); diff != "" {
		t.Errorf("columns mismatch (-want +got):\n%s", diff)
	}
}

func TestExecutor_QualifiedViewLifecycle(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	database, err := repo.CreateDatabase(ctx, "VIEW_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.CreateSchema(ctx, database.ID, "ANALYTICS", ""); err != nil {
		t.Fatalf("CreateSchema() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "VIEW_DB", Schema: "PUBLIC"}

	tests := []struct {
		name    string
		view    string
		create  string
		replace string
		drop    string
	}{
		{name: "two part", view: "V_TWO", create: "CREATE VIEW ANALYTICS.v_two AS SELECT 1 AS n", replace: "CREATE OR REPLACE VIEW ANALYTICS.v_two AS SELECT 2 AS n", drop: "DROP VIEW ANALYTICS.v_two"},
		{name: "three part", view: "V_THREE", create: "CREATE VIEW VIEW_DB.ANALYTICS.v_three AS SELECT 1 AS n", replace: "CREATE OR REPLACE VIEW VIEW_DB.ANALYTICS.v_three AS SELECT 3 AS n", drop: "DROP VIEW IF EXISTS VIEW_DB.ANALYTICS.v_three"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := executor.ExecuteWithContext(ctx, executionContext, tt.create); err != nil {
				t.Fatalf("create error = %v", err)
			}
			if _, err := executor.ExecuteWithContext(ctx, executionContext, tt.replace); err != nil {
				t.Fatalf("replace error = %v", err)
			}
			if _, err := executor.ExecuteWithContext(ctx, executionContext, tt.drop); err != nil {
				t.Fatalf("drop error = %v", err)
			}
			analytics, err := repo.GetSchemaByName(ctx, database.ID, "ANALYTICS")
			if err != nil {
				t.Fatalf("GetSchemaByName() error = %v", err)
			}
			if _, err := repo.GetTableByName(ctx, analytics.ID, tt.view); err == nil {
				t.Fatal("qualified DROP left view metadata behind")
			}
		})
	}
}

func TestExecutor_ViewErrorsAndNamespaceCollisions(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	database, err := repo.CreateDatabase(ctx, "VIEW_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	schema, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "VIEW_DB", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE VIEW duplicate_view AS SELECT 1 AS n"); err != nil {
		t.Fatalf("initial CREATE VIEW error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE VIEW duplicate_view AS SELECT 2 AS n"); err == nil {
		t.Fatal("duplicate CREATE VIEW unexpectedly succeeded")
	}
	if view, err := repo.GetTableByName(ctx, schema.ID, "DUPLICATE_VIEW"); err != nil || view.TableType != viewTableType {
		t.Fatalf("duplicate failure damaged catalog: view=%v err=%v", view, err)
	}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE table_first (id INTEGER)"); err != nil {
		t.Fatalf("CREATE TABLE error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE VIEW table_first AS SELECT 1 AS id"); err == nil {
		t.Fatal("view over existing table unexpectedly succeeded")
	}
	if object, err := repo.GetTableByName(ctx, schema.ID, "TABLE_FIRST"); err != nil || object.TableType != baseTableType {
		t.Fatalf("view collision damaged table metadata: object=%v err=%v", object, err)
	}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE VIEW view_first AS SELECT 1 AS id"); err != nil {
		t.Fatalf("CREATE VIEW error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE view_first (id INTEGER)"); err == nil {
		t.Fatal("table over existing view unexpectedly succeeded")
	}
	if object, err := repo.GetTableByName(ctx, schema.ID, "VIEW_FIRST"); err != nil || object.TableType != viewTableType {
		t.Fatalf("table collision damaged view metadata: object=%v err=%v", object, err)
	}

	for _, statement := range []string{
		"CREATE VIEW MISSING.v AS SELECT 1",
		"CREATE VIEW NO_DB.PUBLIC.v AS SELECT 1",
		"DROP VIEW MISSING.v",
	} {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err == nil {
			t.Errorf("invalid namespace %q unexpectedly succeeded", statement)
		}
	}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE VIEW drop_if_exists AS SELECT 1"); err != nil {
		t.Fatalf("CREATE VIEW for DROP error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP VIEW IF EXISTS drop_if_exists"); err != nil {
		t.Fatalf("DROP VIEW IF EXISTS existing error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP VIEW IF EXISTS missing_view"); err != nil {
		t.Fatalf("DROP VIEW IF EXISTS missing error = %v", err)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM table_first")
	if err != nil || len(result.Columns) != 1 {
		t.Fatalf("physical table inconsistent after collision: result=%v err=%v", result, err)
	}
	result, err = executor.QueryWithContext(ctx, executionContext, "SELECT * FROM view_first")
	if err != nil || len(result.Columns) != 1 {
		t.Fatalf("physical view inconsistent after collision: result=%v err=%v", result, err)
	}
}

func TestExecutor_ViewPersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "views.db")

	open := func() (*sql.DB, *Executor, *metadata.Repository) {
		t.Helper()
		db, err := sql.Open("duckdb", databasePath)
		if err != nil {
			t.Fatalf("open DuckDB: %v", err)
		}
		db.SetMaxOpenConns(1)
		manager := connection.NewManager(db)
		repo, err := metadata.NewRepository(manager)
		if err != nil {
			_ = db.Close()
			t.Fatalf("new repository: %v", err)
		}
		return db, NewExecutor(manager, repo), repo
	}

	db, executor, repo := open()
	database, err := repo.CreateDatabase(ctx, "VIEW_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	schema, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "VIEW_DB", Schema: "PUBLIC"}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE VIEW persistent_view AS SELECT 7 AS n"); err != nil {
		t.Fatalf("CREATE VIEW error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}

	db, executor, repo = open()
	defer func() { _ = db.Close() }()
	view, err := repo.GetTableByName(ctx, schema.ID, "PERSISTENT_VIEW")
	if err != nil || view.TableType != viewTableType {
		t.Fatalf("persisted catalog view=%v error=%v", view, err)
	}
	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT n FROM persistent_view")
	if err != nil {
		t.Fatalf("persisted physical view query error = %v", err)
	}
	if got := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(result.Columns[0]), ";")); got != "n" {
		t.Fatalf("persisted view column = %q, want n", got)
	}
	if diff := cmp.Diff([][]interface{}{{int32(7)}}, result.Rows); diff != "" {
		t.Errorf("persisted view rows mismatch (-want +got):\n%s", diff)
	}
}
