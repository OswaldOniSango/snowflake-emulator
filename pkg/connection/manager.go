// Package connection provides thread-safe database connection management for DuckDB.
package connection

import (
	"context"
	"database/sql"
	"sync"
)

// Manager manages DuckDB connections with proper locking.
//
// The Manager ensures thread-safe access to the DuckDB database:
//   - Query operations can be concurrent (reads)
//   - Exec operations are serialized using a mutex (writes)
//   - Transactions are also serialized to maintain consistency
type Manager struct {
	db      *sql.DB
	runner  sqlRunner
	writeMu *sync.Mutex
}

type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// NewManager creates a new connection manager for the given database.
func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db, runner: db, writeMu: &sync.Mutex{}}
}

// Query executes a read query (can be concurrent).
// Multiple goroutines can call Query simultaneously.
func (m *Manager) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return m.runner.QueryContext(ctx, query, args...)
}

// QueryRow executes a query that is expected to return at most one row.
func (m *Manager) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return m.runner.QueryRowContext(ctx, query, args...)
}

// Exec executes a write operation (serialized).
// Write operations are serialized using a mutex to prevent conflicts.
func (m *Manager) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return m.runner.ExecContext(ctx, query, args...)
}

// WithConnection pins all operations performed by the callback to one
// database/sql connection. This is required for DuckDB connection-scoped
// objects such as temporary tables.
func (m *Manager) WithConnection(ctx context.Context, fn func(*Manager) error) error {
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	pinned := &Manager{db: m.db, runner: conn, writeMu: m.writeMu}
	return fn(pinned)
}

// ExecTx executes multiple statements in a transaction.
// The transaction is serialized using the same write mutex.
// If the provided function returns an error, the transaction is rolled back.
func (m *Manager) ExecTx(ctx context.Context, fn func(*sql.Tx) error) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// DB returns the underlying database connection.
// This is useful for operations that need direct access to the sql.DB,
// such as setting connection pool parameters.
func (m *Manager) DB() *sql.DB {
	return m.db
}
