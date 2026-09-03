// Package query provides SQL query execution against DuckDB with Snowflake SQL translation.
package query

import (
	"github.com/nnnkkk7/snowflake-emulator/server/types"
)

// BindingValue represents a parameter binding value for SQL queries.
// This mirrors the REST API v2 binding format.
type BindingValue struct {
	Type  string // FIXED, TEXT, REAL, BOOLEAN, DATE, TIME, TIMESTAMP, etc.
	Value string // String representation of the value
}

// QueryBindingValue is an alias for BindingValue for backward compatibility.
//
// Deprecated: Use BindingValue instead.
//
//nolint:revive // Keeping for backward compatibility
type QueryBindingValue = BindingValue

// Result represents the result of a SELECT query execution.
type Result struct {
	Columns     []string
	ColumnTypes []types.ColumnMetadata
	Rows        [][]interface{}

	// TotalRows is how many rows the statement produced. It exceeds len(Rows)
	// when a row limit stopped the result being materialized in full, so a
	// caller can say "1,000 of 200,000" rather than implying there were 1,000.
	TotalRows int
}

// Truncated reports whether rows were left behind by a row limit.
func (r *Result) Truncated() bool {
	return r.TotalRows > len(r.Rows)
}

// ExecResult represents the result of a non-query execution (INSERT, UPDATE, DELETE, etc.).
type ExecResult struct {
	RowsAffected int64
}

// CopyResult contains the result of a COPY INTO operation.
type CopyResult struct {
	RowsLoaded   int64
	RowsInserted int64
	FilesLoaded  int
	Errors       []string
}

// MergeResult contains the result of a MERGE operation.
type MergeResult struct {
	RowsInserted int64
	RowsUpdated  int64
	RowsDeleted  int64
}

// SchemaContext provides database/schema context for operations.
type SchemaContext struct {
	DatabaseName string
	SchemaName   string
	SchemaID     string
}

// ExecutionContext contains the Snowflake session context used to resolve
// unqualified object names while executing a statement.
type ExecutionContext struct {
	Database  string
	Schema    string
	Warehouse string
	Role      string
	SessionID string

	// RowLimit caps how many rows a query materializes. Zero uses the
	// executor's default. It lives here because it is a property of one
	// statement's execution, the same as the namespace it resolves against.
	RowLimit int
}
