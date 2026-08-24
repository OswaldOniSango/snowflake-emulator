package query

import (
	"context"
	"testing"
)

func TestProcedureLifecycle(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "LESSON_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.CreateSchema(ctx, database.ID, "PUBLIC", ""); err != nil {
		t.Fatalf("CreateSchema() error = %v", err)
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
	if _, err := repo.CreateSchema(ctx, database.ID, "PUBLIC", ""); err != nil {
		t.Fatalf("CreateSchema() error = %v", err)
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
	if _, err := repo.CreateSchema(ctx, database.ID, "PUBLIC", ""); err != nil {
		t.Fatalf("CreateSchema() error = %v", err)
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
