package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"github.com/nnnkkk7/snowflake-emulator/pkg/query"
	"github.com/nnnkkk7/snowflake-emulator/pkg/warehouse"
	"github.com/nnnkkk7/snowflake-emulator/server/apierror"
	"github.com/nnnkkk7/snowflake-emulator/server/types"
)

// RestAPIv2Handler handles REST API v2 requests.
type RestAPIv2Handler struct {
	executor     *query.Executor
	stmtMgr      *query.StatementManager
	repo         *metadata.Repository
	warehouseMgr *warehouse.Manager
}

// NewRestAPIv2Handler creates a new REST API v2 handler.
func NewRestAPIv2Handler(executor *query.Executor, stmtMgr *query.StatementManager, repo *metadata.Repository) *RestAPIv2Handler {
	warehouseMgr := warehouse.NewManager()
	configureWarehouseValidation(executor, warehouseMgr)
	return &RestAPIv2Handler{
		executor:     executor,
		stmtMgr:      stmtMgr,
		repo:         repo,
		warehouseMgr: warehouseMgr,
	}
}

// NewRestAPIv2HandlerWithWarehouse creates a new REST API v2 handler with warehouse manager.
func NewRestAPIv2HandlerWithWarehouse(executor *query.Executor, stmtMgr *query.StatementManager, repo *metadata.Repository, warehouseMgr *warehouse.Manager) *RestAPIv2Handler {
	configureWarehouseValidation(executor, warehouseMgr)
	return &RestAPIv2Handler{
		executor:     executor,
		stmtMgr:      stmtMgr,
		repo:         repo,
		warehouseMgr: warehouseMgr,
	}
}

func configureWarehouseValidation(executor *query.Executor, warehouseMgr *warehouse.Manager) {
	executor.Configure(query.WithWarehouseValidator(func(ctx context.Context, name string) error {
		_, err := warehouseMgr.GetWarehouse(ctx, name)
		return err
	}))
}

// SubmitStatement handles POST /api/v2/statements.
func (h *RestAPIv2Handler) SubmitStatement(w http.ResponseWriter, r *http.Request) {
	var req types.SubmitStatementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body", types.SQLState42000)
		return
	}

	if req.Statement == "" {
		h.sendError(w, http.StatusBadRequest, "Statement is required", types.SQLState42000)
		return
	}

	// Create statement record
	stmt := h.stmtMgr.CreateStatement(req.Statement, req.Database, req.Schema, req.Warehouse)
	h.stmtMgr.UpdateStatus(stmt.Handle, query.StatementStatusRunning)

	// Execute the statement synchronously
	ctx := r.Context()

	// Classify the SQL statement to determine routing
	classification := query.ClassifySQL(req.Statement)

	// Convert bindings from types.BindingValue to query.QueryBindingValue
	bindings := convertBindings(req.Bindings)
	executionContext := query.ExecutionContext{
		Database:  req.Database,
		Schema:    req.Schema,
		Warehouse: req.Warehouse,
		Role:      req.Role,
		RowLimit:  req.RowLimit,
	}

	var result *query.Result
	var execResult *query.ExecResult
	var err error

	if classification.IsQuery {
		// Handle SELECT, SHOW, DESCRIBE, EXPLAIN
		if len(bindings) > 0 {
			result, err = h.executor.QueryWithBindingsAndContext(ctx, executionContext, req.Statement, bindings)
		} else {
			result, err = h.executor.QueryWithContext(ctx, executionContext, req.Statement)
		}
	} else {
		// Handle DDL (CREATE, DROP, ALTER) and DML (INSERT, UPDATE, DELETE)
		if len(bindings) > 0 {
			execResult, err = h.executor.ExecuteWithBindingsAndContext(ctx, executionContext, req.Statement, bindings)
		} else {
			execResult, err = h.executor.ExecuteWithContext(ctx, executionContext, req.Statement)
		}
	}

	if err != nil {
		sfErr := apierror.NewSnowflakeError(apierror.CodeSQLExecutionError, err.Error())
		h.stmtMgr.SetError(stmt.Handle, sfErr)

		resp := types.StatementResponse{
			StatementHandle:    stmt.Handle,
			Code:               apierror.CodeSQLExecutionError,
			SQLState:           types.SQLState42000,
			StatementStatusURL: "/api/v2/statements/" + stmt.Handle,
			Message:            err.Error(),
			CreatedOn:          stmt.CreatedOn.UnixMilli(),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Build response based on statement type
	var resp types.StatementResponse
	if classification.IsQuery {
		// Store result for queries
		h.stmtMgr.SetResult(stmt.Handle, result)
		resp = h.buildStatementResponse(stmt, result)
	} else {
		// DDL and DML carry no result set, so there is nothing to store — but
		// the statement still has to be marked finished. Without this it stays
		// "running" for as long as it is retained, and every INSERT in the
		// history reads as though it never completed.
		h.stmtMgr.UpdateStatus(stmt.Handle, query.StatementStatusSuccess)
		resp = h.buildExecResponse(stmt, execResult)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// GetStatement handles GET /api/v2/statements/{handle}.
func (h *RestAPIv2Handler) GetStatement(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")

	stmt, ok := h.stmtMgr.GetStatement(handle)
	if !ok {
		h.sendError(w, http.StatusNotFound, "Statement not found", types.SQLState02000)
		return
	}

	var resp types.StatementResponse

	switch stmt.Status {
	case query.StatementStatusRunning, query.StatementStatusPending:
		resp = types.StatementResponse{
			StatementHandle:    stmt.Handle,
			Code:               types.ResponseCodeStatementPending,
			SQLState:           types.SQLState00000,
			StatementStatusURL: "/api/v2/statements/" + stmt.Handle,
			CreatedOn:          stmt.CreatedOn.UnixMilli(),
		}
	case query.StatementStatusSuccess:
		resp = h.buildStatementResponse(stmt, stmt.Result)
	case query.StatementStatusFailed:
		resp = types.StatementResponse{
			StatementHandle:    stmt.Handle,
			Code:               stmt.Error.Code,
			SQLState:           types.SQLState42000,
			StatementStatusURL: "/api/v2/statements/" + stmt.Handle,
			Message:            stmt.Error.Message,
			CreatedOn:          stmt.CreatedOn.UnixMilli(),
		}
	case query.StatementStatusCanceled:
		resp = types.StatementResponse{
			StatementHandle:    stmt.Handle,
			Code:               types.ResponseCodeStatementCanceled,
			SQLState:           types.SQLState00000,
			StatementStatusURL: "/api/v2/statements/" + stmt.Handle,
			Message:            "Statement canceled",
			CreatedOn:          stmt.CreatedOn.UnixMilli(),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// CancelStatement handles POST /api/v2/statements/{handle}/cancel.
func (h *RestAPIv2Handler) CancelStatement(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")

	stmt, ok := h.stmtMgr.GetStatement(handle)
	if !ok {
		h.sendError(w, http.StatusNotFound, "Statement not found", types.SQLState02000)
		return
	}

	if err := h.stmtMgr.CancelStatement(handle); err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error(), types.SQLState42000)
		return
	}

	resp := types.CancelStatementResponse{
		Code:            types.ResponseCodeStatementCanceled,
		SQLState:        types.SQLState00000,
		Message:         "Statement canceled",
		StatementHandle: stmt.Handle,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// buildExecResponse builds a success response from a DDL/DML execution result.
func (h *RestAPIv2Handler) buildExecResponse(stmt *query.Statement, execResult *query.ExecResult) types.StatementResponse {
	// For DDL/DML, we return a minimal response with rows affected
	return types.StatementResponse{
		StatementHandle:    stmt.Handle,
		Code:               types.ResponseCodeSuccess,
		SQLState:           types.SQLState00000,
		StatementStatusURL: "/api/v2/statements/" + stmt.Handle,
		CreatedOn:          stmt.CreatedOn.UnixMilli(),
		ResultSetMetaData: &types.ResultSetMetaData{
			NumRows: execResult.RowsAffected,
			Format:  "jsonv2",
			RowType: []types.RowTypeField{
				{
					Name: "number of rows affected",
					Type: "FIXED",
				},
			},
		},
		Data: [][]interface{}{{execResult.RowsAffected}},
	}
}

// buildStatementResponse builds a success response from a query result.
func (h *RestAPIv2Handler) buildStatementResponse(stmt *query.Statement, result *query.Result) types.StatementResponse {
	// Convert row type
	rowType := make([]types.RowTypeField, len(result.ColumnTypes))
	for i, col := range result.ColumnTypes {
		rowType[i] = types.RowTypeField{
			Name:      col.Name,
			Type:      col.Type,
			Length:    col.Length,
			Precision: col.Precision,
			Scale:     col.Scale,
			Nullable:  col.Nullable,
		}
	}

	// Convert data to interface{} slice
	data := make([][]interface{}, len(result.Rows))
	copy(data, result.Rows)

	return types.StatementResponse{
		StatementHandle:    stmt.Handle,
		Code:               types.ResponseCodeSuccess,
		SQLState:           types.SQLState00000,
		StatementStatusURL: "/api/v2/statements/" + stmt.Handle,
		CreatedOn:          stmt.CreatedOn.UnixMilli(),
		ResultSetMetaData: &types.ResultSetMetaData{
			// numRows is how many the statement produced; data carries what
			// fits under the row limit. A caller compares the two to know it
			// is looking at a slice rather than the whole thing.
			NumRows: int64(totalRows(result)),
			Format:  "jsonv2",
			RowType: rowType,
			PartitionInfo: []types.PartitionInfo{
				{RowCount: int64(len(result.Rows))},
			},
		},
		Data: data,
	}
}

// totalRows reports how many rows a statement produced. Results built before
// the executor started counting report only what they carry.
func totalRows(result *query.Result) int {
	if result.TotalRows > 0 {
		return result.TotalRows
	}
	return len(result.Rows)
}

// sendError sends an error response.
func (h *RestAPIv2Handler) sendError(w http.ResponseWriter, statusCode int, message, sqlState string) {
	resp := types.StatementResponse{
		Code:     apierror.CodeInvalidParameter,
		SQLState: sqlState,
		Message:  message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

// Resource Management Handlers

// ListDatabases handles GET /api/v2/databases.
func (h *RestAPIv2Handler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	databases, err := h.repo.ListDatabases(ctx)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error(), types.SQLState42000)
		return
	}

	resp := make(types.ListDatabasesResponse, len(databases))
	for i, db := range databases {
		resp[i] = types.DatabaseResponse{
			Name:      db.Name,
			Comment:   db.Comment,
			Owner:     db.Owner,
			CreatedOn: db.CreatedAt.Format(time.RFC3339),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// GetDatabase handles GET /api/v2/databases/{database}.
func (h *RestAPIv2Handler) GetDatabase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")

	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	resp := types.DatabaseResponse{
		Name:      db.Name,
		Comment:   db.Comment,
		Owner:     db.Owner,
		CreatedOn: db.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// CreateDatabase handles POST /api/v2/databases.
func (h *RestAPIv2Handler) CreateDatabase(w http.ResponseWriter, r *http.Request) {
	var req types.DatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body", types.SQLState42000)
		return
	}

	if req.Name == "" {
		h.sendError(w, http.StatusBadRequest, "Database name is required", types.SQLState42000)
		return
	}

	ctx := r.Context()

	db, err := h.repo.CreateDatabase(ctx, req.Name, req.Comment)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error(), types.SQLState42000)
		return
	}

	resp := types.DatabaseResponse{
		Name:      db.Name,
		Comment:   db.Comment,
		Owner:     db.Owner,
		CreatedOn: db.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteDatabase handles DELETE /api/v2/databases/{database}.
func (h *RestAPIv2Handler) DeleteDatabase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")

	// First, look up the database by name to get its ID
	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	// Now drop using the ID
	if err := h.repo.DropDatabase(ctx, db.ID); err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error(), types.SQLState42000)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListSchemas handles GET /api/v2/databases/{database}/schemas.
func (h *RestAPIv2Handler) ListSchemas(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")

	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	schemas, err := h.repo.ListSchemas(ctx, db.ID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error(), types.SQLState42000)
		return
	}

	resp := make(types.ListSchemasResponse, len(schemas))
	for i, s := range schemas {
		resp[i] = types.SchemaResponse{
			Name:         s.Name,
			DatabaseName: dbName,
			Comment:      s.Comment,
			Owner:        s.Owner,
			CreatedOn:    s.CreatedAt.Format(time.RFC3339),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// GetSchema handles GET /api/v2/databases/{database}/schemas/{schema}.
func (h *RestAPIv2Handler) GetSchema(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")
	schemaName := chi.URLParam(r, "schema")

	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	schema, err := h.repo.GetSchemaByName(ctx, db.ID, schemaName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Schema not found", types.SQLState02000)
		return
	}

	resp := types.SchemaResponse{
		Name:         schema.Name,
		DatabaseName: dbName,
		Comment:      schema.Comment,
		Owner:        schema.Owner,
		CreatedOn:    schema.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// CreateSchema handles POST /api/v2/databases/{database}/schemas.
func (h *RestAPIv2Handler) CreateSchema(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")

	var req types.SchemaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body", types.SQLState42000)
		return
	}

	if req.Name == "" {
		h.sendError(w, http.StatusBadRequest, "Schema name is required", types.SQLState42000)
		return
	}

	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	schema, err := h.repo.CreateSchema(ctx, db.ID, req.Name, req.Comment)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error(), types.SQLState42000)
		return
	}

	resp := types.SchemaResponse{
		Name:         schema.Name,
		DatabaseName: dbName,
		Comment:      schema.Comment,
		Owner:        schema.Owner,
		CreatedOn:    schema.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteSchema handles DELETE /api/v2/databases/{database}/schemas/{schema}.
func (h *RestAPIv2Handler) DeleteSchema(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")
	schemaName := chi.URLParam(r, "schema")

	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	schema, err := h.repo.GetSchemaByName(ctx, db.ID, schemaName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Schema not found", types.SQLState02000)
		return
	}

	if err := h.repo.DropSchema(ctx, schema.ID); err != nil {
		h.sendError(w, http.StatusNotFound, err.Error(), types.SQLState02000)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListTables handles GET /api/v2/databases/{database}/schemas/{schema}/tables.
func (h *RestAPIv2Handler) ListTables(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")
	schemaName := chi.URLParam(r, "schema")

	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	schema, err := h.repo.GetSchemaByName(ctx, db.ID, schemaName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Schema not found", types.SQLState02000)
		return
	}

	tables, err := h.repo.ListTables(ctx, schema.ID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error(), types.SQLState42000)
		return
	}

	resp := make(types.ListTablesResponse, len(tables))
	for i, t := range tables {
		resp[i] = types.TableResponse{
			Name:      t.Name,
			Database:  dbName,
			Schema:    schemaName,
			TableType: t.TableType,
			Comment:   t.Comment,
			Owner:     t.Owner,
			CreatedOn: t.CreatedAt.Format(time.RFC3339),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// GetTable handles GET /api/v2/databases/{database}/schemas/{schema}/tables/{table}.
func (h *RestAPIv2Handler) GetTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")
	schemaName := chi.URLParam(r, "schema")
	tableName := chi.URLParam(r, "table")

	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	schema, err := h.repo.GetSchemaByName(ctx, db.ID, schemaName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Schema not found", types.SQLState02000)
		return
	}

	table, err := h.repo.GetTableByName(ctx, schema.ID, tableName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Table not found", types.SQLState02000)
		return
	}

	resp := types.TableResponse{
		Name:      table.Name,
		Database:  dbName,
		Schema:    schemaName,
		TableType: table.TableType,
		Comment:   table.Comment,
		Owner:     table.Owner,
		CreatedOn: table.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteTable handles DELETE /api/v2/databases/{database}/schemas/{schema}/tables/{table}.
func (h *RestAPIv2Handler) DeleteTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")
	schemaName := chi.URLParam(r, "schema")
	tableName := chi.URLParam(r, "table")

	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	schema, err := h.repo.GetSchemaByName(ctx, db.ID, schemaName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Schema not found", types.SQLState02000)
		return
	}

	table, err := h.repo.GetTableByName(ctx, schema.ID, tableName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Table not found", types.SQLState02000)
		return
	}

	if err := h.repo.DropTable(ctx, table.ID); err != nil {
		h.sendError(w, http.StatusNotFound, err.Error(), types.SQLState02000)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AlterDatabase handles PUT /api/v2/databases/{database}.
func (h *RestAPIv2Handler) AlterDatabase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")

	var req types.AlterDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body", types.SQLState42000)
		return
	}

	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	// Update comment if provided
	if req.Comment != nil {
		if err := h.repo.UpdateDatabaseComment(ctx, db.ID, *req.Comment); err != nil {
			h.sendError(w, http.StatusInternalServerError, err.Error(), types.SQLState42000)
			return
		}
	}

	// Get updated database
	db, err = h.repo.GetDatabase(ctx, db.ID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error(), types.SQLState42000)
		return
	}

	resp := types.DatabaseResponse{
		Name:      db.Name,
		Comment:   db.Comment,
		Owner:     db.Owner,
		CreatedOn: db.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// CreateTable handles POST /api/v2/databases/{database}/schemas/{schema}/tables.
func (h *RestAPIv2Handler) CreateTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")
	schemaName := chi.URLParam(r, "schema")

	var req types.TableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body", types.SQLState42000)
		return
	}

	if req.Name == "" {
		h.sendError(w, http.StatusBadRequest, "Table name is required", types.SQLState42000)
		return
	}

	if len(req.Columns) == 0 {
		h.sendError(w, http.StatusBadRequest, "At least one column is required", types.SQLState42000)
		return
	}

	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	schema, err := h.repo.GetSchemaByName(ctx, db.ID, schemaName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Schema not found", types.SQLState02000)
		return
	}

	// Convert column definitions
	columns := make([]metadata.ColumnDef, len(req.Columns))
	for i, col := range req.Columns {
		columns[i] = metadata.ColumnDef{
			Name:       col.Name,
			Type:       col.Type,
			Nullable:   col.Nullable,
			Default:    col.Default,
			PrimaryKey: col.PrimaryKey,
		}
	}

	table, err := h.repo.CreateTable(ctx, schema.ID, req.Name, columns, req.Comment)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error(), types.SQLState42000)
		return
	}

	resp := types.TableResponse{
		Name:      table.Name,
		Database:  dbName,
		Schema:    schemaName,
		TableType: table.TableType,
		Comment:   table.Comment,
		Owner:     table.Owner,
		CreatedOn: table.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// AlterTable handles PUT /api/v2/databases/{database}/schemas/{schema}/tables/{table}.
func (h *RestAPIv2Handler) AlterTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbName := chi.URLParam(r, "database")
	schemaName := chi.URLParam(r, "schema")
	tableName := chi.URLParam(r, "table")

	var req types.AlterTableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body", types.SQLState42000)
		return
	}

	db, err := h.repo.GetDatabaseByName(ctx, dbName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found", types.SQLState02000)
		return
	}

	schema, err := h.repo.GetSchemaByName(ctx, db.ID, schemaName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Schema not found", types.SQLState02000)
		return
	}

	table, err := h.repo.GetTableByName(ctx, schema.ID, tableName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Table not found", types.SQLState02000)
		return
	}

	// Update comment if provided
	if req.Comment != nil {
		if err := h.repo.UpdateTableComment(ctx, table.ID, *req.Comment); err != nil {
			h.sendError(w, http.StatusInternalServerError, err.Error(), types.SQLState42000)
			return
		}
	}

	// Get updated table
	table, err = h.repo.GetTable(ctx, table.ID)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error(), types.SQLState42000)
		return
	}

	resp := types.TableResponse{
		Name:      table.Name,
		Database:  dbName,
		Schema:    schemaName,
		TableType: table.TableType,
		Comment:   table.Comment,
		Owner:     table.Owner,
		CreatedOn: table.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// Warehouse Management Handlers

// ListWarehouses handles GET /api/v2/warehouses.
func (h *RestAPIv2Handler) ListWarehouses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	warehouses, err := h.warehouseMgr.ListWarehouses(ctx)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error(), types.SQLState42000)
		return
	}

	resp := make(types.ListWarehousesResponse, len(warehouses))
	for i, wh := range warehouses {
		resp[i] = types.WarehouseResponse{
			Name:        wh.Name,
			State:       string(wh.State),
			Size:        wh.Size,
			Type:        "STANDARD",
			AutoSuspend: wh.AutoSuspend,
			AutoResume:  wh.AutoResume,
			Comment:     wh.Comment,
			Owner:       wh.Owner,
			CreatedOn:   wh.CreatedAt.Format(time.RFC3339),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// GetWarehouse handles GET /api/v2/warehouses/{warehouse}.
func (h *RestAPIv2Handler) GetWarehouse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	warehouseName := chi.URLParam(r, "warehouse")

	wh, err := h.warehouseMgr.GetWarehouse(ctx, warehouseName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Warehouse not found", types.SQLState02000)
		return
	}

	resp := types.WarehouseResponse{
		Name:        wh.Name,
		State:       string(wh.State),
		Size:        wh.Size,
		Type:        "STANDARD",
		AutoSuspend: wh.AutoSuspend,
		AutoResume:  wh.AutoResume,
		Comment:     wh.Comment,
		Owner:       wh.Owner,
		CreatedOn:   wh.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// CreateWarehouse handles POST /api/v2/warehouses.
func (h *RestAPIv2Handler) CreateWarehouse(w http.ResponseWriter, r *http.Request) {
	var req types.WarehouseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body", types.SQLState42000)
		return
	}

	if req.Name == "" {
		h.sendError(w, http.StatusBadRequest, "Warehouse name is required", types.SQLState42000)
		return
	}

	ctx := r.Context()

	wh, err := h.warehouseMgr.CreateWarehouse(ctx, req.Name, req.Size, req.Comment)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error(), types.SQLState42000)
		return
	}

	resp := types.WarehouseResponse{
		Name:        wh.Name,
		State:       string(wh.State),
		Size:        wh.Size,
		Type:        "STANDARD",
		AutoSuspend: wh.AutoSuspend,
		AutoResume:  wh.AutoResume,
		Comment:     wh.Comment,
		Owner:       wh.Owner,
		CreatedOn:   wh.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteWarehouse handles DELETE /api/v2/warehouses/{warehouse}.
func (h *RestAPIv2Handler) DeleteWarehouse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	warehouseName := chi.URLParam(r, "warehouse")

	if err := h.warehouseMgr.DropWarehouse(ctx, warehouseName); err != nil {
		h.sendError(w, http.StatusNotFound, err.Error(), types.SQLState02000)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ResumeWarehouse handles POST /api/v2/warehouses/{warehouse}:resume.
func (h *RestAPIv2Handler) ResumeWarehouse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	warehouseName := chi.URLParam(r, "warehouse")

	if err := h.warehouseMgr.ResumeWarehouse(ctx, warehouseName); err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error(), types.SQLState42000)
		return
	}

	wh, err := h.warehouseMgr.GetWarehouse(ctx, warehouseName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, err.Error(), types.SQLState02000)
		return
	}

	resp := types.WarehouseResponse{
		Name:        wh.Name,
		State:       string(wh.State),
		Size:        wh.Size,
		Type:        "STANDARD",
		AutoSuspend: wh.AutoSuspend,
		AutoResume:  wh.AutoResume,
		Comment:     wh.Comment,
		Owner:       wh.Owner,
		CreatedOn:   wh.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// SuspendWarehouse handles POST /api/v2/warehouses/{warehouse}:suspend.
func (h *RestAPIv2Handler) SuspendWarehouse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	warehouseName := chi.URLParam(r, "warehouse")

	if err := h.warehouseMgr.SuspendWarehouse(ctx, warehouseName); err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error(), types.SQLState42000)
		return
	}

	wh, err := h.warehouseMgr.GetWarehouse(ctx, warehouseName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, err.Error(), types.SQLState02000)
		return
	}

	resp := types.WarehouseResponse{
		Name:        wh.Name,
		State:       string(wh.State),
		Size:        wh.Size,
		Type:        "STANDARD",
		AutoSuspend: wh.AutoSuspend,
		AutoResume:  wh.AutoResume,
		Comment:     wh.Comment,
		Owner:       wh.Owner,
		CreatedOn:   wh.CreatedAt.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// convertBindings converts types.BindingValue map to query.BindingValue map.
func convertBindings(bindings map[string]*types.BindingValue) map[string]*query.BindingValue {
	if bindings == nil {
		return nil
	}

	result := make(map[string]*query.BindingValue, len(bindings))
	for key, val := range bindings {
		if val != nil {
			result[key] = &query.BindingValue{
				Type:  val.Type,
				Value: val.Value,
			}
		}
	}
	return result
}

// TranslateStatement handles POST /api/v2/translate.
//
// It answers what a statement becomes on its way to DuckDB without running it,
// which is the emulator's one genuinely unusual view: every other console shows
// the SQL you wrote, not the SQL that executes.
func (h *RestAPIv2Handler) TranslateStatement(w http.ResponseWriter, r *http.Request) {
	var req types.TranslateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body", types.SQLState42000)
		return
	}

	if req.Statement == "" {
		h.sendError(w, http.StatusBadRequest, "Statement is required", types.SQLState42000)
		return
	}

	preview, err := h.executor.PreviewTranslationWithContext(r.Context(), req.Statement, query.ExecutionContext{
		Database: req.Database,
		Schema:   req.Schema,
	})
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error(), types.SQLState42000)
		return
	}

	resp := types.TranslateResponse{
		Statement:  preview.Statement,
		Translated: preview.Translated,
		HandledBy:  string(preview.HandledBy),
		Complete:   preview.Complete,
		Note:       preview.Note,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to write translate response: %v", err)
	}
}

// ListSchemaObjects handles GET /api/v2/databases/{database}/schemas/{schema}/objects.
//
// It answers with everything a schema contains in one call, so an object
// explorer can expand a schema without issuing five requests. Tables come from
// DuckDB rather than the catalog: _metadata_tables only records tables created
// through the REST API, so a table created with SQL would otherwise be absent.
func (h *RestAPIv2Handler) ListSchemaObjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	databaseName := chi.URLParam(r, "database")
	schemaName := chi.URLParam(r, "schema")

	database, err := h.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Database not found: "+databaseName, types.SQLState42000)
		return
	}

	schema, err := h.repo.GetSchemaByName(ctx, database.ID, schemaName)
	if err != nil {
		h.sendError(w, http.StatusNotFound, "Schema not found: "+schemaName, types.SQLState42000)
		return
	}

	objects := make([]types.SchemaObject, 0)

	tables, err := h.repo.ListPhysicalTables(ctx, database.Name, schema.Name)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err.Error(), types.SQLState42000)
		return
	}
	for _, name := range tables {
		objects = append(objects, types.SchemaObject{Name: name, Kind: "table"})
	}

	if streams, err := h.repo.ListStreams(ctx, schema.ID); err == nil {
		for _, stream := range streams {
			objects = append(objects, types.SchemaObject{
				Name:   stream.Name,
				Kind:   "stream",
				Detail: "on " + stream.SourceTable,
			})
		}
	}

	if procedures, err := h.repo.ListProcedures(ctx, schema.ID); err == nil {
		for _, procedure := range procedures {
			objects = append(objects, types.SchemaObject{
				Name:   procedure.Name,
				Kind:   "procedure",
				Detail: procedure.ReturnType,
			})
		}
	}

	if tasks, err := h.repo.ListTasks(ctx, schema.ID); err == nil {
		for _, task := range tasks {
			objects = append(objects, types.SchemaObject{
				Name:   task.Name,
				Kind:   "task",
				Detail: task.State,
			})
		}
	}

	if stages, err := h.repo.ListStages(ctx, schema.ID); err == nil {
		for _, stage := range stages {
			objects = append(objects, types.SchemaObject{Name: stage.Name, Kind: "stage"})
		}
	}

	resp := types.ListSchemaObjectsResponse{
		Database: database.Name,
		Schema:   schema.Name,
		Objects:  objects,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to write schema objects response: %v", err)
	}
}

// defaultHistoryLimit caps a history listing that asks for no limit of its own.
const defaultHistoryLimit = 200

// ListStatements handles GET /api/v2/statements.
//
// Statements live in memory for the manager's TTL, so this is a recent history
// rather than a complete one: anything older, and everything from before a
// restart, is gone. The response says so rather than leaving a reader to guess
// why a statement they remember is missing.
func (h *RestAPIv2Handler) ListStatements(w http.ResponseWriter, r *http.Request) {
	limit := defaultHistoryLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			h.sendError(w, http.StatusBadRequest, "limit must be a non-negative integer", types.SQLState42000)
			return
		}
		limit = parsed
	}

	summaries := h.stmtMgr.ListStatements(limit)
	entries := make([]types.StatementHistoryEntry, 0, len(summaries))

	for i := range summaries {
		summary := &summaries[i]
		entry := types.StatementHistoryEntry{
			Handle:    summary.Handle,
			Status:    string(summary.Status),
			Statement: summary.SQLText,
			Database:  summary.Database,
			Schema:    summary.Schema,
			Warehouse: summary.Warehouse,
			CreatedOn: summary.CreatedOn.UnixMilli(),
			NumRows:   summary.RowCount,
			Code:      summary.ErrorCode,
			Message:   summary.ErrorMessage,
		}
		if summary.CompletedOn != nil {
			entry.CompletedOn = summary.CompletedOn.UnixMilli()
			entry.DurationMs = summary.CompletedOn.Sub(summary.CreatedOn).Milliseconds()
		}
		entries = append(entries, entry)
	}

	resp := types.ListStatementsResponse{
		Statements:  entries,
		RetainedFor: h.stmtMgr.RetentionPeriod().String(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to write statement history response: %v", err)
	}
}
