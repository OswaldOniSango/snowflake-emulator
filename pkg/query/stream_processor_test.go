package query

import (
	"context"
	"testing"
)

func setupStreamTest(t *testing.T) (*Executor, context.Context) {
	t.Helper()
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "STREAM_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	if _, err := executor.Execute(ctx, "CREATE TABLE STREAM_DB.PUBLIC_EVENTS (ID INTEGER, MESSAGE VARCHAR)"); err != nil {
		t.Fatalf("CREATE TABLE error = %v", err)
	}
	return executor, ctx
}

func TestStreamReturnsRowsInsertedAfterCreation(t *testing.T) {
	executor, ctx := setupStreamTest(t)

	if _, err := executor.Execute(ctx, "INSERT INTO STREAM_DB.PUBLIC_EVENTS VALUES (1, 'before stream')"); err != nil {
		t.Fatalf("initial INSERT error = %v", err)
	}
	if _, err := executor.Execute(ctx, "CREATE STREAM STREAM_DB.PUBLIC.EVENTS_STREAM ON TABLE STREAM_DB.PUBLIC.EVENTS APPEND_ONLY = TRUE"); err != nil {
		t.Fatalf("CREATE STREAM error = %v", err)
	}
	if _, err := executor.Execute(ctx, "INSERT INTO STREAM_DB.PUBLIC_EVENTS VALUES (2, 'first change'), (3, 'second change')"); err != nil {
		t.Fatalf("change INSERT error = %v", err)
	}

	result, err := executor.Query(ctx, "SELECT * FROM STREAM_DB.PUBLIC.EVENTS_STREAM ORDER BY ID")
	if err != nil {
		t.Fatalf("stream SELECT error = %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("stream returned %d rows, want 2: %#v", len(result.Rows), result.Rows)
	}
	if result.Rows[0][0] != int32(2) || result.Rows[0][2] != "INSERT" || result.Rows[0][3] != false {
		t.Fatalf("first stream row = %#v", result.Rows[0])
	}
}

func TestStreamCanFeedInsertSelect(t *testing.T) {
	executor, ctx := setupStreamTest(t)

	if _, err := executor.Execute(ctx, "CREATE TABLE STREAM_DB.PUBLIC_PROCESSED_EVENTS (ID INTEGER, MESSAGE VARCHAR)"); err != nil {
		t.Fatalf("CREATE destination TABLE error = %v", err)
	}
	if _, err := executor.Execute(ctx, "CREATE STREAM STREAM_DB.PUBLIC.EVENTS_STREAM ON TABLE STREAM_DB.PUBLIC.EVENTS"); err != nil {
		t.Fatalf("CREATE STREAM error = %v", err)
	}
	if _, err := executor.Execute(ctx, "INSERT INTO STREAM_DB.PUBLIC_EVENTS VALUES (7, 'process me')"); err != nil {
		t.Fatalf("source INSERT error = %v", err)
	}
	if _, err := executor.Execute(ctx, "INSERT INTO STREAM_DB.PUBLIC_PROCESSED_EVENTS SELECT ID, MESSAGE FROM STREAM_DB.PUBLIC.EVENTS_STREAM"); err != nil {
		t.Fatalf("stream INSERT SELECT error = %v", err)
	}

	result, err := executor.Query(ctx, "SELECT ID, MESSAGE FROM STREAM_DB.PUBLIC_PROCESSED_EVENTS")
	if err != nil {
		t.Fatalf("destination SELECT error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != int32(7) {
		t.Fatalf("destination rows = %#v", result.Rows)
	}

	consumed, err := executor.Query(ctx, "SELECT * FROM STREAM_DB.PUBLIC.EVENTS_STREAM")
	if err != nil {
		t.Fatalf("consumed stream SELECT error = %v", err)
	}
	if len(consumed.Rows) != 0 {
		t.Fatalf("consumed stream returned rows = %#v, want empty", consumed.Rows)
	}
	shown, err := executor.Query(ctx, "SHOW STREAMS")
	if err != nil {
		t.Fatalf("SHOW STREAMS after consumption error = %v", err)
	}
	if len(shown.Rows) != 1 || shown.Rows[0][4].(int64) < 0 {
		t.Fatalf("SHOW STREAMS offset after consumption = %#v", shown.Rows)
	}

	if _, err := executor.Execute(ctx, "INSERT INTO STREAM_DB.PUBLIC_EVENTS VALUES (8, 'next change')"); err != nil {
		t.Fatalf("next source INSERT error = %v", err)
	}
	next, err := executor.Query(ctx, "SELECT ID FROM STREAM_DB.PUBLIC.EVENTS_STREAM")
	if err != nil {
		t.Fatalf("next stream SELECT error = %v", err)
	}
	if len(next.Rows) != 1 || next.Rows[0][0] != int32(8) {
		t.Fatalf("next stream rows = %#v", next.Rows)
	}
}

func TestStreamSelectDoesNotAdvanceOffset(t *testing.T) {
	executor, ctx := setupStreamTest(t)
	if _, err := executor.Execute(ctx, "CREATE STREAM STREAM_DB.PUBLIC.EVENTS_STREAM ON TABLE STREAM_DB.PUBLIC.EVENTS"); err != nil {
		t.Fatalf("CREATE STREAM error = %v", err)
	}
	if _, err := executor.Execute(ctx, "INSERT INTO STREAM_DB.PUBLIC_EVENTS VALUES (1, 'still pending')"); err != nil {
		t.Fatalf("source INSERT error = %v", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		result, err := executor.Query(ctx, "SELECT ID FROM STREAM_DB.PUBLIC.EVENTS_STREAM")
		if err != nil {
			t.Fatalf("stream SELECT %d error = %v", attempt, err)
		}
		if len(result.Rows) != 1 || result.Rows[0][0] != int32(1) {
			t.Fatalf("stream SELECT %d rows = %#v", attempt, result.Rows)
		}
	}
}

func TestFailedDMLDoesNotAdvanceStreamOffset(t *testing.T) {
	executor, ctx := setupStreamTest(t)
	if _, err := executor.Execute(ctx, "CREATE STREAM STREAM_DB.PUBLIC.EVENTS_STREAM ON TABLE STREAM_DB.PUBLIC.EVENTS"); err != nil {
		t.Fatalf("CREATE STREAM error = %v", err)
	}
	if _, err := executor.Execute(ctx, "INSERT INTO STREAM_DB.PUBLIC_EVENTS VALUES (3, 'retry me')"); err != nil {
		t.Fatalf("source INSERT error = %v", err)
	}
	if _, err := executor.Execute(ctx, "INSERT INTO STREAM_DB.PUBLIC.MISSING_TABLE SELECT ID, MESSAGE FROM STREAM_DB.PUBLIC.EVENTS_STREAM"); err == nil {
		t.Fatal("invalid consuming DML returned nil error")
	}
	result, err := executor.Query(ctx, "SELECT ID FROM STREAM_DB.PUBLIC.EVENTS_STREAM")
	if err != nil {
		t.Fatalf("stream SELECT error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != int32(3) {
		t.Fatalf("stream rows after failed DML = %#v", result.Rows)
	}
}

func TestStreamShowAndDrop(t *testing.T) {
	executor, ctx := setupStreamTest(t)

	if _, err := executor.Execute(ctx, "CREATE STREAM STREAM_DB.PUBLIC.EVENTS_STREAM ON TABLE STREAM_DB.PUBLIC.EVENTS"); err != nil {
		t.Fatalf("CREATE STREAM error = %v", err)
	}
	shown, err := executor.Query(ctx, "SHOW STREAMS")
	if err != nil {
		t.Fatalf("SHOW STREAMS error = %v", err)
	}
	if len(shown.Rows) != 1 || shown.Rows[0][1] != "EVENTS_STREAM" {
		t.Fatalf("SHOW STREAMS rows = %#v", shown.Rows)
	}
	if _, err := executor.Execute(ctx, "DROP STREAM STREAM_DB.PUBLIC.EVENTS_STREAM"); err != nil {
		t.Fatalf("DROP STREAM error = %v", err)
	}
	if _, err := executor.Query(ctx, "SELECT * FROM STREAM_DB.PUBLIC.EVENTS_STREAM"); err == nil {
		t.Fatal("stream SELECT after DROP returned nil error")
	}
}

func TestStreamUsesExecutionContextForShortNames(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "LEARNING_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "LEARNING_DB", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE TABLE users (ID INTEGER, NAME VARCHAR)"); err != nil {
		t.Fatalf("short CREATE TABLE error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE STREAM users_stream ON TABLE users"); err != nil {
		t.Fatalf("short CREATE STREAM error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "INSERT INTO users VALUES (1, 'Oswaldo')"); err != nil {
		t.Fatalf("short INSERT error = %v", err)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT * FROM users_stream")
	if err != nil {
		t.Fatalf("short stream SELECT error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != int32(1) || result.Rows[0][1] != "Oswaldo" {
		t.Fatalf("short stream SELECT rows = %#v", result.Rows)
	}
}
