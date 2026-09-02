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

func TestProcedureSupportsDeclareAssignmentCaseAndIf(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "SCRIPTING_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.CreateSchema(ctx, database.ID, "PUBLIC", ""); err != nil {
		t.Fatalf("CreateSchema() error = %v", err)
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

func TestParseProcedureScriptRejectsMalformedBlocks(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing begin", body: "RETURN 'bad'; END"},
		{name: "missing end if", body: "BEGIN IF (TRUE) THEN RETURN 'bad'; END"},
		{name: "missing end case", body: "BEGIN CASE (1) WHEN 1 THEN RETURN 'bad'; END"},
		{name: "default without expression", body: "DECLARE value VARCHAR DEFAULT; BEGIN RETURN value; END"},
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
	if _, err := repo.CreateSchema(ctx, database.ID, "PUBLIC", ""); err != nil {
		t.Fatalf("CreateSchema() error = %v", err)
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
