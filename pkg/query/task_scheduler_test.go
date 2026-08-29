package query

import (
	"testing"
	"time"
)

func TestParseTaskSchedule(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{input: "1 SECOND", want: time.Second},
		{input: "2 seconds", want: 2 * time.Second},
		{input: "5 MINUTES", want: 5 * time.Minute},
		{input: "3 HOURS", want: 3 * time.Hour},
	}
	for _, tt := range tests {
		got, err := parseTaskSchedule(tt.input)
		if err != nil || got != tt.want {
			t.Fatalf("parseTaskSchedule(%q) = %v, %v; want %v", tt.input, got, err, tt.want)
		}
	}
	for _, invalid := range []string{"", "0 MINUTES", "USING CRON * * * * * UTC", "1 DAY"} {
		if _, err := parseTaskSchedule(invalid); err == nil {
			t.Fatalf("parseTaskSchedule(%q) returned nil error", invalid)
		}
	}
}

func TestTaskSchedulerExecutesOnlyDueStartedTasks(t *testing.T) {
	executor, ctx, executionContext := setupTaskTest(t)
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE schedule_log (message VARCHAR)"); err != nil {
		t.Fatalf("CREATE TABLE error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, `CREATE TASK scheduled_task WAREHOUSE = TASK_WH SCHEDULE = '1 SECOND' AS INSERT INTO schedule_log VALUES ('automatic')`); err != nil {
		t.Fatalf("CREATE TASK error = %v", err)
	}
	scheduler := NewTaskScheduler(executor.repo, executor, time.Second)
	if err := scheduler.RunDueTasks(ctx, time.Now().Add(2*time.Second)); err != nil {
		t.Fatalf("RunDueTasks() for suspended task error = %v", err)
	}
	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM schedule_log")
	if err != nil || len(result.Rows) != 0 {
		t.Fatalf("suspended task rows = %#v, error = %v", result, err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "ALTER TASK scheduled_task RESUME"); err != nil {
		t.Fatalf("ALTER TASK RESUME error = %v", err)
	}
	if err := scheduler.RunDueTasks(ctx, time.Now().Add(2*time.Second)); err != nil {
		t.Fatalf("RunDueTasks() error = %v", err)
	}
	result, err = executor.QueryWithContext(ctx, executionContext, "SELECT * FROM schedule_log")
	if err != nil || len(result.Rows) != 1 || result.Rows[0][0] != "automatic" {
		t.Fatalf("scheduled task rows = %#v, error = %v", result, err)
	}
}

func TestTaskSchedulerCallsProcedure(t *testing.T) {
	executor, ctx, executionContext := setupTaskTest(t)
	statements := []string{
		"CREATE TABLE scheduled_procedure_log (message VARCHAR)",
		`CREATE PROCEDURE scheduled_write() RETURNS VARCHAR LANGUAGE SQL AS $$ BEGIN INSERT INTO scheduled_procedure_log VALUES ('scheduled call'); RETURN 'ok'; END $$`,
		`CREATE TASK scheduled_procedure_task WAREHOUSE = TASK_WH SCHEDULE = '1 SECOND' AS CALL scheduled_write()`,
		"ALTER TASK scheduled_procedure_task RESUME",
	}
	for _, statement := range statements {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
			t.Fatalf("statement %q error = %v", statement, err)
		}
	}
	scheduler := NewTaskScheduler(executor.repo, executor, time.Second)
	if err := scheduler.RunDueTasks(ctx, time.Now().Add(2*time.Second)); err != nil {
		t.Fatalf("RunDueTasks() error = %v", err)
	}
	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM scheduled_procedure_log")
	if err != nil || len(result.Rows) != 1 || result.Rows[0][0] != "scheduled call" {
		t.Fatalf("scheduled procedure rows = %#v, error = %v", result, err)
	}
}

func TestCreateTaskRejectsUnsupportedSchedule(t *testing.T) {
	executor, ctx, executionContext := setupTaskTest(t)
	_, err := executor.ExecuteWithContext(ctx, executionContext, `CREATE TASK cron_task WAREHOUSE = TASK_WH SCHEDULE = 'USING CRON * * * * * UTC' AS CALL anything()`)
	if err == nil {
		t.Fatal("CREATE TASK with unsupported schedule returned nil error")
	}
}
