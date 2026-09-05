package query

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/nnnkkk7/snowflake-emulator/pkg/connection"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

func TestExecutor_DynamicTableLifecycleAndRefresh(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	executor.Configure(WithWarehouseValidator(func(_ context.Context, name string) error {
		if name != "COMPUTE_WH" {
			return fmt.Errorf("missing")
		}
		return nil
	}))
	ctx := context.Background()
	database, err := repo.CreateDatabase(ctx, "DYNAMIC_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if err != nil {
		t.Fatal(err)
	}
	executionContext := ExecutionContext{Database: database.Name, Schema: schema.Name}

	mustExecute := func(sql string) {
		t.Helper()
		if _, err := executor.ExecuteWithContext(ctx, executionContext, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	mustExecute("CREATE TABLE source_values (id INTEGER, name VARCHAR)")
	mustExecute("INSERT INTO source_values VALUES (1, 'one')")
	mustExecute("CREATE DYNAMIC TABLE current_values TARGET_LAG = '1 MINUTE' WAREHOUSE = COMPUTE_WH AS SELECT id, NVL(name, 'unknown') AS name FROM source_values")

	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM current_values ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([][]interface{}{{int32(1), "one"}}, result.Rows); diff != "" {
		t.Fatalf("rows (-want +got):\n%s", diff)
	}
	mustExecute("INSERT INTO source_values VALUES (2, 'two')")
	result, _ = executor.QueryWithContext(ctx, executionContext, "SELECT * FROM current_values ORDER BY id")
	if len(result.Rows) != 1 {
		t.Fatalf("dynamic table refreshed automatically: %#v", result.Rows)
	}
	mustExecute("ALTER DYNAMIC TABLE current_values REFRESH")
	result, _ = executor.QueryWithContext(ctx, executionContext, "SELECT * FROM current_values ORDER BY id")
	if len(result.Rows) != 2 {
		t.Fatalf("refresh rows = %#v", result.Rows)
	}

	dynamic, err := repo.GetDynamicTableByName(ctx, schema.ID, "CURRENT_VALUES")
	if err != nil || dynamic.Definition == "" || dynamic.LastRefreshedAt == nil {
		t.Fatalf("dynamic metadata = %#v, %v", dynamic, err)
	}
	if tables, _ := repo.ListTables(ctx, schema.ID); len(tables) != 1 || tables[0].Name != "SOURCE_VALUES" {
		t.Fatalf("ordinary tables leaked dynamic table: %#v", tables)
	}
	if views, _ := repo.ListViews(ctx, schema.ID); len(views) != 0 {
		t.Fatalf("views leaked dynamic table: %#v", views)
	}
	show, err := executor.QueryWithContext(ctx, executionContext, "SHOW DYNAMIC TABLES")
	if err != nil || len(show.Rows) != 1 {
		t.Fatalf("SHOW DYNAMIC TABLES = %#v, %v", show, err)
	}

	mustExecute("CREATE OR REPLACE DYNAMIC TABLE current_values TARGET_LAG = '5 MINUTES' WAREHOUSE = COMPUTE_WH AS SELECT id FROM source_values")
	mustExecute("DROP DYNAMIC TABLE current_values")
	mustExecute("DROP DYNAMIC TABLE IF EXISTS current_values")
	if _, err := repo.GetDynamicTableByName(ctx, schema.ID, "CURRENT_VALUES"); err == nil {
		t.Fatal("metadata remains after drop")
	}
}

func TestExecutor_DynamicTableValidationAndQualifiedNames(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	executor.Configure(WithWarehouseValidator(func(_ context.Context, name string) error {
		if name == "VALID_WH" {
			return nil
		}
		return fmt.Errorf("not found")
	}))
	ctx := context.Background()
	for _, name := range []string{"SOURCE_DB", "TARGET_DB"} {
		if _, err := repo.CreateDatabase(ctx, name, ""); err != nil {
			t.Fatal(err)
		}
	}
	sourceContext := ExecutionContext{Database: "SOURCE_DB", Schema: "PUBLIC"}
	if _, err := executor.ExecuteWithContext(ctx, sourceContext, "CREATE TABLE values_table (value INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, sourceContext, "INSERT INTO values_table VALUES (7)"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, sourceContext, "CREATE VIEW occupied_view AS SELECT 1 AS n"); err != nil {
		t.Fatal(err)
	}
	statement := "CREATE DYNAMIC TABLE TARGET_DB.PUBLIC.summary TARGET_LAG = 'DOWNSTREAM' WAREHOUSE = VALID_WH AS SELECT value FROM SOURCE_DB.PUBLIC.values_table"
	if _, err := executor.ExecuteWithContext(ctx, sourceContext, statement); err != nil {
		t.Fatal(err)
	}
	result, err := executor.QueryWithContext(ctx, ExecutionContext{Database: "TARGET_DB", Schema: "PUBLIC"}, "SELECT * FROM summary")
	if err != nil || len(result.Rows) != 1 {
		t.Fatalf("qualified result = %#v, %v", result, err)
	}
	targetContext := ExecutionContext{Database: "TARGET_DB", Schema: "PUBLIC"}
	if _, err := executor.ExecuteWithContext(ctx, targetContext, "CREATE DYNAMIC TABLE PUBLIC.two_part TARGET_LAG = '2 HOURS' WAREHOUSE = VALID_WH AS SELECT value FROM SOURCE_DB.PUBLIC.values_table"); err != nil {
		t.Fatalf("two-part CREATE DYNAMIC TABLE: %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, targetContext, "ALTER DYNAMIC TABLE PUBLIC.two_part REFRESH"); err != nil {
		t.Fatalf("two-part ALTER DYNAMIC TABLE: %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, sourceContext, "CREATE DYNAMIC TABLE TARGET_DB.PUBLIC.caller_context TARGET_LAG = '1 MINUTE' WAREHOUSE = VALID_WH AS SELECT value FROM values_table"); err != nil {
		t.Fatalf("qualified target with caller-context source: %v", err)
	}
	callerResult, err := executor.QueryWithContext(ctx, targetContext, "SELECT * FROM caller_context")
	if err != nil || len(callerResult.Rows) != 1 {
		t.Fatalf("caller-context rows = %#v, %v", callerResult, err)
	}

	tests := []string{
		"CREATE DYNAMIC TABLE bad TARGET_LAG = '1 MINUTE' WAREHOUSE = MISSING AS SELECT 1",
		"CREATE DYNAMIC TABLE bad_lag TARGET_LAG = 'whenever' WAREHOUSE = VALID_WH AS SELECT 1",
		"CREATE DYNAMIC TABLE missing_options AS SELECT 1",
		"ALTER DYNAMIC TABLE missing REFRESH",
		"DROP DYNAMIC TABLE missing",
		"CREATE DYNAMIC TABLE values_table TARGET_LAG = '1 MINUTE' WAREHOUSE = VALID_WH AS SELECT 1",
		"CREATE OR REPLACE DYNAMIC TABLE occupied_view TARGET_LAG = '1 MINUTE' WAREHOUSE = VALID_WH AS SELECT 1",
	}
	for _, sql := range tests {
		if _, err := executor.ExecuteWithContext(ctx, sourceContext, sql); err == nil {
			t.Errorf("%s unexpectedly succeeded", sql)
		}
	}
	if _, err := executor.QueryWithContext(ctx, sourceContext, "SHOW DYNAMIC TABLES LIKE 'X%'"); err == nil {
		t.Fatal("unsupported SHOW DYNAMIC TABLES variant succeeded")
	}
}

func TestExecutor_DynamicTableRejectsOrdinaryMutations(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	executor.Configure(WithWarehouseValidator(func(context.Context, string) error { return nil }))
	ctx := context.Background()
	database, _ := repo.CreateDatabase(ctx, "MUTATION_DB", "")
	executionContext := ExecutionContext{Database: database.Name, Schema: "PUBLIC"}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE DYNAMIC TABLE snapshot TARGET_LAG = '1 MINUTE' WAREHOUSE = WH AS SELECT 1 AS id"); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		"INSERT INTO snapshot VALUES (2)", "UPDATE snapshot SET id = 2", "DELETE FROM snapshot",
		"TRUNCATE TABLE snapshot", "ALTER TABLE snapshot ADD COLUMN x INTEGER", "DROP TABLE snapshot",
		"CREATE OR REPLACE TABLE snapshot AS SELECT 2 AS id", "MERGE INTO snapshot USING snapshot s ON FALSE WHEN NOT MATCHED THEN INSERT VALUES (2)",
		"COPY INTO snapshot FROM @missing_stage",
		"WITH incoming AS (SELECT 2 AS id) INSERT INTO snapshot SELECT id FROM incoming",
		"WITH replacement AS (SELECT 2 AS id) UPDATE snapshot SET id = (SELECT id FROM replacement)",
		"WITH doomed AS (SELECT 1 AS id) DELETE FROM snapshot WHERE id IN (SELECT id FROM doomed)",
		"WITH incoming AS (SELECT 2 AS id) MERGE INTO snapshot AS target USING incoming ON target.id = incoming.id WHEN NOT MATCHED THEN INSERT (id) VALUES (incoming.id)",
	}
	for _, statement := range statements {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err == nil {
			t.Errorf("ordinary mutation succeeded: %s", statement)
		}
	}
	if result, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM snapshot"); err != nil || len(result.Rows) != 1 {
		t.Fatalf("SELECT after rejected mutations = %#v, %v", result, err)
	}
	selectResult, err := executor.QueryWithContext(ctx, executionContext, "WITH current_rows AS (SELECT * FROM snapshot) SELECT * FROM current_rows")
	if err != nil || len(selectResult.Rows) != 1 {
		t.Fatalf("CTE SELECT was affected: %#v, %v", selectResult, err)
	}
}

func TestExecutor_CTEPrefixedMutationsStillWorkForOrdinaryTables(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	database, _ := repo.CreateDatabase(ctx, "CTE_DML_DB", "")
	executionContext := ExecutionContext{Database: database.Name, Schema: "PUBLIC"}
	tests := []struct{ name, statement string }{
		{"insert", "WITH incoming AS (SELECT 2 AS id) INSERT INTO ordinary_insert SELECT id FROM incoming"},
		{"update", "WITH replacement AS (SELECT 2 AS id) UPDATE ordinary_update SET id = (SELECT id FROM replacement)"},
		{"delete", "WITH doomed AS (SELECT 1 AS id) DELETE FROM ordinary_delete WHERE id IN (SELECT id FROM doomed)"},
		{"merge", "WITH incoming AS (SELECT 2 AS id) MERGE INTO ordinary_merge AS target USING incoming ON target.id = incoming.id WHEN NOT MATCHED THEN INSERT (id) VALUES (incoming.id)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			table := "ordinary_" + test.name
			if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE "+table+" (id INTEGER)"); err != nil {
				t.Fatal(err)
			}
			if _, err := executor.ExecuteWithContext(ctx, executionContext, "INSERT INTO "+table+" VALUES (1)"); err != nil {
				t.Fatal(err)
			}
			if _, err := executor.ExecuteWithContext(ctx, executionContext, test.statement); err != nil {
				t.Fatalf("ordinary CTE mutation failed: %v", err)
			}
		})
	}
}

func TestExecutor_DynamicTableFailurePreservesPreviousState(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	executor.Configure(WithWarehouseValidator(func(context.Context, string) error { return nil }))
	ctx := context.Background()
	database, _ := repo.CreateDatabase(ctx, "SAFE_DB", "")
	schema, _ := repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	executionContext := ExecutionContext{Database: database.Name, Schema: schema.Name}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE source_data (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "INSERT INTO source_data VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	create := "CREATE DYNAMIC TABLE safe_snapshot TARGET_LAG = '1 MINUTE' WAREHOUSE = WH AS SELECT id FROM source_data"
	if _, err := executor.ExecuteWithContext(ctx, executionContext, create); err != nil {
		t.Fatal(err)
	}
	before, _ := repo.GetDynamicTableByName(ctx, schema.ID, "SAFE_SNAPSHOT")
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP TABLE source_data"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "ALTER DYNAMIC TABLE safe_snapshot REFRESH"); err == nil {
		t.Fatal("failed refresh succeeded")
	}
	afterRefresh, _ := repo.GetDynamicTableByName(ctx, schema.ID, "SAFE_SNAPSHOT")
	if afterRefresh.Definition != before.Definition || !afterRefresh.LastRefreshedAt.Equal(*before.LastRefreshedAt) {
		t.Fatal("failed refresh changed metadata")
	}
	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM safe_snapshot")
	if err != nil || len(result.Rows) != 1 {
		t.Fatalf("failed refresh lost prior rows: %#v, %v", result, err)
	}
	badReplace := "CREATE OR REPLACE DYNAMIC TABLE safe_snapshot TARGET_LAG = '2 HOURS' WAREHOUSE = WH AS SELECT * FROM missing_source"
	if _, err := executor.ExecuteWithContext(ctx, executionContext, badReplace); err == nil {
		t.Fatal("failed replacement succeeded")
	}
	afterReplace, _ := repo.GetDynamicTableByName(ctx, schema.ID, "SAFE_SNAPSHOT")
	if afterReplace.TargetLag != before.TargetLag || afterReplace.Definition != before.Definition {
		t.Fatal("failed replacement changed definition metadata")
	}
	result, err = executor.QueryWithContext(ctx, executionContext, "SELECT * FROM safe_snapshot")
	if err != nil || len(result.Rows) != 1 {
		t.Fatalf("failed replacement lost prior object: %#v, %v", result, err)
	}
}

func TestExecutor_DynamicTableConcurrentRefreshesAreSerialized(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	executor.Configure(WithWarehouseValidator(func(context.Context, string) error { return nil }))
	ctx := context.Background()
	database, _ := repo.CreateDatabase(ctx, "CONCURRENT_DB", "")
	executionContext := ExecutionContext{Database: database.Name, Schema: "PUBLIC"}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE DYNAMIC TABLE snapshot TARGET_LAG = '1 MINUTE' WAREHOUSE = WH AS SELECT 1 AS id"); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := executor.ExecuteWithContext(ctx, executionContext, "ALTER DYNAMIC TABLE snapshot REFRESH")
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Errorf("concurrent refresh: %v", err)
		}
	}
}

func TestExecutor_DynamicTableMetadataFailureRollsBackPhysicalCreation(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	executor.Configure(WithWarehouseValidator(func(context.Context, string) error { return nil }))
	ctx := context.Background()
	database, _ := repo.CreateDatabase(ctx, "ROLLBACK_DB", "")
	schema, _ := repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	executionContext := ExecutionContext{Database: database.Name, Schema: schema.Name}
	if _, err := executor.mgr.Exec(ctx, "DROP TABLE _metadata_dynamic_tables"); err != nil {
		t.Fatal(err)
	}
	statement := "CREATE DYNAMIC TABLE should_rollback TARGET_LAG = '1 MINUTE' WAREHOUSE = WH AS SELECT 1 AS id"
	if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err == nil {
		t.Fatal("metadata failure unexpectedly succeeded")
	}
	if _, err := repo.GetTableByName(ctx, schema.ID, "SHOULD_ROLLBACK"); err == nil {
		t.Fatal("table catalog record survived rollback")
	}
	var count int
	if err := executor.mgr.QueryRow(ctx, `SELECT COUNT(*) FROM duckdb_tables() WHERE schema_name = 'ROLLBACK_DB' AND table_name = 'PUBLIC_SHOULD_ROLLBACK'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("physical table survived metadata rollback")
	}
}

func TestClassifier_DynamicTables(t *testing.T) {
	classifier := NewClassifier()
	tests := []struct {
		sql   string
		check func(string) bool
	}{
		{"CREATE DYNAMIC TABLE d TARGET_LAG = '1 MINUTE' WAREHOUSE = w AS SELECT 1", classifier.IsCreateDynamicTable},
		{"CREATE OR REPLACE DYNAMIC TABLE d TARGET_LAG = '1 MINUTE' WAREHOUSE = w AS SELECT 1", classifier.IsCreateDynamicTable},
		{"ALTER DYNAMIC TABLE d REFRESH", classifier.IsAlterDynamicTable},
		{"DROP DYNAMIC TABLE IF EXISTS d", classifier.IsDropDynamicTable},
		{"SHOW DYNAMIC TABLES", classifier.IsShowDynamicTables},
	}
	for _, test := range tests {
		if !test.check(test.sql) {
			t.Errorf("failed to classify %q", test.sql)
		}
	}
}

func TestExecutor_DynamicTablePersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dynamic.db")
	open := func() (*sql.DB, *Executor, *metadata.Repository) {
		db, err := sql.Open("duckdb", path)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		manager := connection.NewManager(db)
		repo, err := metadata.NewRepository(manager)
		if err != nil {
			t.Fatal(err)
		}
		executor := NewExecutor(manager, repo)
		return db, executor, repo
	}
	db, executor, repo := open()
	executor.Configure(WithWarehouseValidator(func(context.Context, string) error { return nil }))
	database, err := repo.CreateDatabase(ctx, "SOURCE_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if _, err := repo.CreateDatabase(ctx, "TARGET_DB", ""); err != nil {
		t.Fatal(err)
	}
	executionContext := ExecutionContext{Database: database.Name, Schema: schema.Name}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE source_values (answer INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "INSERT INTO source_values VALUES (42)"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE DYNAMIC TABLE TARGET_DB.PUBLIC.persisted TARGET_LAG = '1 HOUR' WAREHOUSE = COMPUTE_WH AS SELECT answer FROM source_values"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, executor, repo = open()
	defer func() { _ = db.Close() }()
	targetDatabase, _ := repo.GetDatabaseByName(ctx, "TARGET_DB")
	targetSchema, _ := repo.GetSchemaByName(ctx, targetDatabase.ID, "PUBLIC")
	dynamic, err := repo.GetDynamicTableByName(ctx, targetSchema.ID, "PERSISTED")
	if err != nil || dynamic.Definition != "SELECT answer FROM source_values" || dynamic.DefinitionDatabase != "SOURCE_DB" {
		t.Fatalf("persisted metadata = %#v, %v", dynamic, err)
	}
	targetContext := ExecutionContext{Database: "TARGET_DB", Schema: "PUBLIC"}
	result, err := executor.QueryWithContext(ctx, targetContext, "SELECT * FROM persisted")
	if err != nil || len(result.Rows) != 1 {
		t.Fatalf("persisted materialization = %#v, %v", result, err)
	}
	if _, err := executor.ExecuteWithContext(ctx, targetContext, "ALTER DYNAMIC TABLE persisted REFRESH"); err == nil {
		t.Fatal("refresh succeeded before the in-memory warehouse was recreated")
	}
	executor.Configure(WithWarehouseValidator(func(_ context.Context, name string) error {
		if name != "COMPUTE_WH" {
			return fmt.Errorf("not found")
		}
		return nil
	}))
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "INSERT INTO source_values VALUES (43)"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, targetContext, "ALTER DYNAMIC TABLE persisted REFRESH"); err != nil {
		t.Fatalf("refresh after restart: %v", err)
	}
	result, err = executor.QueryWithContext(ctx, targetContext, "SELECT * FROM persisted ORDER BY answer")
	if err != nil || len(result.Rows) != 2 {
		t.Fatalf("persisted source context refresh = %#v, %v", result, err)
	}
}
