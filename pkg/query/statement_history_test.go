package query

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"github.com/nnnkkk7/snowflake-emulator/server/apierror"
)

// fakeHistory stands in for the repository, and can be told to fail.
type fakeHistory struct {
	mu       sync.Mutex
	records  []metadata.StatementRecord
	prunedTo time.Time
	failing  bool
}

func (f *fakeHistory) RecordStatement(_ context.Context, record *metadata.StatementRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		return errors.New("history unavailable")
	}
	for i := range f.records {
		if f.records[i].Handle == record.Handle {
			f.records[i] = *record
			return nil
		}
	}
	f.records = append(f.records, *record)
	return nil
}

func (f *fakeHistory) ListStatementHistory(_ context.Context, limit int) ([]metadata.StatementRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		return nil, errors.New("history unavailable")
	}
	out := make([]metadata.StatementRecord, len(f.records))
	copy(out, f.records)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeHistory) PruneStatementHistory(_ context.Context, before time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prunedTo = before
	return 0, nil
}

func (f *fakeHistory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.records)
}

func (f *fakeHistory) statusOf(handle string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.records {
		if f.records[i].Handle == handle {
			return f.records[i].Status
		}
	}
	return ""
}

func managerWithHistory(t *testing.T) (*StatementManager, *fakeHistory) {
	t.Helper()
	store := &fakeHistory{}
	manager := NewStatementManager(time.Hour)
	manager.SetHistoryStore(store, 7*24*time.Hour, true)
	return manager, store
}

func TestSetResultRecordsTheStatement(t *testing.T) {
	manager, store := managerWithHistory(t)

	stmt := manager.CreateStatement("SELECT 1", "TEST_DB", "PUBLIC", "COMPUTE_WH")
	manager.SetResult(stmt.Handle, &Result{Rows: [][]interface{}{{1}}})

	if store.count() != 1 {
		t.Fatalf("expected the statement to be recorded, got %d records", store.count())
	}
	if got := store.statusOf(stmt.Handle); got != string(StatementStatusSuccess) {
		t.Errorf("recorded status = %q, want success", got)
	}
}

func TestSetErrorRecordsTheFailure(t *testing.T) {
	manager, store := managerWithHistory(t)

	stmt := manager.CreateStatement("SELECT nope", "TEST_DB", "PUBLIC", "")
	manager.SetError(stmt.Handle, &apierror.SnowflakeError{Code: "002003", Message: "no such object"})

	if got := store.statusOf(stmt.Handle); got != string(StatementStatusFailed) {
		t.Errorf("recorded status = %q, want failed", got)
	}
}

func TestCancelStatementRecordsTheCancellation(t *testing.T) {
	manager, store := managerWithHistory(t)

	stmt := manager.CreateStatement("SELECT 1", "TEST_DB", "PUBLIC", "")
	manager.UpdateStatus(stmt.Handle, StatementStatusRunning)
	if err := manager.CancelStatement(stmt.Handle); err != nil {
		t.Fatalf("CancelStatement failed: %v", err)
	}

	if got := store.statusOf(stmt.Handle); got != string(StatementStatusCanceled) {
		t.Errorf("recorded status = %q, want canceled", got)
	}
}

func TestRecordingTheSameStatementTwiceKeepsOneRow(t *testing.T) {
	manager, store := managerWithHistory(t)

	stmt := manager.CreateStatement("SELECT 1", "TEST_DB", "PUBLIC", "")
	manager.UpdateStatus(stmt.Handle, StatementStatusRunning)
	manager.SetResult(stmt.Handle, &Result{})

	if store.count() != 1 {
		t.Errorf("a statement should hold one row through its life, got %d", store.count())
	}
}

func TestAFailingHistoryDoesNotFailTheStatement(t *testing.T) {
	manager, store := managerWithHistory(t)
	store.failing = true

	stmt := manager.CreateStatement("SELECT 1", "TEST_DB", "PUBLIC", "")
	if !manager.SetResult(stmt.Handle, &Result{}) {
		t.Error("a statement should succeed even when it cannot be recorded")
	}
	if got, _ := manager.GetStatement(stmt.Handle); got.Status != StatementStatusSuccess {
		t.Errorf("status = %q, want success", got.Status)
	}
}

func TestListStatementsIncludesWhatOnlyTheStoreHolds(t *testing.T) {
	manager, store := managerWithHistory(t)

	// A statement from a previous run of the emulator: recorded, never in
	// this manager's memory.
	earlier := time.Now().Add(-time.Hour)
	_ = store.RecordStatement(context.Background(), &metadata.StatementRecord{
		Handle: "01old", Status: "success", SQLText: "SELECT 'yesterday'",
		Database: "TEST_DB", Schema: "PUBLIC", CreatedOn: earlier,
	})

	stmt := manager.CreateStatement("SELECT 'today'", "TEST_DB", "PUBLIC", "")
	manager.SetResult(stmt.Handle, &Result{})

	summaries := manager.ListStatements(0)
	if len(summaries) != 2 {
		t.Fatalf("expected memory and history to be merged, got %d", len(summaries))
	}
	if summaries[0].SQLText != "SELECT 'today'" {
		t.Errorf("the newest statement should come first, got %q", summaries[0].SQLText)
	}
	if summaries[1].SQLText != "SELECT 'yesterday'" {
		t.Errorf("the recorded statement should follow, got %q", summaries[1].SQLText)
	}
}

func TestListStatementsDoesNotShowAStatementTwice(t *testing.T) {
	manager, _ := managerWithHistory(t)

	stmt := manager.CreateStatement("SELECT 1", "TEST_DB", "PUBLIC", "")
	manager.SetResult(stmt.Handle, &Result{})

	summaries := manager.ListStatements(0)
	if len(summaries) != 1 {
		t.Fatalf("a statement in memory and in the store is one statement, got %d", len(summaries))
	}
}

func TestMemoryWinsOverTheRecordedCopy(t *testing.T) {
	manager, store := managerWithHistory(t)

	stmt := manager.CreateStatement("SELECT 1", "TEST_DB", "PUBLIC", "")
	manager.UpdateStatus(stmt.Handle, StatementStatusRunning)
	// The store now holds "running"; memory has moved on.
	manager.SetResult(stmt.Handle, &Result{})
	store.mu.Lock()
	store.records[0].Status = "running"
	store.mu.Unlock()

	summaries := manager.ListStatements(0)
	if summaries[0].Status != StatementStatusSuccess {
		t.Errorf("status = %q, want the in-memory value", summaries[0].Status)
	}
}

func TestListStatementsSurvivesAnUnreadableHistory(t *testing.T) {
	manager, store := managerWithHistory(t)

	stmt := manager.CreateStatement("SELECT 1", "TEST_DB", "PUBLIC", "")
	manager.SetResult(stmt.Handle, &Result{})
	store.failing = true

	summaries := manager.ListStatements(0)
	if len(summaries) != 1 {
		t.Errorf("an unreadable history should cost history, not the listing: got %d", len(summaries))
	}
}

func TestListStatementsHonoursTheLimitAcrossBothSources(t *testing.T) {
	manager, store := managerWithHistory(t)

	base := time.Now().Add(-time.Hour)
	for i, handle := range []string{"01a", "01b", "01c"} {
		_ = store.RecordStatement(context.Background(), &metadata.StatementRecord{
			Handle: handle, Status: "success", SQLText: handle,
			CreatedOn: base.Add(time.Duration(i) * time.Minute),
		})
	}
	stmt := manager.CreateStatement("newest", "TEST_DB", "PUBLIC", "")
	manager.SetResult(stmt.Handle, &Result{})

	summaries := manager.ListStatements(2)
	if len(summaries) != 2 {
		t.Fatalf("limit = 2 returned %d statements", len(summaries))
	}
	if summaries[0].SQLText != "newest" || summaries[1].SQLText != "01c" {
		t.Errorf("the limit should keep the newest, got %q and %q", summaries[0].SQLText, summaries[1].SQLText)
	}
}

func TestRetentionPeriodReportsTheStoresWhenThereIsOne(t *testing.T) {
	manager := NewStatementManager(time.Hour)
	if manager.RetentionPeriod() != time.Hour {
		t.Errorf("without a store the in-memory TTL is the answer, got %v", manager.RetentionPeriod())
	}
	if manager.HistoryIsPersistent() {
		t.Error("no store means no persistent history")
	}

	manager.SetHistoryStore(&fakeHistory{}, 7*24*time.Hour, true)
	if manager.RetentionPeriod() != 7*24*time.Hour {
		t.Errorf("with a store its retention is the answer, got %v", manager.RetentionPeriod())
	}
	if !manager.HistoryIsPersistent() {
		t.Error("a durable store means a persistent history")
	}
}

func TestSetHistoryStoreDefaultsTheRetention(t *testing.T) {
	manager := NewStatementManager(time.Hour)
	manager.SetHistoryStore(&fakeHistory{}, 0, true)
	if manager.RetentionPeriod() != DefaultHistoryRetention {
		t.Errorf("retention = %v, want the default", manager.RetentionPeriod())
	}
}

func TestNoStoreMeansNoRecording(t *testing.T) {
	manager := NewStatementManager(time.Hour)
	stmt := manager.CreateStatement("SELECT 1", "TEST_DB", "PUBLIC", "")

	if !manager.SetResult(stmt.Handle, &Result{}) {
		t.Fatal("SetResult should still succeed without a store")
	}
	if len(manager.ListStatements(0)) != 1 {
		t.Error("the in-memory listing should be unchanged")
	}
}

func TestAnInMemoryHistoryIsNotPersistent(t *testing.T) {
	manager := NewStatementManager(time.Hour)
	manager.SetHistoryStore(&fakeHistory{}, time.Hour, false)

	if manager.HistoryIsPersistent() {
		t.Error("recording into an in-memory database does not survive a restart")
	}
	if manager.RetentionPeriod() != time.Hour {
		t.Errorf("retention should still be the store's, got %v", manager.RetentionPeriod())
	}
}

// Cancellation has to be the last word: the work may finish anyway, and a
// statement the reader stopped must not come back as though it had succeeded.
func TestCancellationIsFinal(t *testing.T) {
	manager := NewStatementManager(time.Hour)
	stmt := manager.CreateStatement("SELECT 1", "TEST_DB", "PUBLIC", "")
	manager.UpdateStatus(stmt.Handle, StatementStatusRunning)
	if err := manager.CancelStatement(stmt.Handle); err != nil {
		t.Fatalf("CancelStatement failed: %v", err)
	}

	if manager.SetResult(stmt.Handle, &Result{Rows: [][]interface{}{{1}}}) {
		t.Error("a result arriving after cancellation should be refused")
	}
	if manager.UpdateStatus(stmt.Handle, StatementStatusSuccess) {
		t.Error("a canceled statement should not be moved to success")
	}
	if manager.SetError(stmt.Handle, &apierror.SnowflakeError{Code: "x", Message: "y"}) {
		t.Error("the interruption should not be recorded over the cancellation")
	}

	got, _ := manager.GetStatement(stmt.Handle)
	if got.Status != StatementStatusCanceled {
		t.Errorf("status = %q, want canceled", got.Status)
	}
	if got.Result != nil {
		t.Error("a canceled statement should carry no result")
	}
}

func TestACanceledStatementStaysCanceledInTheHistory(t *testing.T) {
	manager, store := managerWithHistory(t)
	stmt := manager.CreateStatement("SELECT 1", "TEST_DB", "PUBLIC", "")
	manager.UpdateStatus(stmt.Handle, StatementStatusRunning)
	_ = manager.CancelStatement(stmt.Handle)

	manager.SetResult(stmt.Handle, &Result{})

	if got := store.statusOf(stmt.Handle); got != string(StatementStatusCanceled) {
		t.Errorf("recorded status = %q, want canceled", got)
	}
}
