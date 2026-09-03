package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// StatementRecord is one statement as the console's history shows it.
//
// It is deliberately narrower than QueryHistoryEntry: a history is scanned,
// not read, so no result set is kept. The handle is the identity — recording
// the same statement twice updates the row rather than adding a second one.
type StatementRecord struct {
	Handle       string
	Status       string
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

// RecordStatement writes a statement to the persistent history.
//
// The row is keyed on the handle, so a statement recorded again as it moves
// from running to finished updates in place. Statements outlive the process
// only when the emulator was given a database file; with the default in-memory
// database the history is still recorded, and still lost on exit.
func (r *Repository) RecordStatement(ctx context.Context, record *StatementRecord) error {
	if record.Handle == "" {
		return fmt.Errorf("cannot record a statement with no handle")
	}

	var completedAt any
	var durationMs int64
	if record.CompletedOn != nil {
		completedAt = *record.CompletedOn
		durationMs = record.CompletedOn.Sub(record.CreatedOn).Milliseconds()
	}

	// DuckDB has no ON CONFLICT for a non-unique column, and query_id is not
	// the primary key, so the update is spelled out.
	const update = `UPDATE _metadata_query_history
		SET sql_text = ?, status = ?, rows_affected = ?, execution_time_ms = ?,
		    error_code = ?, error_message = ?, started_at = ?, completed_at = ?,
		    database_name = ?, schema_name = ?, warehouse = ?
		WHERE query_id = ?`

	result, err := r.mgr.Exec(ctx, update,
		record.SQLText, record.Status, int64(record.RowCount), durationMs,
		record.ErrorCode, record.ErrorMessage, record.CreatedOn, completedAt,
		record.Database, record.Schema, record.Warehouse, record.Handle)
	if err != nil {
		return fmt.Errorf("failed to update statement history: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected > 0 {
		return nil
	}

	const insert = `INSERT INTO _metadata_query_history
		(id, session_id, query_id, sql_text, status, rows_affected, execution_time_ms,
		 error_code, error_message, started_at, completed_at,
		 database_name, schema_name, warehouse)
		VALUES (?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	if _, err := r.mgr.Exec(ctx, insert,
		record.Handle, record.Handle, record.SQLText, record.Status,
		int64(record.RowCount), durationMs, record.ErrorCode, record.ErrorMessage,
		record.CreatedOn, completedAt,
		record.Database, record.Schema, record.Warehouse); err != nil {
		return fmt.Errorf("failed to insert statement history: %w", err)
	}
	return nil
}

// ListStatementHistory returns recorded statements, most recent first.
// A limit of zero or less returns every row.
func (r *Repository) ListStatementHistory(ctx context.Context, limit int) ([]StatementRecord, error) {
	query := `SELECT query_id, status, sql_text, rows_affected, error_code, error_message,
		started_at, completed_at, database_name, schema_name, warehouse
		FROM _metadata_query_history
		WHERE query_id IS NOT NULL AND query_id <> ''
		ORDER BY started_at DESC`
	args := []any{}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.mgr.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to read statement history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	records := make([]StatementRecord, 0, limit)
	for rows.Next() {
		var record StatementRecord
		var rowCount int64
		var errorCode, errorMessage, database, schema, warehouse sql.NullString
		var completedAt sql.NullTime

		if err := rows.Scan(&record.Handle, &record.Status, &record.SQLText, &rowCount,
			&errorCode, &errorMessage, &record.CreatedOn, &completedAt,
			&database, &schema, &warehouse); err != nil {
			return nil, fmt.Errorf("failed to scan statement history row: %w", err)
		}

		record.RowCount = int(rowCount)
		record.ErrorCode = errorCode.String
		record.ErrorMessage = errorMessage.String
		record.Database = database.String
		record.Schema = schema.String
		record.Warehouse = warehouse.String
		if completedAt.Valid {
			record.CompletedOn = &completedAt.Time
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read statement history: %w", err)
	}
	return records, nil
}

// PruneStatementHistory removes statements that finished before the cutoff, so
// a long-lived database file does not accumulate history without bound.
func (r *Repository) PruneStatementHistory(ctx context.Context, before time.Time) (int64, error) {
	const query = `DELETE FROM _metadata_query_history
		WHERE completed_at IS NOT NULL AND completed_at < ?`

	result, err := r.mgr.Exec(ctx, query, before)
	if err != nil {
		return 0, fmt.Errorf("failed to prune statement history: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return affected, nil
}
