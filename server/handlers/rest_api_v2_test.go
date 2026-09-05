package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
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
	"github.com/nnnkkk7/snowflake-emulator/pkg/stage"
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
	stageMgr := stage.NewManager(repo, t.TempDir())
	executor.Configure(query.WithStageManager(stageMgr))
	stmtMgr := query.NewStatementManager(1 * time.Hour)

	handler := NewRestAPIv2Handler(executor, stmtMgr, repo)
	handler.stageMgr = stageMgr

	// Setup router
	r := chi.NewRouter()
	r.Route("/api/v2", func(r chi.Router) {
		r.Post("/statements", handler.SubmitStatement)
		r.Get("/statements/{handle}", handler.GetStatement)
		r.Post("/statements/{handle}/cancel", handler.CancelStatement)
		r.Get("/databases/{database}/schemas/{schema}/stages", handler.ListStages)
		r.Post("/databases/{database}/schemas/{schema}/stages", handler.CreateStage)
		r.Delete("/databases/{database}/schemas/{schema}/stages/{stage}", handler.DeleteStage)
		r.Get("/databases/{database}/schemas/{schema}/stages/{stage}/files", handler.ListStageFiles)
		r.Post("/databases/{database}/schemas/{schema}/stages/{stage}/files", handler.UploadStageFile)
	})

	return handler, r
}

func TestRestAPIv2Handler_InternalStageCSVWorkflow(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	submit := func(statement string) types.StatementResponse {
		t.Helper()
		requestBody, err := json.Marshal(types.SubmitStatementRequest{Statement: statement, Database: "TEST_DB", Schema: "PUBLIC"})
		if err != nil {
			t.Fatalf("marshal statement request: %v", err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v2/statements", bytes.NewReader(requestBody))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", statement, response.Code, response.Body.String())
		}
		var body types.StatementResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s response: %v", statement, err)
		}
		if body.SQLState != types.SQLState00000 {
			t.Fatalf("%s failed: %s", statement, body.Message)
		}
		return body
	}

	submit("CREATE TABLE users (id INTEGER, name VARCHAR)")
	submit("CREATE STAGE users_stage COMMENT = 'CSV lessons'")

	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	part, err := writer.CreateFormFile("file", "users.csv")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write([]byte("id,name\n1,Alice\n2,Bob\n")); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart body: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v2/databases/TEST_DB/schemas/PUBLIC/stages/USERS_STAGE/files", &upload)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", response.Code, response.Body.String())
	}

	listed := submit("LIST @users_stage")
	if len(listed.Data) != 1 || listed.Data[0][0] != "users.csv" {
		t.Fatalf("LIST data = %#v, want users.csv", listed.Data)
	}

	copied := submit("COPY INTO users FROM @users_stage FILE_FORMAT = (TYPE = CSV SKIP_HEADER = 1)")
	if copied.Data[0][0] != float64(2) {
		t.Fatalf("COPY rows affected = %#v, want 2", copied.Data)
	}

	selected := submit("SELECT id, name FROM users ORDER BY id")
	want := [][]interface{}{{float64(1), "Alice"}, {float64(2), "Bob"}}
	if diff := cmp.Diff(want, selected.Data); diff != "" {
		t.Fatalf("loaded rows mismatch (-want +got):\n%s", diff)
	}
}

func TestRestAPIv2Handler_InternalStageCRUD(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)
	basePath := "/api/v2/databases/TEST_DB/schemas/PUBLIC/stages"

	request := httptest.NewRequest(http.MethodPost, basePath, strings.NewReader(`{"name":"API_STAGE","comment":"created through REST"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	var created types.StageResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode stage: %v", err)
	}
	if created.Name != "API_STAGE" || created.StageType != "INTERNAL" {
		t.Fatalf("created stage = %#v", created)
	}

	request = httptest.NewRequest(http.MethodGet, basePath, nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "API_STAGE") {
		t.Fatalf("list status = %d, body = %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodDelete, basePath+"/API_STAGE", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestRestAPIv2Handler_WarehouseSizeRoundTrip(t *testing.T) {
	handler, _ := setupRestAPIv2Handler(t)
	router := chi.NewRouter()
	router.Post("/api/v2/warehouses", handler.CreateWarehouse)
	router.Get("/api/v2/warehouses/{warehouse}", handler.GetWarehouse)

	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{name: "Snowflake request field", payload: `{"name":"SNOWFLAKE_WH","warehouse_size":"Small"}`, want: "SMALL"},
		{name: "web response field", payload: `{"name":"WEB_WH","size":"Medium"}`, want: "MEDIUM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v2/warehouses", strings.NewReader(tt.payload))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusCreated {
				t.Fatalf("POST status = %d, body = %s", response.Code, response.Body.String())
			}

			var created types.WarehouseResponse
			if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
				t.Fatalf("decode POST response: %v", err)
			}
			if created.Size != tt.want {
				t.Fatalf("POST size = %q, want %q", created.Size, tt.want)
			}

			request = httptest.NewRequest(http.MethodGet, "/api/v2/warehouses/"+created.Name, nil)
			response = httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET status = %d, body = %s", response.Code, response.Body.String())
			}
			var fetched types.WarehouseResponse
			if err := json.Unmarshal(response.Body.Bytes(), &fetched); err != nil {
				t.Fatalf("decode GET response: %v", err)
			}
			if fetched.Size != tt.want {
				t.Fatalf("GET size = %q, want %q", fetched.Size, tt.want)
			}
		})
	}
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
			wantTranslated: "SELECT IF(a, 'y', 'n') FROM t",
			wantHandledBy:  "translator",
			wantComplete:   true,
		},
		{
			name:           "resolves short names against the context",
			body:           `{"statement":"SELECT * FROM users","database":"TEST_DB","schema":"PUBLIC"}`,
			wantStatus:     http.StatusOK,
			wantTranslated: "SELECT * FROM TEST_DB.PUBLIC_USERS",
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
	run("CREATE VIEW active_users AS SELECT id FROM users")

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
	if diff := cmp.Diff([]string{"ACTIVE_USERS"}, byKind["view"]); diff != "" {
		t.Errorf("views mismatch (-want +got):\n%s", diff)
	}
	viewCount := 0
	for _, object := range resp.Objects {
		if object.Name == "ACTIVE_USERS" {
			viewCount++
			if object.Kind != "view" {
				t.Errorf("ACTIVE_USERS kind = %q, want view", object.Kind)
			}
		}
	}
	if viewCount != 1 {
		t.Errorf("ACTIVE_USERS appears %d times, want once", viewCount)
	}

	t.Run("table routes exclude views", func(t *testing.T) {
		params := map[string]string{"database": "TEST_DB", "schema": "PUBLIC"}
		recorder := httptest.NewRecorder()
		handler.ListTables(recorder, withURLParams(httptest.NewRequest(http.MethodGet, "/tables", nil), params))
		if recorder.Code != http.StatusOK {
			t.Fatalf("ListTables status = %d, body %s", recorder.Code, recorder.Body.String())
		}
		var tables types.ListTablesResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &tables); err != nil {
			t.Fatalf("decode tables: %v", err)
		}
		for _, table := range tables {
			if table.Name == "ACTIVE_USERS" {
				t.Fatal("view leaked into table listing")
			}
		}

		viewParams := map[string]string{"database": "TEST_DB", "schema": "PUBLIC", "table": "ACTIVE_USERS"}
		requests := []struct {
			name   string
			method string
			body   string
			call   func(http.ResponseWriter, *http.Request)
		}{
			{name: "get", method: http.MethodGet, call: handler.GetTable},
			{name: "alter", method: http.MethodPut, body: `{"comment":"not a table"}`, call: handler.AlterTable},
			{name: "delete", method: http.MethodDelete, call: handler.DeleteTable},
		}
		for _, request := range requests {
			t.Run(request.name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				httpRequest := httptest.NewRequest(request.method, "/tables/ACTIVE_USERS", strings.NewReader(request.body))
				request.call(recorder, withURLParams(httpRequest, viewParams))
				if recorder.Code != http.StatusNotFound {
					t.Errorf("status = %d, want %d (body %s)", recorder.Code, http.StatusNotFound, recorder.Body.String())
				}
			})
		}

		queryBody := `{"statement":"SELECT * FROM active_users","database":"TEST_DB","schema":"PUBLIC"}`
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v2/statements", strings.NewReader(queryBody)))
		if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "does not exist") {
			t.Fatalf("table DELETE route damaged view: status %d body %s", recorder.Code, recorder.Body.String())
		}
	})

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

// statusSuccess is the status a finished statement reports.
const statusSuccess = "success"

func TestRestAPIv2Handler_ListStatements(t *testing.T) {
	handler, router := setupRestAPIv2Handler(t)

	submit := func(statement string) {
		t.Helper()
		body := `{"statement":` + strconv.Quote(statement) + `,"database":"TEST_DB","schema":"PUBLIC"}`
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v2/statements", strings.NewReader(body)))
	}

	submit("CREATE TABLE history_probe (id INTEGER)")
	submit("INSERT INTO history_probe VALUES (1)")
	submit("SELECT * FROM history_probe")
	submit("SELECT * FROM does_not_exist")

	rec := httptest.NewRecorder()
	handler.ListStatements(rec, httptest.NewRequest(http.MethodGet, "/api/v2/statements", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp types.ListStatementsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if len(resp.Statements) != 4 {
		t.Fatalf("got %d statements, want 4", len(resp.Statements))
	}
	if resp.RetainedFor == "" {
		t.Error("the response should say how long statements are kept")
	}

	t.Run("newest first", func(t *testing.T) {
		if !strings.Contains(resp.Statements[0].Statement, "does_not_exist") {
			t.Errorf("first entry = %q, want the most recent statement", resp.Statements[0].Statement)
		}
	})

	t.Run("a failure carries its code", func(t *testing.T) {
		failed := resp.Statements[0]
		if failed.Status != "failed" {
			t.Errorf("status = %q, want failed", failed.Status)
		}
		if failed.Code == "" || failed.Message == "" {
			t.Error("a failed statement should carry its code and message")
		}
	})

	t.Run("DDL and DML are reported as finished", func(t *testing.T) {
		// They carry no result set, so nothing used to mark them complete and
		// every one of them read as still running.
		for _, entry := range resp.Statements {
			if strings.HasPrefix(entry.Statement, "CREATE") || strings.HasPrefix(entry.Statement, "INSERT") {
				if entry.Status != statusSuccess {
					t.Errorf("%q reported %q, want success", entry.Statement, entry.Status)
				}
				if entry.CompletedOn == 0 {
					t.Errorf("%q carries no completion time", entry.Statement)
				}
			}
		}
	})
}

func TestRestAPIv2Handler_ListStatementsLimit(t *testing.T) {
	handler, router := setupRestAPIv2Handler(t)

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		body := `{"statement":"SELECT ` + strconv.Itoa(i) + `"}`
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v2/statements", strings.NewReader(body)))
	}

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantCount  int
	}{
		{name: "limits the listing", query: "?limit=2", wantStatus: http.StatusOK, wantCount: 2},
		{name: "zero means no limit", query: "?limit=0", wantStatus: http.StatusOK, wantCount: 3},
		{name: "no limit given", query: "", wantStatus: http.StatusOK, wantCount: 3},
		{name: "rejects a non-number", query: "?limit=abc", wantStatus: http.StatusBadRequest},
		{name: "rejects a negative", query: "?limit=-1", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ListStatements(rec, httptest.NewRequest(http.MethodGet, "/api/v2/statements"+tt.query, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			var resp types.ListStatementsResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decoding response: %v", err)
			}
			if len(resp.Statements) != tt.wantCount {
				t.Errorf("got %d statements, want %d", len(resp.Statements), tt.wantCount)
			}
		})
	}
}
