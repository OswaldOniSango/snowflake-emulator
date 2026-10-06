package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nnnkkk7/snowflake-emulator/pkg/identity"
	"github.com/nnnkkk7/snowflake-emulator/pkg/query"
	"github.com/nnnkkk7/snowflake-emulator/pkg/session"
	"github.com/nnnkkk7/snowflake-emulator/pkg/warehouse"
	"github.com/nnnkkk7/snowflake-emulator/server/types"
)

type isolationFixture struct {
	handler        *RestAPIv2Handler
	router         *chi.Mux
	reader, writer *session.Session
}

func setupIsolation(t *testing.T) *isolationFixture {
	t.Helper()
	h, router := setupRestAPIv2Handler(t)
	ctx := context.Background()
	service, err := identity.NewService(ctx, h.repo)
	if err != nil {
		t.Fatal(err)
	}
	h.identity = service
	h.executor.Configure(query.WithIdentityService(service))
	h.sessionMgr = session.NewManager(time.Hour)
	h.warehouseMgr, err = warehouse.NewPersistentManager(ctx, h.repo)
	if err != nil {
		t.Fatal(err)
	}
	h.executor.Configure(query.WithWarehouseManager(h.warehouseMgr))
	h.stmtMgr.SetHistoryStore(h.repo, time.Hour, true)
	router.Get("/api/v2/statements", h.ListStatements)
	router.Post("/queries/v1/query-request", NewQueryHandler(h.executor, h.sessionMgr, service).ExecuteQuery)
	for _, sql := range []string{
		"CREATE WAREHOUSE isolation_wh",
		"CREATE TABLE TEST_DB.PUBLIC.private_rows (id INTEGER)",
		"CREATE ROLE isolation_reader", "CREATE ROLE isolation_writer",
		"CREATE USER alice PASSWORD = 'secret' DEFAULT_ROLE = isolation_reader",
		"CREATE USER bob PASSWORD = 'secret' DEFAULT_ROLE = isolation_writer",
		"GRANT USAGE ON WAREHOUSE isolation_wh TO ROLE isolation_reader",
		"GRANT USAGE ON WAREHOUSE isolation_wh TO ROLE isolation_writer",
		"GRANT USAGE ON DATABASE TEST_DB TO ROLE isolation_reader",
		"GRANT USAGE ON DATABASE TEST_DB TO ROLE isolation_writer",
		"GRANT USAGE ON SCHEMA TEST_DB.PUBLIC TO ROLE isolation_reader",
		"GRANT USAGE ON SCHEMA TEST_DB.PUBLIC TO ROLE isolation_writer",
		"GRANT SELECT ON TABLE TEST_DB.PUBLIC.private_rows TO ROLE isolation_reader",
		"GRANT INSERT ON TABLE TEST_DB.PUBLIC.private_rows TO ROLE isolation_writer",
		"GRANT UPDATE ON TABLE TEST_DB.PUBLIC.private_rows TO ROLE isolation_writer",
		"GRANT DELETE ON TABLE TEST_DB.PUBLIC.private_rows TO ROLE isolation_writer",
	} {
		if _, err := h.executor.Execute(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	createSession := func(username string) *session.Session {
		t.Helper()
		principal, err := service.Authenticate(ctx, username, "secret")
		if err != nil {
			t.Fatal(err)
		}
		sess, err := h.sessionMgr.CreateAuthenticatedSession(ctx, session.CreateInput{
			UserID: principal.UserID, Username: principal.Username, ActiveRoleID: principal.DefaultRoleID,
			ActiveRole: principal.DefaultRole, Database: "TEST_DB", Schema: "PUBLIC", Warehouse: "ISOLATION_WH",
		})
		if err != nil {
			t.Fatal(err)
		}
		return sess
	}
	return &isolationFixture{handler: h, router: router, reader: createSession("alice"), writer: createSession("bob")}
}

func (f *isolationFixture) request(method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}

func (f *isolationFixture) submit(t *testing.T, token, sql string) types.StatementResponse {
	t.Helper()
	body, err := json.Marshal(types.SubmitStatementRequest{Statement: sql})
	if err != nil {
		t.Fatal(err)
	}
	w := f.request(http.MethodPost, "/api/v2/statements", token, string(body))
	var result types.StatementResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAuthenticatedTwoUserPrivilegeWorkflow(t *testing.T) {
	f := setupIsolation(t)
	for _, test := range []struct {
		token, sql string
		allowed    bool
	}{
		{f.writer.Token, "INSERT INTO TEST_DB.PUBLIC.private_rows VALUES (1)", true},
		{f.reader.Token, "SELECT * FROM TEST_DB.PUBLIC.private_rows", true},
		{f.reader.Token, "INSERT INTO private_rows VALUES (2)", false},
		{f.reader.Token, "UPDATE private_rows SET id = 2", false},
		{f.reader.Token, "DELETE FROM private_rows", false},
		{f.reader.Token, "CREATE TABLE unauthorized (id INTEGER)", false},
		{f.writer.Token, "SELECT * FROM private_rows", false},
		{f.writer.Token, "UPDATE private_rows SET id = 2", true},
		{f.writer.Token, "DELETE FROM private_rows", true},
		{f.reader.Token, "USE ROLE ACCOUNTADMIN", false},
		{f.reader.Token, "USE ROLE isolation_writer", false},
		{f.reader.Token, "USE ROLE PUBLIC", true},
		{f.reader.Token, "SELECT * FROM private_rows", false},
		{f.reader.Token, "USE ROLE isolation_reader", true},
		{f.reader.Token, "SELECT * FROM private_rows", true},
	} {
		t.Run(test.sql, func(t *testing.T) {
			got := f.submit(t, test.token, test.sql)
			if (got.SQLState == types.SQLState00000) != test.allowed {
				t.Fatalf("allowed=%v response=%+v", test.allowed, got)
			}
		})
	}
	ctx := context.Background()
	if err := f.handler.identity.SetDefaultRole(ctx, "alice", identity.RolePublic); err != nil {
		t.Fatal(err)
	}
	if err := f.handler.identity.RevokeRoleFromUser(ctx, "isolation_reader", "alice"); err != nil {
		t.Fatal(err)
	}
	if got := f.submit(t, f.reader.Token, "SELECT 1"); got.SQLState == types.SQLState00000 {
		t.Fatal("revoked active role still works")
	}
	w := f.request(http.MethodPost, "/queries/v1/query-request", f.reader.Token, `{"sqlText":"SELECT 1"}`)
	var protocol types.QueryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &protocol); err != nil {
		t.Fatal(err)
	}
	if protocol.Success {
		t.Fatal("driver accepted revoked role")
	}
	if got := f.submit(t, f.reader.Token, "USE ROLE PUBLIC"); got.SQLState != types.SQLState00000 {
		t.Fatalf("could not recover: %+v", got)
	}
	if err := f.handler.identity.SetUserDisabled(ctx, "alice", true); err != nil {
		t.Fatal(err)
	}
	w = f.request(http.MethodGet, "/api/v2/statements", f.reader.Token, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled user status=%d", w.Code)
	}
}

func TestStatementOwnershipAcrossSessionsAndRestart(t *testing.T) {
	f := setupIsolation(t)
	alice := f.submit(t, f.reader.Token, "SELECT * FROM private_rows")
	if alice.SQLState != types.SQLState00000 {
		t.Fatalf("SELECT failed: %+v", alice)
	}
	_ = f.submit(t, f.writer.Token, "INSERT INTO private_rows VALUES (3)")
	queued := f.handler.stmtMgr.CreateStatementForUser("SELECT 1", "TEST_DB", "PUBLIC", "ISOLATION_WH", f.reader.UserID)
	f.handler.stmtMgr.UpdateStatus(queued.Handle, query.StatementStatusQueued)
	for _, test := range []struct {
		token  string
		status int
	}{
		{f.reader.Token, http.StatusOK}, {f.writer.Token, http.StatusNotFound}, {"", http.StatusUnauthorized}, {"invalid", http.StatusUnauthorized},
	} {
		w := f.request(http.MethodGet, "/api/v2/statements/"+alice.StatementHandle, test.token, "")
		if w.Code != test.status {
			t.Fatalf("result expected %d, got %d: %s", test.status, w.Code, w.Body.String())
		}
	}
	w := f.request(http.MethodPost, "/api/v2/statements/"+queued.Handle+"/cancel", f.writer.Token, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("other user cancel status=%d", w.Code)
	}
	w = f.request(http.MethodPost, "/api/v2/statements/"+queued.Handle+"/cancel", f.reader.Token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner queued cancel: %s", w.Body.String())
	}
	if err := f.handler.sessionMgr.CloseSession(context.Background(), f.reader.Token); err != nil {
		t.Fatal(err)
	}
	w = f.request(http.MethodGet, "/api/v2/statements/"+alice.StatementHandle, f.reader.Token, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatal("logged-out token still works")
	}
	// A new session of the same stable user can read its own retained history.
	sess, err := f.handler.sessionMgr.CreateAuthenticatedSession(context.Background(), session.CreateInput{
		UserID: f.reader.UserID, Username: f.reader.Username, ActiveRoleID: f.reader.ActiveRoleID, ActiveRole: f.reader.ActiveRole,
		Database: "TEST_DB", Schema: "PUBLIC",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.handler.stmtMgr = query.NewStatementManager(time.Hour)
	f.handler.stmtMgr.SetHistoryStore(f.handler.repo, time.Hour, true)
	w = f.request(http.MethodGet, "/api/v2/statements?limit=1", sess.Token, "")
	var history types.ListStatementsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Statements) != 1 || history.Statements[0].Handle != queued.Handle {
		t.Fatalf("wrong user history: %s", w.Body.String())
	}
	for _, token := range []string{"", "invalid", f.reader.Token} {
		w = f.request(http.MethodPost, "/api/v2/statements", token, `{"statement":"SELECT 1"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("invalid session accepted: %s", w.Body.String())
		}
	}
}
