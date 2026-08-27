package query

import (
	"context"
	"strings"
	"testing"

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
	if _, err := repo.CreateSchema(ctx, database.ID, "PUBLIC", ""); err != nil {
		t.Fatalf("CreateSchema() error = %v", err)
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
