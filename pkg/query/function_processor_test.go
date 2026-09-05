package query

import (
	"context"
	"testing"
)

func TestFunctionLifecycle(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "FUNC_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "FUNC_DB", Schema: "PUBLIC"}

	createSQL := `CREATE FUNCTION area(width FLOAT, height FLOAT)
		RETURNS FLOAT
		LANGUAGE SQL
		AS $$
		width * height
		$$`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, createSQL); err != nil {
		t.Fatalf("CREATE FUNCTION error = %v", err)
	}

	// Called unqualified, the same way a table would be — resolved through
	// the execution context rather than a real DuckDB search path.
	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT area(3, 4) AS a")
	if err != nil {
		t.Fatalf("SELECT area() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != int32(12) {
		t.Fatalf("SELECT area() rows = %#v, want 12", result.Rows)
	}

	shown, err := executor.QueryWithContext(ctx, executionContext, "SHOW FUNCTIONS")
	if err != nil {
		t.Fatalf("SHOW FUNCTIONS error = %v", err)
	}
	if len(shown.Rows) != 1 || shown.Rows[0][1] != "AREA" {
		t.Fatalf("SHOW FUNCTIONS result = %#v", shown.Rows)
	}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP FUNCTION area(FLOAT, FLOAT)"); err != nil {
		t.Fatalf("DROP FUNCTION error = %v", err)
	}
	if _, err := executor.QueryWithContext(ctx, executionContext, "SELECT area(3, 4)"); err == nil {
		t.Fatal("SELECT area() after DROP returned nil error")
	}
}

func TestFunctionUsableInWhereClause(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "FUNC_WHERE_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "FUNC_WHERE_DB", Schema: "PUBLIC"}

	for _, statement := range []string{
		"CREATE FUNCTION is_even(n INTEGER) RETURNS BOOLEAN AS $$ n % 2 = 0 $$",
		"CREATE TABLE nums (n INTEGER)",
		"INSERT INTO nums VALUES (1), (2), (3), (4)",
	} {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
			t.Fatalf("setup statement %q error = %v", statement, err)
		}
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT n FROM nums WHERE is_even(n) ORDER BY n")
	if err != nil {
		t.Fatalf("SELECT ... WHERE is_even(n) error = %v", err)
	}
	if len(result.Rows) != 2 || result.Rows[0][0] != int32(2) || result.Rows[1][0] != int32(4) {
		t.Fatalf("rows = %#v, want [2, 4]", result.Rows)
	}
}

func TestFunctionCanCallAnotherFunction(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "FUNC_NEST_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "FUNC_NEST_DB", Schema: "PUBLIC"}

	for _, statement := range []string{
		"CREATE FUNCTION double_it(n INTEGER) RETURNS INTEGER AS $$ n * 2 $$",
		"CREATE FUNCTION quadruple_it(n INTEGER) RETURNS INTEGER AS $$ double_it(double_it(n)) $$",
	} {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
			t.Fatalf("setup statement %q error = %v", statement, err)
		}
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT quadruple_it(5) AS q")
	if err != nil {
		t.Fatalf("SELECT quadruple_it(5) error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != int32(20) {
		t.Fatalf("rows = %#v, want 20", result.Rows)
	}
}

func TestFunctionUsableFromInsideAProcedure(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "FUNC_PROC_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "FUNC_PROC_DB", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE FUNCTION greeting(name VARCHAR) RETURNS VARCHAR AS $$ 'hello ' || name $$"); err != nil {
		t.Fatalf("CREATE FUNCTION error = %v", err)
	}

	createSQL := `CREATE PROCEDURE greet_proc(name VARCHAR)
		RETURNS VARCHAR
		LANGUAGE SQL
		AS $$
		BEGIN
			RETURN greeting(:name);
		END;
		$$`
	if _, err := executor.ExecuteWithContext(ctx, executionContext, createSQL); err != nil {
		t.Fatalf("CREATE PROCEDURE error = %v", err)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "CALL greet_proc('bob')")
	if err != nil {
		t.Fatalf("CALL greet_proc error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "hello bob" {
		t.Fatalf("rows = %#v, want \"hello bob\"", result.Rows)
	}
}

func TestFunctionCreateOrReplace(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "FUNC_REPLACE_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "FUNC_REPLACE_DB", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE FUNCTION plus_one(n INTEGER) RETURNS INTEGER AS $$ n + 1 $$"); err != nil {
		t.Fatalf("CREATE FUNCTION error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE OR REPLACE FUNCTION plus_one(n INTEGER) RETURNS INTEGER AS $$ n + 2 $$"); err != nil {
		t.Fatalf("CREATE OR REPLACE FUNCTION error = %v", err)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT plus_one(10) AS p")
	if err != nil {
		t.Fatalf("SELECT plus_one(10) error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != int32(12) {
		t.Fatalf("rows = %#v, want 12 (the replaced definition)", result.Rows)
	}

	shown, err := executor.QueryWithContext(ctx, executionContext, "SHOW FUNCTIONS")
	if err != nil {
		t.Fatalf("SHOW FUNCTIONS error = %v", err)
	}
	if len(shown.Rows) != 1 {
		t.Fatalf("SHOW FUNCTIONS rows = %#v, want exactly one entry after replace", shown.Rows)
	}
}

func TestFunctionDropIfExists(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "FUNC_DROP_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "FUNC_DROP_DB", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP FUNCTION IF EXISTS never_created(INTEGER)"); err != nil {
		t.Fatalf("DROP FUNCTION IF EXISTS error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP FUNCTION never_created(INTEGER)"); err == nil {
		t.Fatal("DROP FUNCTION without IF EXISTS returned nil error for a function that was never created")
	}
}

func TestFunctionDoesNotShadowBuiltins(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "FUNC_BUILTIN_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "FUNC_BUILTIN_DB", Schema: "PUBLIC"}

	for _, statement := range []string{
		"CREATE FUNCTION double_it(n INTEGER) RETURNS INTEGER AS $$ n * 2 $$",
		"CREATE TABLE nums (n INTEGER)",
		"INSERT INTO nums VALUES (1), (2), (3)",
	} {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
			t.Fatalf("setup statement %q error = %v", statement, err)
		}
	}

	// COUNT and UPPER are builtins, unaffected by a schema that happens to
	// also define its own function.
	result, err := executor.QueryWithContext(ctx, executionContext, "SELECT UPPER('hi') AS u, double_it(n) AS d FROM nums WHERE n = 1")
	if err != nil {
		t.Fatalf("SELECT with a builtin and a UDF error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "HI" || result.Rows[0][1] != int32(2) {
		t.Fatalf("rows = %#v, want [HI, 2]", result.Rows)
	}

	count, err := executor.QueryWithContext(ctx, executionContext, "SELECT COUNT(*) AS c FROM nums")
	if err != nil {
		t.Fatalf("SELECT COUNT(*) error = %v", err)
	}
	if len(count.Rows) != 1 || count.Rows[0][0] != int64(3) {
		t.Fatalf("COUNT(*) rows = %#v, want 3", count.Rows)
	}
}

func TestCreateFunctionRejectsUnsupportedLanguage(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "FUNC_LANG_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "FUNC_LANG_DB", Schema: "PUBLIC"}

	_, err = executor.ExecuteWithContext(ctx, executionContext,
		"CREATE FUNCTION bad(n INTEGER) RETURNS INTEGER LANGUAGE JAVASCRIPT AS $$ n $$")
	if err == nil {
		t.Fatal("CREATE FUNCTION with LANGUAGE JAVASCRIPT returned nil error")
	}
}

func TestCreateFunctionRejectsMalformedSyntax(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "FUNC_MALFORMED_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "FUNC_MALFORMED_DB", Schema: "PUBLIC"}

	// Missing RETURNS clause entirely.
	_, err = executor.ExecuteWithContext(ctx, executionContext, "CREATE FUNCTION bad(n INTEGER) AS $$ n $$")
	if err == nil {
		t.Fatal("CREATE FUNCTION without RETURNS returned nil error")
	}
}
