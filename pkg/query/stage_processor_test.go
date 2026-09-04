package query

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/nnnkkk7/snowflake-emulator/pkg/connection"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"github.com/nnnkkk7/snowflake-emulator/pkg/stage"
)

func setupStageProcessorTest(t *testing.T) (*Executor, *stage.Manager, *metadata.Schema) {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("open DuckDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	manager := connection.NewManager(db)
	repo, err := metadata.NewRepository(manager)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	database, err := repo.CreateDatabase(context.Background(), "STAGE_DB", "")
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	schemaMetadata, err := repo.GetSchemaByName(context.Background(), database.ID, "PUBLIC")
	if err != nil {
		t.Fatalf("get PUBLIC schema: %v", err)
	}
	stageManager := stage.NewManager(repo, t.TempDir())
	executor := NewExecutor(manager, repo, WithStageManager(stageManager))
	return executor, stageManager, schemaMetadata
}

func TestStageProcessorLifecycleAndList(t *testing.T) {
	executor, stageManager, schemaMetadata := setupStageProcessorTest(t)
	ctx := context.Background()
	executionContext := ExecutionContext{Database: "STAGE_DB", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE STAGE lessons COMMENT = 'CSV lessons'"); err != nil {
		t.Fatalf("CREATE STAGE: %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE STAGE IF NOT EXISTS lessons"); err != nil {
		t.Fatalf("CREATE STAGE IF NOT EXISTS: %v", err)
	}
	if err := stageManager.PutFile(ctx, schemaMetadata.ID, "LESSONS", "incoming/users.csv", bytes.NewBufferString("1,Alice\n")); err != nil {
		t.Fatalf("put CSV: %v", err)
	}
	if err := stageManager.PutFile(ctx, schemaMetadata.ID, "LESSONS", "incoming/readme.txt", bytes.NewBufferString("ignore")); err != nil {
		t.Fatalf("put text file: %v", err)
	}

	result, err := executor.QueryWithContext(ctx, executionContext, "LIST @lessons/incoming PATTERN = '*.csv'")
	if err != nil {
		t.Fatalf("LIST: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "incoming/users.csv" {
		t.Fatalf("LIST rows = %#v", result.Rows)
	}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "CREATE OR REPLACE STAGE lessons"); err != nil {
		t.Fatalf("CREATE OR REPLACE STAGE: %v", err)
	}
	result, err = executor.QueryWithContext(ctx, executionContext, "LIST @lessons")
	if err != nil {
		t.Fatalf("LIST replaced stage: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("replaced stage retained files: %#v", result.Rows)
	}

	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP STAGE lessons"); err != nil {
		t.Fatalf("DROP STAGE: %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "DROP STAGE IF EXISTS lessons"); err != nil {
		t.Fatalf("DROP STAGE IF EXISTS: %v", err)
	}
}

func TestStageProcessorRejectsUnsupportedSyntax(t *testing.T) {
	executor, _, _ := setupStageProcessorTest(t)
	ctx := context.Background()

	tests := []struct {
		name             string
		executionContext ExecutionContext
		statement        string
	}{
		{name: "external stage", executionContext: ExecutionContext{Database: "STAGE_DB", Schema: "PUBLIC"}, statement: "CREATE STAGE ext URL = 's3://bucket'"},
		{name: "missing context", statement: "CREATE STAGE lessons"},
		{name: "conflicting modifiers", executionContext: ExecutionContext{Database: "STAGE_DB", Schema: "PUBLIC"}, statement: "CREATE OR REPLACE STAGE IF NOT EXISTS lessons"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := executor.ExecuteWithContext(ctx, test.executionContext, test.statement); err == nil {
				t.Fatalf("%s unexpectedly succeeded", test.statement)
			}
		})
	}
}

// TestCreateAndDropStageAllowALeadingComment mirrors the fix already pinned
// for CREATE PROCEDURE: a comment folded into the same statement text as the
// CREATE or DROP that follows it, the shape the console's own splitter
// produces when a comment sits right above a statement with no semicolon of
// its own to end a prior one.
func TestCreateAndDropStageAllowALeadingComment(t *testing.T) {
	executor, _, _ := setupStageProcessorTest(t)
	ctx := context.Background()
	executionContext := ExecutionContext{Database: "STAGE_DB", Schema: "PUBLIC"}

	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"-- CSV drop point\nCREATE STAGE lessons"); err != nil {
		t.Fatalf("commented CREATE STAGE error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"-- no longer needed\nDROP STAGE lessons"); err != nil {
		t.Fatalf("commented DROP STAGE error = %v", err)
	}
}
