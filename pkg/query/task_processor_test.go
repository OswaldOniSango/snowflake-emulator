package query

import (
	"context"
	"strings"
	"testing"

	"github.com/nnnkkk7/snowflake-emulator/pkg/warehouse"
)

func setupTaskTest(t *testing.T) (*Executor, context.Context, ExecutionContext) {
	t.Helper()
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	database, err := repo.CreateDatabase(ctx, "TASK_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	warehouseManager := warehouse.NewManager()
	if _, err := warehouseManager.CreateWarehouse(ctx, "TASK_WH", "X-SMALL", ""); err != nil {
		t.Fatalf("CreateWarehouse() error = %v", err)
	}
	executor.Configure(WithWarehouseValidator(func(ctx context.Context, name string) error {
		_, err := warehouseManager.GetWarehouse(ctx, name)
		return err
	}))
	return executor, ctx, ExecutionContext{Database: "TASK_DB", Schema: "PUBLIC"}
}

func TestTaskLifecycleAndManualExecution(t *testing.T) {
	executor, ctx, executionContext := setupTaskTest(t)
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE task_log (message VARCHAR)"); err != nil {
		t.Fatalf("CREATE TABLE error = %v", err)
	}
	create := `CREATE TASK log_task WAREHOUSE = TASK_WH SCHEDULE = '1 MINUTE' AS INSERT INTO task_log VALUES ('executed')`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, create); err != nil {
		t.Fatalf("CREATE TASK error = %v", err)
	}
	shown, err := executor.QueryWithContext(ctx, executionContext, "SHOW TASKS")
	if err != nil {
		t.Fatalf("SHOW TASKS error = %v", err)
	}
	if len(shown.Rows) != 1 || shown.Rows[0][1] != "LOG_TASK" || shown.Rows[0][6] != "SUSPENDED" {
		t.Fatalf("SHOW TASKS rows = %#v", shown.Rows)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "ALTER TASK log_task RESUME"); err != nil {
		t.Fatalf("ALTER TASK RESUME error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "EXECUTE TASK log_task"); err != nil {
		t.Fatalf("EXECUTE TASK error = %v", err)
	}
	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT message FROM task_log")
	if err != nil || len(result.Rows) != 1 || result.Rows[0][0] != "executed" {
		t.Fatalf("task effect result = %#v, error = %v", result, err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "ALTER TASK log_task SUSPEND"); err != nil {
		t.Fatalf("ALTER TASK SUSPEND error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP TASK log_task"); err != nil {
		t.Fatalf("DROP TASK error = %v", err)
	}
}

func TestTaskExecutesProcedureUsingTaskContext(t *testing.T) {
	executor, ctx, executionContext := setupTaskTest(t)
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE procedure_log (message VARCHAR)"); err != nil {
		t.Fatalf("CREATE TABLE error = %v", err)
	}
	procedure := `CREATE PROCEDURE write_log()
		RETURNS VARCHAR LANGUAGE SQL AS $$
		BEGIN
			INSERT INTO procedure_log VALUES ('called by task');
			RETURN 'ok';
		END
		$$`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, procedure); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}
	task := `CREATE TASK procedure_task WAREHOUSE = TASK_WH SCHEDULE = '5 MINUTES' AS CALL write_log()`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, task); err != nil {
		t.Fatalf("CREATE TASK error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "EXECUTE TASK procedure_task"); err != nil {
		t.Fatalf("EXECUTE TASK error = %v", err)
	}
	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT message FROM procedure_log")
	if err != nil || len(result.Rows) != 1 || result.Rows[0][0] != "called by task" {
		t.Fatalf("procedure task result = %#v, error = %v", result, err)
	}
}

func TestTaskRecordsProcedureFailure(t *testing.T) {
	executor, ctx, executionContext := setupTaskTest(t)
	task := `CREATE TASK broken_task WAREHOUSE = TASK_WH SCHEDULE = '1 MINUTE' AS CALL missing_procedure()`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, task); err != nil {
		t.Fatalf("CREATE TASK error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "EXECUTE TASK broken_task"); err == nil {
		t.Fatal("EXECUTE TASK returned nil error")
	}
	shown, err := executor.QueryWithContext(ctx, executionContext, "SHOW TASKS")
	if err != nil {
		t.Fatalf("SHOW TASKS error = %v", err)
	}
	lastError, ok := shown.Rows[0][10].(string)
	if !ok || !strings.Contains(lastError, "MISSING_PROCEDURE") {
		t.Fatalf("recorded last_error = %#v", shown.Rows[0][10])
	}
}

func TestCreateTaskRejectsMissingWarehouse(t *testing.T) {
	executor, ctx, executionContext := setupTaskTest(t)
	_, err := executor.ExecuteWithContext(ctx, executionContext, `CREATE TASK bad_task WAREHOUSE = MISSING_WH SCHEDULE = '1 MINUTE' AS CALL anything()`)
	if err == nil || !strings.Contains(err.Error(), "warehouse MISSING_WH not found") {
		t.Fatalf("CREATE TASK error = %v", err)
	}
}

func TestTaskConsumesStream(t *testing.T) {
	executor, ctx, executionContext := setupTaskTest(t)
	statements := []string{
		"CREATE TABLE events (id INTEGER, message VARCHAR)",
		"CREATE TABLE processed_events (id INTEGER, message VARCHAR)",
		"CREATE STREAM events_stream ON TABLE events",
		"INSERT INTO events VALUES (1, 'from stream')",
		`CREATE TASK consume_task WAREHOUSE = TASK_WH SCHEDULE = '1 MINUTE' AS INSERT INTO processed_events SELECT id, message FROM events_stream`,
		"EXECUTE TASK consume_task",
	}
	for _, statement := range statements {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
			t.Fatalf("statement %q error = %v", statement, err)
		}
	}
	processed, err := executor.QueryWithContext(ctx, executionContext, "SELECT id, message FROM processed_events")
	if err != nil || len(processed.Rows) != 1 || processed.Rows[0][0] != int32(1) {
		t.Fatalf("processed rows = %#v, error = %v", processed, err)
	}
	pending, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM events_stream")
	if err != nil || len(pending.Rows) != 0 {
		t.Fatalf("stream rows after task = %#v, error = %v", pending, err)
	}
}
