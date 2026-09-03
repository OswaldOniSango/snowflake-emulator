// Package types provides API request/response types for Snowflake REST API v2.
package types

import "encoding/json"

// SQL REST API v2 Types
// Reference: https://docs.snowflake.com/en/developer-guide/sql-api/

// SubmitStatementRequest represents POST /api/v2/statements request body.
type SubmitStatementRequest struct {
	Statement  string                   `json:"statement"`
	Timeout    int                      `json:"timeout,omitempty"`    // Timeout in seconds
	Database   string                   `json:"database,omitempty"`   // Database context
	Schema     string                   `json:"schema,omitempty"`     // Schema context
	Warehouse  string                   `json:"warehouse,omitempty"`  // Warehouse to use
	Role       string                   `json:"role,omitempty"`       // Role context
	Bindings   map[string]*BindingValue `json:"bindings,omitempty"`   // Parameter bindings
	Parameters map[string]string        `json:"parameters,omitempty"` // Session parameters

	// RowLimit caps how many rows come back. Zero uses the server's default,
	// which exists so that a query over a large table cannot build a response
	// no client can render.
	RowLimit int `json:"rowLimit,omitempty"`

	// Async returns a handle as soon as the statement is accepted, rather than
	// holding the response open until it finishes. The caller then polls
	// GET /api/v2/statements/{handle}, and can cancel it in the meantime.
	// It can also be asked for with ?async=true, as Snowflake's API does.
	Async bool `json:"async,omitempty"`
}

// BindingValue represents a parameter binding value.
type BindingValue struct {
	Type  string `json:"type"`  // FIXED, TEXT, REAL, BOOLEAN, DATE, TIME, TIMESTAMP, etc.
	Value string `json:"value"` // String representation of the value
}

// StatementResponse represents the response from statement operations.
type StatementResponse struct {
	ResultSetMetaData  *ResultSetMetaData `json:"resultSetMetaData,omitempty"`
	Data               [][]interface{}    `json:"data,omitempty"`
	Code               string             `json:"code"`
	StatementStatusURL string             `json:"statementStatusUrl,omitempty"`
	RequestID          string             `json:"requestId,omitempty"`
	SQLState           string             `json:"sqlState"`
	StatementHandle    string             `json:"statementHandle"`
	Message            string             `json:"message,omitempty"`
	CreatedOn          int64              `json:"createdOn,omitempty"`
}

// ResultSetMetaData contains metadata about the result set.
type ResultSetMetaData struct {
	NumRows       int64           `json:"numRows"`
	Format        string          `json:"format"` // "jsonv2" or "arrow"
	RowType       []RowTypeField  `json:"rowType"`
	PartitionInfo []PartitionInfo `json:"partitionInfo,omitempty"`
}

// RowTypeField describes a column in the result set.
type RowTypeField struct {
	Name       string `json:"name"`
	Database   string `json:"database,omitempty"`
	Schema     string `json:"schema,omitempty"`
	Table      string `json:"table,omitempty"`
	Type       string `json:"type"`
	Length     int64  `json:"length,omitempty"`
	Precision  int64  `json:"precision,omitempty"`
	Scale      int64  `json:"scale,omitempty"`
	Nullable   bool   `json:"nullable"`
	ByteLength int64  `json:"byteLength,omitempty"`
	Collation  string `json:"collation,omitempty"`
}

// PartitionInfo describes data partitioning.
type PartitionInfo struct {
	RowCount         int64 `json:"rowCount"`
	UncompressedSize int64 `json:"uncompressedSize"`
	CompressedSize   int64 `json:"compressedSize,omitempty"`
}

// CancelStatementResponse represents the response from canceling a statement.
type CancelStatementResponse struct {
	Code            string `json:"code"`
	SQLState        string `json:"sqlState"`
	Message         string `json:"message,omitempty"`
	StatementHandle string `json:"statementHandle"`
}

// Resource Management API Types

// DatabaseRequest represents a request to create/alter a database.
type DatabaseRequest struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
}

// DatabaseResponse represents database information.
type DatabaseResponse struct {
	Name      string `json:"name"`
	Comment   string `json:"comment,omitempty"`
	Owner     string `json:"owner,omitempty"`
	CreatedOn string `json:"created_on,omitempty"`
}

// ListDatabasesResponse represents a list of databases.
type ListDatabasesResponse []DatabaseResponse

// SchemaRequest represents a request to create/alter a schema.
type SchemaRequest struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
}

// SchemaResponse represents schema information.
type SchemaResponse struct {
	Name         string `json:"name"`
	DatabaseName string `json:"database_name,omitempty"`
	Comment      string `json:"comment,omitempty"`
	Owner        string `json:"owner,omitempty"`
	CreatedOn    string `json:"created_on,omitempty"`
}

// ListSchemasResponse represents a list of schemas.
type ListSchemasResponse []SchemaResponse

// TableRequest represents a request to create a table.
type TableRequest struct {
	Name    string      `json:"name"`
	Columns []ColumnDef `json:"columns"`
	Comment string      `json:"comment,omitempty"`
}

// ColumnDef represents a column definition for CREATE TABLE.
type ColumnDef struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Nullable   bool    `json:"nullable,omitempty"`
	Default    *string `json:"default,omitempty"`
	PrimaryKey bool    `json:"primary_key,omitempty"`
}

// AlterDatabaseRequest represents ALTER DATABASE request.
type AlterDatabaseRequest struct {
	Comment *string `json:"comment,omitempty"`
}

// AlterTableRequest represents ALTER TABLE request.
type AlterTableRequest struct {
	Comment *string `json:"comment,omitempty"`
}

// TableResponse represents table information.
type TableResponse struct {
	Name      string `json:"name"`
	Database  string `json:"database_name,omitempty"`
	Schema    string `json:"schema_name,omitempty"`
	TableType string `json:"table_type,omitempty"` // BASE TABLE, VIEW, etc.
	Comment   string `json:"comment,omitempty"`
	Owner     string `json:"owner,omitempty"`
	CreatedOn string `json:"created_on,omitempty"`
	RowCount  int64  `json:"row_count,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
}

// ListTablesResponse represents a list of tables.
type ListTablesResponse []TableResponse

// WarehouseRequest represents a request to create/alter a warehouse.
type WarehouseRequest struct {
	Name        string `json:"name"`
	Size        string `json:"warehouse_size,omitempty"` // X-SMALL, SMALL, MEDIUM, etc.
	AutoSuspend int    `json:"auto_suspend,omitempty"`   // Seconds
	AutoResume  bool   `json:"auto_resume,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

// UnmarshalJSON accepts both the Snowflake-style warehouse_size request field
// and the size field returned by this API and reused by the web client.
func (r *WarehouseRequest) UnmarshalJSON(data []byte) error {
	var payload struct {
		Name          string `json:"name"`
		WarehouseSize string `json:"warehouse_size"`
		Size          string `json:"size"`
		AutoSuspend   int    `json:"auto_suspend"`
		AutoResume    bool   `json:"auto_resume"`
		Comment       string `json:"comment"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	r.Name = payload.Name
	r.Size = payload.WarehouseSize
	if r.Size == "" {
		r.Size = payload.Size
	}
	r.AutoSuspend = payload.AutoSuspend
	r.AutoResume = payload.AutoResume
	r.Comment = payload.Comment
	return nil
}

// WarehouseResponse represents warehouse information.
type WarehouseResponse struct {
	Name        string `json:"name"`
	State       string `json:"state"` // STARTED, SUSPENDED, RESUMING, SUSPENDING
	Size        string `json:"size,omitempty"`
	Type        string `json:"type,omitempty"` // STANDARD, SNOWPARK-OPTIMIZED
	AutoSuspend int    `json:"auto_suspend,omitempty"`
	AutoResume  bool   `json:"auto_resume,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Owner       string `json:"owner,omitempty"`
	CreatedOn   string `json:"created_on,omitempty"`
}

// ListWarehousesResponse represents a list of warehouses.
type ListWarehousesResponse []WarehouseResponse

// Common response wrapper for REST API v2
type RESTAPIV2Response struct {
	Code    string      `json:"code"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// Statement status codes
const (
	StatementStatusRunning  = "running"
	StatementStatusSuccess  = "success"
	StatementStatusFailed   = "failed"
	StatementStatusCanceled = "canceled"
)

// SQL State codes for REST API v2
const (
	SQLState00000 = "00000" // Success
	SQLState02000 = "02000" // No data
	SQLState22000 = "22000" // Data exception
	SQLState42000 = "42000" // Syntax error or access rule violation
)

// Response codes
const (
	ResponseCodeSuccess           = "090001" // Statement succeeded
	ResponseCodeStatementPending  = "333334" // Statement still running
	ResponseCodeStatementCanceled = "000604" // Statement canceled
)

// TranslateRequest represents POST /api/v2/translate request body.
type TranslateRequest struct {
	Statement string `json:"statement"`
	Database  string `json:"database,omitempty"` // Execution context for short object names
	Schema    string `json:"schema,omitempty"`
}

// TranslateResponse shows what a statement becomes on its way to DuckDB,
// without executing it.
type TranslateResponse struct {
	Statement  string `json:"statement"`
	Translated string `json:"translated"`

	// HandledBy names the component that executes the statement. Several
	// statement kinds are routed to processors before the translator sees
	// them, and their final SQL is built elsewhere.
	HandledBy string `json:"handledBy"`

	// Complete reports whether Translated is the SQL DuckDB actually receives.
	Complete bool `json:"complete"`

	// Note explains why an incomplete preview is incomplete.
	Note string `json:"note,omitempty"`

	// Rewrites lists what the statement's parts become, so a reader does not
	// have to diff two blocks of SQL by eye to find the differences.
	Rewrites []TranslationRewrite `json:"rewrites"`
}

// TranslationRewrite is one substitution, from what was written to what runs.
type TranslationRewrite struct {
	From string `json:"from"`
	To   string `json:"to"`

	// Kind is "function" or "object".
	Kind string `json:"kind"`
}

// SchemaObject is one entry in a schema's contents.
type SchemaObject struct {
	Name string `json:"name"`

	// Kind is "table", "stream", "procedure", "task" or "stage".
	Kind string `json:"kind"`

	// Detail is a short, human-readable qualifier: a stream's source table, a
	// procedure's arguments, a task's state.
	Detail string `json:"detail,omitempty"`
}

// ListSchemaObjectsResponse lists everything a schema contains, in one call so
// that a tree can expand a schema without fanning out.
type ListSchemaObjectsResponse struct {
	Database string         `json:"database"`
	Schema   string         `json:"schema"`
	Objects  []SchemaObject `json:"objects"`
}

// CreateStageRequest creates a named internal stage.
type CreateStageRequest struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
}

// StageResponse describes a named internal stage.
type StageResponse struct {
	Name      string `json:"name"`
	Database  string `json:"database_name"`
	Schema    string `json:"schema_name"`
	StageType string `json:"type"`
	Comment   string `json:"comment,omitempty"`
	CreatedOn string `json:"created_on"`
}

// StageFileResponse describes one file stored in an internal stage.
type StageFileResponse struct {
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified"`
}

// StatementHistoryEntry is one row of the statement history.
type StatementHistoryEntry struct {
	Handle      string `json:"statementHandle"`
	Status      string `json:"status"`
	Statement   string `json:"statement"`
	Database    string `json:"database,omitempty"`
	Schema      string `json:"schema,omitempty"`
	Warehouse   string `json:"warehouse,omitempty"`
	CreatedOn   int64  `json:"createdOn"`
	CompletedOn int64  `json:"completedOn,omitempty"`

	// DurationMs is set once a statement has finished.
	DurationMs int64 `json:"durationMs,omitempty"`
	NumRows    int   `json:"numRows"`

	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// ListStatementsResponse is the statement history.
type ListStatementsResponse struct {
	Statements []StatementHistoryEntry `json:"statements"`

	// RetainedFor says how long a statement is kept, so a reader can tell an
	// empty history from a short one.
	RetainedFor string `json:"retainedFor"`

	// Persistent says whether the history survives a restart. It does only
	// when the emulator was given a database file rather than the default
	// in-memory one.
	Persistent bool `json:"persistent"`
}
