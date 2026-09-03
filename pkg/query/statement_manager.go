package query

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"github.com/nnnkkk7/snowflake-emulator/server/apierror"
)

// StatementStatus represents the status of a statement.
type StatementStatus string

const (
	StatementStatusPending  StatementStatus = "pending"
	StatementStatusRunning  StatementStatus = "running"
	StatementStatusSuccess  StatementStatus = "success"
	StatementStatusFailed   StatementStatus = "failed"
	StatementStatusCanceled StatementStatus = "canceled"
)

// Statement represents an executing or completed SQL statement.
type Statement struct {
	Handle      string
	Status      StatementStatus
	SQLText     string
	Database    string
	Schema      string
	Warehouse   string
	CreatedOn   time.Time
	CompletedOn *time.Time
	Result      *Result
	Error       *apierror.SnowflakeError
	cancelFunc  context.CancelFunc
}

// HistoryStore keeps statements past the manager's own retention, so that the
// console's history survives a restart when the emulator was given a database
// file. Satisfied by metadata.Repository.
type HistoryStore interface {
	RecordStatement(ctx context.Context, record *metadata.StatementRecord) error
	ListStatementHistory(ctx context.Context, limit int) ([]metadata.StatementRecord, error)
	PruneStatementHistory(ctx context.Context, before time.Time) (int64, error)
}

// StatementManager manages active statements with thread safety.
type StatementManager struct {
	mu         sync.RWMutex
	statements map[string]*Statement
	ttl        time.Duration

	// Guards the store itself, not the rows: the store is set once at startup
	// but read from the cleanup goroutine.
	historyMu        sync.RWMutex
	history          HistoryStore
	historyRetention time.Duration
	historyDurable   bool
}

// DefaultHistoryRetention is how long a recorded statement is kept. It is far
// longer than the in-memory TTL because the point of recording is to still
// have the statement tomorrow.
const DefaultHistoryRetention = 7 * 24 * time.Hour

// SetHistoryStore makes the manager record statements as they finish. Passing
// a retention of zero or less keeps recorded statements for the default.
//
// durable says whether those records outlive the process. They do only when
// the emulator was given a database file: recording into the default in-memory
// database still widens the history well past the in-memory TTL, but it goes
// when the process does, and the console should not promise otherwise.
func (sm *StatementManager) SetHistoryStore(store HistoryStore, retention time.Duration, durable bool) {
	if retention <= 0 {
		retention = DefaultHistoryRetention
	}
	sm.historyMu.Lock()
	defer sm.historyMu.Unlock()
	sm.history = store
	sm.historyRetention = retention
	sm.historyDurable = durable
}

func (sm *StatementManager) historyStore() (HistoryStore, time.Duration) {
	sm.historyMu.RLock()
	defer sm.historyMu.RUnlock()
	return sm.history, sm.historyRetention
}

// record persists one statement. Failing to record must never fail the
// statement itself, so the error is reported and dropped.
func (sm *StatementManager) record(summary *StatementSummary) {
	store, _ := sm.historyStore()
	if store == nil {
		return
	}

	err := store.RecordStatement(context.Background(), &metadata.StatementRecord{
		Handle:       summary.Handle,
		Status:       string(summary.Status),
		SQLText:      summary.SQLText,
		Database:     summary.Database,
		Schema:       summary.Schema,
		Warehouse:    summary.Warehouse,
		CreatedOn:    summary.CreatedOn,
		CompletedOn:  summary.CompletedOn,
		RowCount:     summary.RowCount,
		ErrorCode:    summary.ErrorCode,
		ErrorMessage: summary.ErrorMessage,
	})
	if err != nil {
		log.Printf("Failed to record statement %s in history: %v", summary.Handle, err)
	}
}

// NewStatementManager creates a new statement manager.
func NewStatementManager(ttl time.Duration) *StatementManager {
	sm := &StatementManager{
		statements: make(map[string]*Statement),
		ttl:        ttl,
	}
	go sm.cleanupLoop()
	return sm
}

// CreateStatement creates a new statement and returns its handle.
func (sm *StatementManager) CreateStatement(sqlText, database, schema, warehouse string) *Statement {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	handle := generateStatementHandle()
	stmt := &Statement{
		Handle:    handle,
		Status:    StatementStatusPending,
		SQLText:   sqlText,
		Database:  database,
		Schema:    schema,
		Warehouse: warehouse,
		CreatedOn: time.Now(),
	}
	sm.statements[handle] = stmt
	return stmt
}

// GetStatement retrieves a statement by handle.
func (sm *StatementManager) GetStatement(handle string) (*Statement, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	stmt, ok := sm.statements[handle]
	return stmt, ok
}

// UpdateStatus updates the status of a statement.
func (sm *StatementManager) UpdateStatus(handle string, status StatementStatus) bool {
	sm.mu.Lock()
	locked := true
	defer func() {
		if locked {
			sm.mu.Unlock()
		}
	}()

	stmt, ok := sm.statements[handle]
	if !ok {
		return false
	}

	stmt.Status = status
	if status == StatementStatusSuccess || status == StatementStatusFailed || status == StatementStatusCanceled {
		now := time.Now()
		stmt.CompletedOn = &now
	}
	summary := summarize(stmt)
	sm.mu.Unlock()
	locked = false

	sm.record(&summary)
	return true
}

// SetResult sets the result of a successful statement.
func (sm *StatementManager) SetResult(handle string, result *Result) bool {
	sm.mu.Lock()
	stmt, ok := sm.statements[handle]
	if !ok {
		sm.mu.Unlock()
		return false
	}

	stmt.Result = result
	stmt.Status = StatementStatusSuccess
	now := time.Now()
	stmt.CompletedOn = &now
	summary := summarize(stmt)
	sm.mu.Unlock()

	sm.record(&summary)
	return true
}

// SetError sets the error of a failed statement.
func (sm *StatementManager) SetError(handle string, err *apierror.SnowflakeError) bool {
	sm.mu.Lock()
	stmt, ok := sm.statements[handle]
	if !ok {
		sm.mu.Unlock()
		return false
	}

	stmt.Error = err
	stmt.Status = StatementStatusFailed
	now := time.Now()
	stmt.CompletedOn = &now
	summary := summarize(stmt)
	sm.mu.Unlock()

	sm.record(&summary)
	return true
}

// SetCancelFunc sets the cancel function for a running statement.
func (sm *StatementManager) SetCancelFunc(handle string, cancelFunc context.CancelFunc) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stmt, ok := sm.statements[handle]
	if !ok {
		return false
	}

	stmt.cancelFunc = cancelFunc
	return true
}

// CancelStatement cancels a running statement.
func (sm *StatementManager) CancelStatement(handle string) error {
	sm.mu.Lock()
	locked := true
	defer func() {
		if locked {
			sm.mu.Unlock()
		}
	}()

	stmt, ok := sm.statements[handle]
	if !ok {
		return fmt.Errorf("statement not found: %s", handle)
	}

	if stmt.Status != StatementStatusRunning && stmt.Status != StatementStatusPending {
		return fmt.Errorf("statement %s is not running (status: %s)", handle, stmt.Status)
	}

	if stmt.cancelFunc != nil {
		stmt.cancelFunc()
	}

	stmt.Status = StatementStatusCanceled
	now := time.Now()
	stmt.CompletedOn = &now
	summary := summarize(stmt)
	sm.mu.Unlock()
	locked = false

	sm.record(&summary)
	return nil
}

// DeleteStatement removes a statement from the manager.
func (sm *StatementManager) DeleteStatement(handle string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.statements, handle)
}

// cleanupLoop periodically removes expired statements.
func (sm *StatementManager) cleanupLoop() {
	ticker := time.NewTicker(sm.ttl / 2)
	defer ticker.Stop()

	for range ticker.C {
		sm.cleanup()
		sm.pruneHistory()
	}
}

// pruneHistory drops recorded statements past their retention, so a long-lived
// database file does not grow a history without bound.
func (sm *StatementManager) pruneHistory() {
	store, retention := sm.historyStore()
	if store == nil {
		return
	}
	if _, err := store.PruneStatementHistory(context.Background(), time.Now().Add(-retention)); err != nil {
		log.Printf("Failed to prune statement history: %v", err)
	}
}

// cleanup removes statements that have been completed for longer than TTL.
func (sm *StatementManager) cleanup() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	for handle, stmt := range sm.statements {
		if stmt.CompletedOn != nil && now.Sub(*stmt.CompletedOn) > sm.ttl {
			delete(sm.statements, handle)
		}
	}
}

// generateStatementHandle generates a unique statement handle in Snowflake format.
func generateStatementHandle() string {
	id := uuid.New()
	return fmt.Sprintf("01%s", id.String()[:32])
}

// StatementSummary describes a finished or running statement for a history
// listing. It carries no result set: a history is scanned, not read.
type StatementSummary struct {
	Handle       string
	Status       StatementStatus
	SQLText      string
	Database     string
	Schema       string
	Warehouse    string
	CreatedOn    time.Time
	CompletedOn  *time.Time
	RowCount     int
	ErrorCode    string
	ErrorMessage string
}

// ListStatements returns the statements still held, most recent first.
//
// A limit of zero returns them all. Without a history store this covers only
// what is still in memory: statements older than the TTL, and every statement
// from before a restart, are gone.
func (sm *StatementManager) ListStatements(limit int) []StatementSummary {
	return sm.ListStatementsWithContext(context.Background(), limit)
}

// ListStatementsWithContext is ListStatements against a recorded history.
//
// Statements still in memory and statements read back from the store are
// merged on their handle, with memory winning: it holds the newer state of
// anything still running, and the row for it was written before it finished.
func (sm *StatementManager) ListStatementsWithContext(ctx context.Context, limit int) []StatementSummary {
	sm.mu.RLock()
	summaries := make([]StatementSummary, 0, len(sm.statements))
	seen := make(map[string]struct{}, len(sm.statements))
	for _, statement := range sm.statements {
		summaries = append(summaries, summarize(statement))
		seen[statement.Handle] = struct{}{}
	}
	sm.mu.RUnlock()

	summaries = append(summaries, sm.recordedStatements(ctx, limit, seen)...)

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].CreatedOn.After(summaries[j].CreatedOn)
	})

	if limit > 0 && len(summaries) > limit {
		summaries = summaries[:limit]
	}
	return summaries
}

// recordedStatements reads the persisted history, skipping handles memory
// already covers. A store that cannot be read costs history, not a listing.
func (sm *StatementManager) recordedStatements(
	ctx context.Context,
	limit int,
	seen map[string]struct{},
) []StatementSummary {
	store, _ := sm.historyStore()
	if store == nil {
		return nil
	}

	// Ask for more than the caller wants: some of the rows will be duplicates
	// of what memory already holds, and would otherwise crowd out older
	// statements that belong in the answer.
	fetch := 0
	if limit > 0 {
		fetch = limit + len(seen)
	}

	records, err := store.ListStatementHistory(ctx, fetch)
	if err != nil {
		log.Printf("Failed to read statement history: %v", err)
		return nil
	}

	summaries := make([]StatementSummary, 0, len(records))
	for i := range records {
		record := &records[i]
		if _, ok := seen[record.Handle]; ok {
			continue
		}
		summaries = append(summaries, StatementSummary{
			Handle:       record.Handle,
			Status:       StatementStatus(record.Status),
			SQLText:      record.SQLText,
			Database:     record.Database,
			Schema:       record.Schema,
			Warehouse:    record.Warehouse,
			CreatedOn:    record.CreatedOn,
			CompletedOn:  record.CompletedOn,
			RowCount:     record.RowCount,
			ErrorCode:    record.ErrorCode,
			ErrorMessage: record.ErrorMessage,
		})
	}
	return summaries
}

func summarize(statement *Statement) StatementSummary {
	summary := StatementSummary{
		Handle:      statement.Handle,
		Status:      statement.Status,
		SQLText:     statement.SQLText,
		Database:    statement.Database,
		Schema:      statement.Schema,
		Warehouse:   statement.Warehouse,
		CreatedOn:   statement.CreatedOn,
		CompletedOn: statement.CompletedOn,
	}

	if statement.Result != nil {
		summary.RowCount = len(statement.Result.Rows)
	}
	if statement.Error != nil {
		summary.ErrorCode = statement.Error.Code
		summary.ErrorMessage = statement.Error.Message
	}

	return summary
}

// RetentionPeriod is how long a statement is kept before it is discarded.
// With a history store that is the store's retention, which is what the
// console should tell a reader; without one it is the in-memory TTL.
func (sm *StatementManager) RetentionPeriod() time.Duration {
	if store, retention := sm.historyStore(); store != nil {
		return retention
	}
	return sm.ttl
}

// HistoryIsPersistent reports whether recorded statements survive a restart.
func (sm *StatementManager) HistoryIsPersistent() bool {
	sm.historyMu.RLock()
	defer sm.historyMu.RUnlock()
	return sm.history != nil && sm.historyDurable
}
