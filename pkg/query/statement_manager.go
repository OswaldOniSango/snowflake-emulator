package query

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
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

// StatementManager manages active statements with thread safety.
type StatementManager struct {
	mu         sync.RWMutex
	statements map[string]*Statement
	ttl        time.Duration
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
	defer sm.mu.Unlock()

	stmt, ok := sm.statements[handle]
	if !ok {
		return false
	}

	stmt.Status = status
	if status == StatementStatusSuccess || status == StatementStatusFailed || status == StatementStatusCanceled {
		now := time.Now()
		stmt.CompletedOn = &now
	}
	return true
}

// SetResult sets the result of a successful statement.
func (sm *StatementManager) SetResult(handle string, result *Result) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stmt, ok := sm.statements[handle]
	if !ok {
		return false
	}

	stmt.Result = result
	stmt.Status = StatementStatusSuccess
	now := time.Now()
	stmt.CompletedOn = &now
	return true
}

// SetError sets the error of a failed statement.
func (sm *StatementManager) SetError(handle string, err *apierror.SnowflakeError) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stmt, ok := sm.statements[handle]
	if !ok {
		return false
	}

	stmt.Error = err
	stmt.Status = StatementStatusFailed
	now := time.Now()
	stmt.CompletedOn = &now
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
	defer sm.mu.Unlock()

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
// The manager keeps statements in memory for its TTL, so this is a recent
// history rather than a complete one: statements older than the TTL, and every
// statement from before a restart, are gone. A limit of zero returns them all.
func (sm *StatementManager) ListStatements(limit int) []StatementSummary {
	sm.mu.RLock()
	summaries := make([]StatementSummary, 0, len(sm.statements))
	for _, statement := range sm.statements {
		summaries = append(summaries, summarize(statement))
	}
	sm.mu.RUnlock()

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].CreatedOn.After(summaries[j].CreatedOn)
	})

	if limit > 0 && len(summaries) > limit {
		summaries = summaries[:limit]
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
func (sm *StatementManager) RetentionPeriod() time.Duration {
	return sm.ttl
}
