package query

import (
	"context"
	"strings"
	"testing"
)

func TestCreateSchemaSQLRegistersInDatabaseCatalog(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	firstDatabase, err := repo.CreateDatabase(ctx, "FIRST_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase(FIRST_DB) error = %v", err)
	}
	secondDatabase, err := repo.CreateDatabase(ctx, "SECOND_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase(SECOND_DB) error = %v", err)
	}

	tests := []struct {
		name             string
		statement        string
		executionContext ExecutionContext
		databaseID       string
		wantSchema       string
		wantComment      string
	}{
		{
			name:             "database context",
			statement:        "CREATE SCHEMA analytics COMMENT = 'Created with SQL'",
			executionContext: ExecutionContext{Database: "FIRST_DB", Schema: "PUBLIC"},
			databaseID:       firstDatabase.ID,
			wantSchema:       "ANALYTICS",
			wantComment:      "Created with SQL",
		},
		{
			name:             "qualified database and schema",
			statement:        "CREATE SCHEMA SECOND_DB.analytics",
			executionContext: ExecutionContext{Database: "FIRST_DB", Schema: "PUBLIC"},
			databaseID:       secondDatabase.ID,
			wantSchema:       "ANALYTICS",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := executor.ExecuteWithContext(ctx, tt.executionContext, tt.statement); err != nil {
				t.Fatalf("ExecuteWithContext(%q) error = %v", tt.statement, err)
			}
			schema, err := repo.GetSchemaByName(ctx, tt.databaseID, tt.wantSchema)
			if err != nil {
				t.Fatalf("GetSchemaByName(%s) error = %v", tt.wantSchema, err)
			}
			if schema.Comment != tt.wantComment {
				t.Fatalf("schema comment = %q, want %q", schema.Comment, tt.wantComment)
			}
		})
	}

	if _, err := executor.ExecuteWithContext(ctx,
		ExecutionContext{Database: "FIRST_DB", Schema: "PUBLIC"},
		"CREATE SCHEMA IF NOT EXISTS analytics",
	); err != nil {
		t.Fatalf("CREATE SCHEMA IF NOT EXISTS error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx,
		ExecutionContext{Database: "FIRST_DB", Schema: "PUBLIC"},
		"DROP SCHEMA analytics",
	); err != nil {
		t.Fatalf("DROP SCHEMA error = %v", err)
	}
	if _, err := repo.GetSchemaByName(ctx, firstDatabase.ID, "ANALYTICS"); err == nil {
		t.Fatal("ANALYTICS remains in FIRST_DB catalog after DROP SCHEMA")
	}
	if _, err := repo.GetSchemaByName(ctx, secondDatabase.ID, "ANALYTICS"); err != nil {
		t.Fatalf("DROP SCHEMA removed SECOND_DB.ANALYTICS: %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx,
		ExecutionContext{Database: "FIRST_DB", Schema: "PUBLIC"},
		"DROP SCHEMA IF EXISTS analytics",
	); err != nil {
		t.Fatalf("DROP SCHEMA IF EXISTS error = %v", err)
	}
}

func TestCreateAndDropTableSQLSynchronizesCatalog(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "CATALOG_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	schema, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if err != nil {
		t.Fatalf("GetSchemaByName(PUBLIC) error = %v", err)
	}
	executionContext := ExecutionContext{Database: "CATALOG_DB", Schema: "PUBLIC"}

	tests := []struct {
		name      string
		statement string
		tableName string
		tableType string
	}{
		{
			name:      "base table with declared columns",
			statement: "CREATE TABLE users (id INTEGER NOT NULL, name VARCHAR DEFAULT 'unknown')",
			tableName: "USERS",
			tableType: "BASE TABLE",
		},
		{
			name:      "transient table as select",
			statement: "CREATE OR REPLACE TRANSIENT TABLE staged_users AS SELECT 1 AS id",
			tableName: "STAGED_USERS",
			tableType: transientTableType,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := executor.ExecuteWithContext(ctx, executionContext, tt.statement); err != nil {
				t.Fatalf("ExecuteWithContext(%q) error = %v", tt.statement, err)
			}
			table, err := repo.GetTableByName(ctx, schema.ID, tt.tableName)
			if err != nil {
				t.Fatalf("GetTableByName(%s) error = %v", tt.tableName, err)
			}
			if table.TableType != tt.tableType {
				t.Fatalf("table type = %q, want %q", table.TableType, tt.tableType)
			}
			if !strings.Contains(table.ColumnDefinitions, "ID:INTEGER") {
				t.Fatalf("column definitions = %q, want ID metadata", table.ColumnDefinitions)
			}
		})
	}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP TABLE users"); err != nil {
		t.Fatalf("DROP TABLE users error = %v", err)
	}
	if _, err := repo.GetTableByName(ctx, schema.ID, "USERS"); err == nil {
		t.Fatal("USERS remains in catalog after DROP TABLE")
	}
	if _, err := repo.GetTableByName(ctx, schema.ID, "STAGED_USERS"); err != nil {
		t.Fatalf("unrelated STAGED_USERS metadata was removed: %v", err)
	}
}

func TestCreateSchemaSQLRequiresDatabaseContext(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	if _, err := executor.Execute(context.Background(), "CREATE SCHEMA analytics"); err == nil || !strings.Contains(err.Error(), "requires a database context") {
		t.Fatalf("CREATE SCHEMA without database error = %v", err)
	}
}
