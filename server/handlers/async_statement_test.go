package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nnnkkk7/snowflake-emulator/server/types"
)

func submit(t *testing.T, router *chi.Mux, body string) (*httptest.ResponseRecorder, types.StatementResponse) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/api/v2/statements", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	var resp types.StatementResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v (body %q)", err, recorder.Body.String())
	}
	return recorder, resp
}

func poll(t *testing.T, router *chi.Mux, handle string) types.StatementResponse {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		request := httptest.NewRequest(http.MethodGet, "/api/v2/statements/"+handle, nil)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		var resp types.StatementResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode status response: %v", err)
		}
		if resp.Code != types.ResponseCodeStatementPending {
			return resp
		}
		if time.Now().After(deadline) {
			t.Fatalf("statement %s never left the pending state", handle)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSubmitAsyncReturnsAHandleBeforeTheResult(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	recorder, resp := submit(t, router,
		`{"statement":"SELECT 1 AS n","database":"TEST_DB","schema":"PUBLIC","async":true}`)

	if recorder.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", recorder.Code)
	}
	if resp.Code != types.ResponseCodeStatementPending {
		t.Errorf("code = %q, want the pending code", resp.Code)
	}
	if resp.StatementHandle == "" {
		t.Fatal("an async submission must return a handle to poll")
	}
	if resp.StatementStatusURL != "/api/v2/statements/"+resp.StatementHandle {
		t.Errorf("status URL = %q", resp.StatementStatusURL)
	}
	if len(resp.Data) != 0 {
		t.Error("an async submission carries no rows; they are fetched by handle")
	}
}

func TestAnAsyncQueryEventuallyReturnsItsRows(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	_, accepted := submit(t, router,
		`{"statement":"SELECT 7 AS n","database":"TEST_DB","schema":"PUBLIC","async":true}`)
	final := poll(t, router, accepted.StatementHandle)

	if final.Code != types.ResponseCodeSuccess {
		t.Fatalf("code = %q, message %q", final.Code, final.Message)
	}
	if len(final.Data) != 1 || len(final.Data[0]) != 1 {
		t.Fatalf("expected one row of one column, got %v", final.Data)
	}
}

// The request's context ends when the 202 is written. Running the statement on
// it would cancel every async statement the moment it was accepted.
func TestAnAsyncStatementOutlivesItsRequest(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	_, accepted := submit(t, router,
		`{"statement":"CREATE TABLE ASYNC_MADE_THIS (id INT)","database":"TEST_DB","schema":"PUBLIC","async":true}`)
	if final := poll(t, router, accepted.StatementHandle); final.Code != types.ResponseCodeSuccess {
		t.Fatalf("the statement did not finish: code %q, message %q", final.Code, final.Message)
	}

	// The table exists only if the work actually ran.
	_, check := submit(t, router,
		`{"statement":"SELECT * FROM ASYNC_MADE_THIS","database":"TEST_DB","schema":"PUBLIC"}`)
	if check.Code != types.ResponseCodeSuccess {
		t.Errorf("the async statement did not take effect: %q", check.Message)
	}
}

func TestAsyncCanBeAskedForWithAQueryParameter(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	request := httptest.NewRequest(http.MethodPost, "/api/v2/statements?async=true",
		bytes.NewBufferString(`{"statement":"SELECT 1","database":"TEST_DB","schema":"PUBLIC"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202 for ?async=true", recorder.Code)
	}
}

func TestAnAsyncFailureIsReportedByHandle(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	_, accepted := submit(t, router,
		`{"statement":"SELECT * FROM NO_SUCH_TABLE","database":"TEST_DB","schema":"PUBLIC","async":true}`)
	if accepted.StatementHandle == "" {
		t.Fatal("a statement that will fail is still accepted with a handle")
	}

	final := poll(t, router, accepted.StatementHandle)
	if final.Code == types.ResponseCodeSuccess {
		t.Fatal("a statement over a missing table should not succeed")
	}
	if final.Message == "" {
		t.Error("the failure should carry a message")
	}
}

func TestSubmittingWithoutAsyncStillAnswersWithTheResult(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	recorder, resp := submit(t, router,
		`{"statement":"SELECT 1 AS n","database":"TEST_DB","schema":"PUBLIC"}`)

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for a synchronous submission", recorder.Code)
	}
	if len(resp.Data) != 1 {
		t.Errorf("a synchronous submission carries its rows, got %v", resp.Data)
	}
}

func TestCancellingAnAsyncStatementReportsItAsCanceled(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	// Long enough to still be running when the cancel arrives.
	_, accepted := submit(t, router,
		`{"statement":"SELECT count(*) FROM range(20000000000)","database":"TEST_DB","schema":"PUBLIC","async":true}`)

	cancel := httptest.NewRequest(http.MethodPost, "/api/v2/statements/"+accepted.StatementHandle+"/cancel", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, cancel)
	if recorder.Code != http.StatusOK {
		t.Fatalf("cancel returned %d: %s", recorder.Code, recorder.Body.String())
	}

	final := poll(t, router, accepted.StatementHandle)
	if final.Code != types.ResponseCodeStatementCanceled {
		t.Errorf("code = %q, want the canceled code (message %q)", final.Code, final.Message)
	}
}

func TestCancellingAFinishedStatementIsRejected(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	_, accepted := submit(t, router,
		`{"statement":"SELECT 1","database":"TEST_DB","schema":"PUBLIC","async":true}`)
	poll(t, router, accepted.StatementHandle)

	request := httptest.NewRequest(http.MethodPost, "/api/v2/statements/"+accepted.StatementHandle+"/cancel", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code == http.StatusOK {
		t.Error("a statement that already finished cannot be canceled")
	}
}

// A DDL statement finishes with no result set. Polling one used to reach a
// nil dereference, which nothing hit while every statement ran synchronously.
func TestPollingADDLStatementReportsSuccessWithNoRows(t *testing.T) {
	_, router := setupRestAPIv2Handler(t)

	_, accepted := submit(t, router,
		`{"statement":"CREATE TABLE POLLED_DDL (id INT)","database":"TEST_DB","schema":"PUBLIC","async":true}`)
	final := poll(t, router, accepted.StatementHandle)

	if final.Code != types.ResponseCodeSuccess {
		t.Fatalf("code = %q, message %q", final.Code, final.Message)
	}
	if len(final.Data) != 0 {
		t.Errorf("a DDL statement has no rows, got %v", final.Data)
	}
	if final.ResultSetMetaData == nil {
		t.Error("the response should still describe an empty result set")
	}
}
