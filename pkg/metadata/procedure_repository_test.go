package metadata

import (
	"context"
	"testing"
)

func TestRepository_ProcedureLifecycle(t *testing.T) {
	repo := setupTestRepository(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "LESSON_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	schema, err := repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}

	procedure, err := repo.CreateProcedure(ctx, schema.ID, "HELLO", `[{"name":"NAME","type":"VARCHAR"}]`, "VARCHAR", "SQL", "RETURN :NAME", "", false)
	if err != nil {
		t.Fatalf("CreateProcedure() error = %v", err)
	}
	if procedure.Name != "HELLO" || procedure.Language != "SQL" {
		t.Fatalf("unexpected procedure: %+v", procedure)
	}

	procedures, err := repo.ListProcedures(ctx, schema.ID)
	if err != nil {
		t.Fatalf("ListProcedures() error = %v", err)
	}
	if len(procedures) != 1 {
		t.Fatalf("ListProcedures() returned %d procedures, want 1", len(procedures))
	}

	if err := repo.DropProcedure(ctx, schema.ID, "HELLO", false); err != nil {
		t.Fatalf("DropProcedure() error = %v", err)
	}
	if _, err := repo.GetProcedureByName(ctx, schema.ID, "HELLO"); err == nil {
		t.Fatal("GetProcedureByName() after drop returned nil error")
	}
}
