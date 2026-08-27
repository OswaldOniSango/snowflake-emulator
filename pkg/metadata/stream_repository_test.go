package metadata

import (
	"context"
	"testing"
)

func TestRepository_StreamLifecycle(t *testing.T) {
	repo := setupTestRepository(t)
	ctx := context.Background()

	database, err := repo.CreateDatabase(ctx, "STREAM_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	schema, err := repo.CreateSchema(ctx, database.ID, "PUBLIC", "")
	if err != nil {
		t.Fatalf("CreateSchema() error = %v", err)
	}

	stream, err := repo.CreateStream(ctx, schema.ID, "EVENTS_STREAM", "STREAM_DB", "PUBLIC", "EVENTS", "APPEND_ONLY", 4, false)
	if err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	if stream.Name != "EVENTS_STREAM" || stream.Offset != 4 {
		t.Fatalf("unexpected stream: %+v", stream)
	}

	streams, err := repo.ListStreams(ctx, schema.ID)
	if err != nil {
		t.Fatalf("ListStreams() error = %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("ListStreams() returned %d streams, want 1", len(streams))
	}

	if err := repo.DropStream(ctx, schema.ID, stream.Name, false); err != nil {
		t.Fatalf("DropStream() error = %v", err)
	}
	if _, err := repo.GetStreamByName(ctx, schema.ID, stream.Name); err == nil {
		t.Fatal("GetStreamByName() after drop returned nil error")
	}
}
