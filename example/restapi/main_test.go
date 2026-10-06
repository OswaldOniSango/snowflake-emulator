package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatementSendsIdentityAndReportsSQLFailure(t *testing.T) {
	oldURL, oldToken := baseURL, sessionToken
	t.Cleanup(func() { baseURL, sessionToken = oldURL, oldToken })
	sessionToken = "test-token"
	for _, sqlState := range []string{"00000", "42000"} {
		t.Run(sqlState, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing session token")
				}
				var request StatementRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Warehouse != "DEMO_WH" {
					t.Error("missing warehouse")
				}
				if err := json.NewEncoder(w).Encode(StatementResponse{SQLState: sqlState, Message: "denied"}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			baseURL = server.URL
			_, err := executeStatement("SELECT 1", "TEST_DB", "PUBLIC")
			if (err == nil) != (sqlState == "00000") {
				t.Fatalf("SQLSTATE %s error=%v", sqlState, err)
			}
		})
	}
}
