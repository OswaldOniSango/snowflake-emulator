package metadata

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/nnnkkk7/snowflake-emulator/pkg/connection"
)

func newHistoryRepo(t *testing.T, path string) (*Repository, func()) {
	t.Helper()

	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	repo, err := NewRepository(connection.NewManager(db))
	if err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}
	return repo, func() { _ = db.Close() }
}

func record(handle, status string, created time.Time) *StatementRecord {
	completed := created.Add(50 * time.Millisecond)
	return &StatementRecord{
		Handle:      handle,
		Status:      status,
		SQLText:     "SELECT 1",
		Database:    "TEST_DB",
		Schema:      "PUBLIC",
		Warehouse:   "COMPUTE_WH",
		CreatedOn:   created,
		CompletedOn: &completed,
		RowCount:    1,
	}
}

func TestRecordStatementRoundTrip(t *testing.T) {
	repo, closeDB := newHistoryRepo(t, ":memory:")
	defer closeDB()
	ctx := context.Background()

	created := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	if err := repo.RecordStatement(ctx, record("01abc", "success", created)); err != nil {
		t.Fatalf("RecordStatement failed: %v", err)
	}

	records, err := repo.ListStatementHistory(ctx, 10)
	if err != nil {
		t.Fatalf("ListStatementHistory failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	got := records[0]
	if got.Handle != "01abc" || got.Status != "success" || got.SQLText != "SELECT 1" {
		t.Errorf("record did not round-trip: %+v", got)
	}
	if got.Database != "TEST_DB" || got.Schema != "PUBLIC" || got.Warehouse != "COMPUTE_WH" {
		t.Errorf("the namespace did not round-trip: %+v", got)
	}
	if got.RowCount != 1 {
		t.Errorf("row count = %d, want 1", got.RowCount)
	}
	if got.CompletedOn == nil {
		t.Error("completed_at should have been kept")
	}
}

func TestRecordStatementUpdatesInPlace(t *testing.T) {
	repo, closeDB := newHistoryRepo(t, ":memory:")
	defer closeDB()
	ctx := context.Background()

	created := time.Now().Truncate(time.Millisecond)
	if err := repo.RecordStatement(ctx, record("01abc", "running", created)); err != nil {
		t.Fatalf("first RecordStatement failed: %v", err)
	}
	if err := repo.RecordStatement(ctx, record("01abc", "success", created)); err != nil {
		t.Fatalf("second RecordStatement failed: %v", err)
	}

	records, err := repo.ListStatementHistory(ctx, 10)
	if err != nil {
		t.Fatalf("ListStatementHistory failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("recording the same handle twice should update, got %d rows", len(records))
	}
	if records[0].Status != "success" {
		t.Errorf("status = %q, want the later value", records[0].Status)
	}
}

func TestRecordStatementKeepsFailureDetail(t *testing.T) {
	repo, closeDB := newHistoryRepo(t, ":memory:")
	defer closeDB()
	ctx := context.Background()

	failed := record("01fail", "failed", time.Now().Truncate(time.Millisecond))
	failed.ErrorCode = "002003"
	failed.ErrorMessage = "Object 'NOPE' does not exist"
	if err := repo.RecordStatement(ctx, failed); err != nil {
		t.Fatalf("RecordStatement failed: %v", err)
	}

	records, _ := repo.ListStatementHistory(ctx, 10)
	if len(records) != 1 || records[0].ErrorCode != "002003" {
		t.Fatalf("the error code should round-trip, got %+v", records)
	}
	if records[0].ErrorMessage != "Object 'NOPE' does not exist" {
		t.Errorf("error message = %q", records[0].ErrorMessage)
	}
}

func TestListStatementHistoryOrdersNewestFirst(t *testing.T) {
	repo, closeDB := newHistoryRepo(t, ":memory:")
	defer closeDB()
	ctx := context.Background()

	base := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	for i, handle := range []string{"old", "middle", "new"} {
		if err := repo.RecordStatement(ctx, record(handle, "success", base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("RecordStatement failed: %v", err)
		}
	}

	records, err := repo.ListStatementHistory(ctx, 0)
	if err != nil {
		t.Fatalf("ListStatementHistory failed: %v", err)
	}
	got := []string{records[0].Handle, records[1].Handle, records[2].Handle}
	want := []string{"new", "middle", "old"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestListStatementHistoryHonoursLimit(t *testing.T) {
	repo, closeDB := newHistoryRepo(t, ":memory:")
	defer closeDB()
	ctx := context.Background()

	base := time.Now().Truncate(time.Millisecond)
	for i, handle := range []string{"a", "b", "c"} {
		_ = repo.RecordStatement(ctx, record(handle, "success", base.Add(time.Duration(i)*time.Second)))
	}

	records, err := repo.ListStatementHistory(ctx, 2)
	if err != nil {
		t.Fatalf("ListStatementHistory failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("limit ignored: got %d records", len(records))
	}
}

func TestRecordStatementRejectsAnEmptyHandle(t *testing.T) {
	repo, closeDB := newHistoryRepo(t, ":memory:")
	defer closeDB()

	if err := repo.RecordStatement(context.Background(), &StatementRecord{}); err == nil {
		t.Error("a statement with no handle should not be recorded")
	}
}

func TestPruneStatementHistoryDropsOnlyWhatFinishedBefore(t *testing.T) {
	repo, closeDB := newHistoryRepo(t, ":memory:")
	defer closeDB()
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	_ = repo.RecordStatement(ctx, record("old", "success", now.Add(-48*time.Hour)))
	_ = repo.RecordStatement(ctx, record("recent", "success", now.Add(-time.Minute)))

	removed, err := repo.PruneStatementHistory(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("PruneStatementHistory failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed %d rows, want 1", removed)
	}

	records, _ := repo.ListStatementHistory(ctx, 0)
	if len(records) != 1 || records[0].Handle != "recent" {
		t.Errorf("pruning kept the wrong rows: %+v", records)
	}
}

func TestPruneStatementHistoryKeepsWhatHasNotFinished(t *testing.T) {
	repo, closeDB := newHistoryRepo(t, ":memory:")
	defer closeDB()
	ctx := context.Background()

	running := record("running", "running", time.Now().Add(-48*time.Hour))
	running.CompletedOn = nil
	_ = repo.RecordStatement(ctx, running)

	if _, err := repo.PruneStatementHistory(ctx, time.Now()); err != nil {
		t.Fatalf("PruneStatementHistory failed: %v", err)
	}

	records, _ := repo.ListStatementHistory(ctx, 0)
	if len(records) != 1 {
		t.Error("a statement that never finished has no completion to prune on")
	}
}

// The point of recording: a statement written by one process is read by the
// next one to open the same database file.
func TestHistorySurvivesReopeningTheDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	ctx := context.Background()

	first, closeFirst := newHistoryRepo(t, path)
	if err := first.RecordStatement(ctx, record("01abc", "success", time.Now().Truncate(time.Millisecond))); err != nil {
		t.Fatalf("RecordStatement failed: %v", err)
	}
	closeFirst()

	second, closeSecond := newHistoryRepo(t, path)
	defer closeSecond()

	records, err := second.ListStatementHistory(ctx, 10)
	if err != nil {
		t.Fatalf("ListStatementHistory failed: %v", err)
	}
	if len(records) != 1 || records[0].Handle != "01abc" {
		t.Fatalf("the statement should have survived the restart, got %+v", records)
	}
}

// A database file written by an earlier build has the history table without
// the console's columns. Opening it must add them rather than fail.
func TestOpeningADatabaseFromBeforeTheseColumnsMigratesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	old, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	_, err = old.Exec(`CREATE TABLE _metadata_query_history (
		id VARCHAR PRIMARY KEY,
		session_id VARCHAR,
		query_id VARCHAR,
		sql_text TEXT,
		status VARCHAR NOT NULL,
		rows_affected BIGINT DEFAULT 0,
		execution_time_ms BIGINT DEFAULT 0,
		error_message TEXT,
		started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("failed to create the old table: %v", err)
	}
	if _, err := old.Exec(
		`INSERT INTO _metadata_query_history (id, query_id, sql_text, status, started_at)
		 VALUES ('1', '01legacy', 'SELECT 1', 'success', ?)`, time.Now()); err != nil {
		t.Fatalf("failed to seed the old table: %v", err)
	}
	_ = old.Close()

	repo, closeDB := newHistoryRepo(t, path)
	defer closeDB()
	ctx := context.Background()

	records, err := repo.ListStatementHistory(ctx, 0)
	if err != nil {
		t.Fatalf("reading a migrated table failed: %v", err)
	}
	if len(records) != 1 || records[0].Handle != "01legacy" {
		t.Fatalf("the existing row should survive the migration, got %+v", records)
	}
	if records[0].Database != "" {
		t.Errorf("a row from before the column has no namespace, got %q", records[0].Database)
	}

	// And the new columns are usable on the migrated table.
	if err := repo.RecordStatement(ctx, record("01new", "success", time.Now().Truncate(time.Millisecond))); err != nil {
		t.Fatalf("recording into a migrated table failed: %v", err)
	}
	records, _ = repo.ListStatementHistory(ctx, 0)
	if len(records) != 2 {
		t.Fatalf("expected both rows, got %d", len(records))
	}
}
