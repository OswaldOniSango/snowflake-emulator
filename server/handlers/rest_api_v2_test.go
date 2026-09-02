package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/go-cmp/cmp"
	"github.com/nnnkkk7/snowflake-emulator/pkg/connection"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"github.com/nnnkkk7/snowflake-emulator/pkg/query"
	"github.com/nnnkkk7/snowflake-emulator/server/types"
)

// setupRestAPIv2Handler creates a test handler with dependencies.
func setupRestAPIv2Handler(t *testing.T) (*RestAPIv2Handler, *chi.Mux) {
	t.Helper()

	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("failed to open DuckDB: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close DB: %v", err)
		}
	})

	connMgr := connection.NewManager(db)
	repo, err := metadata.NewRepository(connMgr)
	if err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}
	testDatabase, err := repo.CreateDatabase(context.Background(), "TEST_DB", "")
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	if _, err := repo.GetSchemaByName(context.Background(), testDatabase.ID, "PUBLIC"); err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}

	executor := query.NewExecutor(connMgr, repo)
	stmtMgr := query.NewStatementManager(1 * time.Hour)

	handler := NewRestAPIv2Handler(executor, stmtMgr, repo)

	// Setup router
	r := chi.NewRouter()
	r.Route("/api/v2", func(r chi.Router) {
		r.Post("/statements", handler.SubmitStatement)
		r.Get("/statements/{handle}", handler.GetStatement)
		r.Post("/statements/{handle}/cancel", handler.CancelStatement)
	})

	return handler, r
}

func TestRestAPIv2Handler_SubmitStatement_Sync(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	reqBody := types.SubmitStatementRequest{
		Statement: "SELECT 1 AS num",
		Database:  "TEST_DB",
		Schema:    "PUBLIC",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/statements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp types.StatementResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.StatementHandle == "" {
		t.Error("Expected statement handle to be set")
	}

	if resp.Code != types.ResponseCodeSuccess {
		t.Errorf("Expected code %s, got %s", types.ResponseCodeSuccess, resp.Code)
	}

	if resp.SQLState != types.SQLState00000 {
		t.Errorf("Expected SQLState %s, got %s", types.SQLState00000, resp.SQLState)
	}

	if resp.ResultSetMetaData == nil {
		t.Error("Expected ResultSetMetaData to be set")
	}

	if resp.Data == nil || len(resp.Data) == 0 {
		t.Error("Expected data to be returned")
	}
}

func TestRestAPIv2Handler_SubmitStatement_WithBindings(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	reqBody := types.SubmitStatementRequest{
		Statement: "SELECT :1 AS num, :2 AS name",
		Database:  "TEST_DB",
		Schema:    "PUBLIC",
		Bindings: map[string]*types.BindingValue{
			"1": {Type: "FIXED", Value: "42"},
			"2": {Type: "TEXT", Value: "hello"},
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/statements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp types.StatementResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Code != types.ResponseCodeSuccess {
		t.Errorf("Expected code %s, got %s. Message: %s", types.ResponseCodeSuccess, resp.Code, resp.Message)
	}

	if resp.Data == nil || len(resp.Data) == 0 {
		t.Error("Expected data to be returned")
		return
	}

	// Check that the values are correct
	if len(resp.Data[0]) != 2 {
		t.Errorf("Expected 2 columns, got %d", len(resp.Data[0]))
	}
}

func TestRestAPIv2Handler_StreamUsesRequestContext(t *testing.T) {
	handler, router := setupRestAPIv2Handler(t)
	ctx := context.Background()
	database, err := handler.repo.CreateDatabase(ctx, "LEARNING_DB", "")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if _, err := handler.repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatalf("GetSchemaByName() error = %v", err)
	}

	execute := func(statement string) types.StatementResponse {
		t.Helper()
		requestBody := types.SubmitStatementRequest{Statement: statement, Database: "LEARNING_DB", Schema: "PUBLIC"}
		body, err := json.Marshal(requestBody)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v2/statements", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		responseRecorder := httptest.NewRecorder()
		router.ServeHTTP(responseRecorder, request)

		var response types.StatementResponse
		if err := json.Unmarshal(responseRecorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("response decode error = %v; body = %s", err, responseRecorder.Body.String())
		}
		if response.Code != types.ResponseCodeSuccess {
			t.Fatalf("statement %q failed: %s", statement, response.Message)
		}
		return response
	}

	execute("CREATE TABLE users (ID INTEGER, NAME VARCHAR)")
	execute("CREATE STREAM users_stream ON TABLE users")
	execute("INSERT INTO users VALUES (1, 'Oswaldo')")
	response := execute("SELECT * FROM users_stream")
	if len(response.Data) != 1 || response.Data[0][1] != "Oswaldo" {
		t.Fatalf("stream response data = %#v", response.Data)
	}
}

func TestRestAPIv2Handler_SubmitStatement_EmptyStatement(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	reqBody := types.SubmitStatementRequest{
		Statement: "",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/statements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	// Should return error
	var resp types.StatementResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.SQLState == types.SQLState00000 {
		t.Error("Expected error SQLState for empty statement")
	}
}

func TestRestAPIv2Handler_SubmitStatement_InvalidSQL(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	reqBody := types.SubmitStatementRequest{
		Statement: "INVALID SQL STATEMENT",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/statements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	var resp types.StatementResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Invalid SQL should fail
	if resp.SQLState == types.SQLState00000 {
		t.Error("Expected error SQLState for invalid SQL")
	}
}

func TestRestAPIv2Handler_GetStatement(t *testing.T) {
	handler, router := setupRestAPIv2Handler(t)

	// First, submit a statement
	reqBody := types.SubmitStatementRequest{
		Statement: "SELECT 1 AS num",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/statements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	var submitResp types.StatementResponse
	json.Unmarshal(rr.Body.Bytes(), &submitResp)

	// Now get the statement
	req = httptest.NewRequest(http.MethodGet, "/api/v2/statements/"+submitResp.StatementHandle, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr = httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var getResp types.StatementResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if getResp.StatementHandle != submitResp.StatementHandle {
		t.Errorf("Expected handle %s, got %s", submitResp.StatementHandle, getResp.StatementHandle)
	}

	_ = handler // Use handler to avoid unused warning
}

func TestRestAPIv2Handler_GetStatement_NotFound(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/statements/non-existing-handle", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestRestAPIv2Handler_CancelStatement(t *testing.T) {
	handler, router := setupRestAPIv2Handler(t)

	// Create a statement directly in the manager (simulating a long-running query)
	stmt := handler.stmtMgr.CreateStatement("SELECT pg_sleep(100)", "TEST_DB", "PUBLIC", "")
	handler.stmtMgr.UpdateStatus(stmt.Handle, query.StatementStatusRunning)

	// Set a mock cancel function
	cancelled := false
	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	handler.stmtMgr.SetCancelFunc(stmt.Handle, func() {
		cancelled = true
		cancelFunc()
	})
	_ = cancelCtx // Use cancelCtx to avoid unused warning

	// Cancel the statement
	req := httptest.NewRequest(http.MethodPost, "/api/v2/statements/"+stmt.Handle+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if !cancelled {
		t.Error("Expected cancel function to be called")
	}

	// Verify statement status
	updatedStmt, _ := handler.stmtMgr.GetStatement(stmt.Handle)
	if updatedStmt.Status != query.StatementStatusCanceled {
		t.Errorf("Expected status %s, got %s", query.StatementStatusCanceled, updatedStmt.Status)
	}
}

func TestRestAPIv2Handler_CancelStatement_NotFound(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/statements/non-existing-handle/cancel", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestRestAPIv2Handler_InvalidJSON(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/statements", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestRestAPIv2Handler_TranslateStatement(t *testing.T) {
	handler, _ := setupRestAPIv2Handler(t)

	tests := []struct {
		name           string
		body           string
		wantStatus     int
		wantTranslated string
		wantHandledBy  string
		wantComplete   bool
	}{
		{
			name:           "translates a function",
			body:           `{"statement":"SELECT IFF(a, 'y', 'n') FROM t"}`,
			wantStatus:     http.StatusOK,
			wantTranslated: "select IF(a, 'y', 'n') from t",
			wantHandledBy:  "translator",
			wantComplete:   true,
		},
		{
			name:           "resolves short names against the context",
			body:           `{"statement":"SELECT * FROM users","database":"TEST_DB","schema":"PUBLIC"}`,
			wantStatus:     http.StatusOK,
			wantTranslated: "select * from TEST_DB.PUBLIC_USERS",
			wantHandledBy:  "translator",
			wantComplete:   true,
		},
		{
			name:          "reports the processor that really handles it",
			body:          `{"statement":"COPY INTO t FROM @stage"}`,
			wantStatus:    http.StatusOK,
			wantHandledBy: "copy_processor",
			wantComplete:  false,
		},
		{
			name:       "rejects an empty statement",
			body:       `{"statement":""}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "rejects a malformed body",
			body:       `not json`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v2/translate", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.TranslateStatement(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			var resp types.TranslateResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decoding response: %v", err)
			}

			if tt.wantTranslated != "" && resp.Translated != tt.wantTranslated {
				t.Errorf("translated = %q, want %q", resp.Translated, tt.wantTranslated)
			}
			if resp.HandledBy != tt.wantHandledBy {
				t.Errorf("handledBy = %q, want %q", resp.HandledBy, tt.wantHandledBy)
			}
			if resp.Complete != tt.wantComplete {
				t.Errorf("complete = %v, want %v", resp.Complete, tt.wantComplete)
			}
			if !resp.Complete && resp.Note == "" {
				t.Error("an incomplete preview must explain why")
			}
		})
	}
}

// TestTranslateStatementDoesNotExecute pins the endpoint's core promise: a
// statement that would change data must leave the database untouched.
func TestTranslateStatementDoesNotExecute(t *testing.T) {
	handler, router := setupRestAPIv2Handler(t)

	create := `{"statement":"CREATE TABLE preview_probe (id INTEGER)","database":"TEST_DB","schema":"PUBLIC"}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v2/statements", strings.NewReader(create)))
	if rec.Code != http.StatusOK {
		t.Fatalf("creating the probe table: status %d, body %s", rec.Code, rec.Body.String())
	}

	// Preview a statement that would drop it.
	body := `{"statement":"DROP TABLE preview_probe","database":"TEST_DB","schema":"PUBLIC"}`
	rec = httptest.NewRecorder()
	handler.TranslateStatement(rec, httptest.NewRequest(http.MethodPost, "/api/v2/translate", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Selecting from it must still work: the preview ran nothing.
	probe := `{"statement":"SELECT * FROM preview_probe","database":"TEST_DB","schema":"PUBLIC"}`
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v2/statements", strings.NewReader(probe)))

	var resp types.StatementResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.SQLState != types.SQLState00000 {
		t.Errorf("the table is gone, so translate executed the DROP: %s", resp.Message)
	}
}

func TestRestAPIv2Handler_ListSchemaObjects(t *testing.T) {
	handler, router := setupRestAPIv2Handler(t)

	run := func(statement string) {
		t.Helper()
		body := `{"statement":` + strconv.Quote(statement) + `,"database":"TEST_DB","schema":"PUBLIC"}`
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v2/statements", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", statement, rec.Code, rec.Body.String())
		}
	}

	// Created with SQL, which is exactly what _metadata_tables does not record.
	run("CREATE TABLE users (id INTEGER)")
	run("CREATE TABLE orders_staging (id INTEGER)")
	run("CREATE STREAM users_stream ON TABLE users")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/databases/TEST_DB/schemas/PUBLIC/objects", nil)
	handler.ListSchemaObjects(rec, withURLParams(req, map[string]string{"database": "TEST_DB", "schema": "PUBLIC"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp types.ListSchemaObjectsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	byKind := map[string][]string{}
	for _, object := range resp.Objects {
		byKind[object.Kind] = append(byKind[object.Kind], object.Name)
	}

	if diff := cmp.Diff([]string{"ORDERS_STAGING", "USERS"}, byKind["table"]); diff != "" {
		t.Errorf("tables mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"USERS_STREAM"}, byKind["stream"]); diff != "" {
		t.Errorf("streams mismatch (-want +got):\n%s", diff)
	}

	for _, object := range resp.Objects {
		if strings.HasPrefix(object.Name, "_METADATA") {
			t.Errorf("internal table %q leaked into the listing", object.Name)
		}
		if strings.Contains(object.Name, "PUBLIC_") {
			t.Errorf("physical name %q leaked into the listing", object.Name)
		}
	}
}

func TestRestAPIv2Handler_ListSchemaObjectsNotFound(t *testing.T) {
	handler, _ := setupRestAPIv2Handler(t)

	tests := []struct {
		name   string
		params map[string]string
	}{
		{name: "unknown database", params: map[string]string{"database": "NOPE", "schema": "PUBLIC"}},
		{name: "unknown schema", params: map[string]string{"database": "TEST_DB", "schema": "NOPE"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/objects", nil)
			handler.ListSchemaObjects(rec, withURLParams(req, tt.params))

			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
		})
	}
}

// withURLParams attaches chi's route parameters to a request built by hand.
func withURLParams(r *http.Request, params map[string]string) *http.Request {
	routeContext := chi.NewRouteContext()
	for key, value := range params {
		routeContext.URLParams.Add(key, value)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeContext))
}
