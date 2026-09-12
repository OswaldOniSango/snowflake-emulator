package query

import (
	"context"
	"strings"
	"testing"
)

func TestCreateAndDropDatabaseSQLSynchronizesCatalog(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	if _, err := executor.Execute(ctx, "CREATE DATABASE phase7_db COMMENT = 'Identity tests'"); err != nil {
		t.Fatalf("CREATE DATABASE error = %v", err)
	}
	database, err := repo.GetDatabaseByName(ctx, "PHASE7_DB")
	if err != nil {
		t.Fatalf("PHASE7_DB was not registered: %v", err)
	}
	if database.Comment != "Identity tests" {
		t.Fatalf("database comment = %q, want Identity tests", database.Comment)
	}
	if _, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("PUBLIC schema was not created: %v", err)
	}
	if _, err := executor.Execute(ctx, "CREATE DATABASE IF NOT EXISTS phase7_db"); err != nil {
		t.Fatalf("CREATE DATABASE IF NOT EXISTS error = %v", err)
	}
	if _, err := executor.Execute(ctx, "DROP DATABASE phase7_db"); err != nil {
		t.Fatalf("DROP DATABASE error = %v", err)
	}
	if _, err := repo.GetDatabaseByName(ctx, "PHASE7_DB"); err == nil {
		t.Fatal("PHASE7_DB remains after DROP DATABASE")
	}
	if _, err := executor.Execute(ctx, "DROP DATABASE IF EXISTS phase7_db"); err != nil {
		t.Fatalf("DROP DATABASE IF EXISTS error = %v", err)
	}
}

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
			tableType: baseTableType,
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

func TestQualifiedCreateAndDropTableSQLSynchronizesCatalog(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()
	database, err := repo.CreateDatabase(ctx, "PHASE7_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	schema, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "PHASE7_DB", Schema: "PUBLIC"}
	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"CREATE TABLE PHASE7_DB.PUBLIC.LESSON_USERS (id INTEGER)"); err != nil {
		t.Fatalf("qualified CREATE TABLE error = %v", err)
	}
	if _, err := repo.GetTableByName(ctx, schema.ID, "LESSON_USERS"); err != nil {
		t.Fatalf("qualified table was not registered: %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"DROP TABLE PHASE7_DB.PUBLIC.LESSON_USERS"); err != nil {
		t.Fatalf("qualified DROP TABLE error = %v", err)
	}
	if _, err := repo.GetTableByName(ctx, schema.ID, "LESSON_USERS"); err == nil {
		t.Fatal("qualified table metadata remains after DROP TABLE")
	}
}

func TestCreateSchemaSQLRequiresDatabaseContext(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	if _, err := executor.Execute(context.Background(), "CREATE SCHEMA analytics"); err == nil || !strings.Contains(err.Error(), "requires a database context") {
		t.Fatalf("CREATE SCHEMA without database error = %v", err)
	}
}

// TestCreateAndDropSQLAllowALeadingComment pins the fix for a statement the
// console's own splitter produces routinely: a comment describing the object,
// with no semicolon of its own, ends up folded into the same statement text
// as the CREATE or DROP that follows it. Classify already looks past a
// leading comment to route the statement here; these regexes, anchored at
// the very start of the string, did not.
func TestCreateAndDropSQLAllowALeadingComment(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	ctx := context.Background()

	if _, err := repo.CreateDatabase(ctx, "COMMENT_DB", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "COMMENT_DB", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"-- analytics objects live here\nCREATE SCHEMA analytics"); err != nil {
		t.Fatalf("commented CREATE SCHEMA error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"-- raw user data\nCREATE TABLE users (id INTEGER)"); err != nil {
		t.Fatalf("commented CREATE TABLE error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"-- no longer needed\nDROP TABLE users"); err != nil {
		t.Fatalf("commented DROP TABLE error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"-- cleanup\nDROP SCHEMA analytics"); err != nil {
		t.Fatalf("commented DROP SCHEMA error = %v", err)
	}
}
