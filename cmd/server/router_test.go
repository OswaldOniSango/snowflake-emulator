package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubUI stands in for the embedded console: it answers 200 with a marker body
// so that tests can tell a console response from an API one.
type stubUI struct{}

const stubUIBody = "CONSOLE"

func (stubUI) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(stubUIBody))
}

// TestRouterPrecedence pins the contract the console must never break: it may
// answer only paths that no API route claims, and it must not absorb method
// mismatches on routes that do exist.
func TestRouterPrecedence(t *testing.T) {
	// Handlers are nil: these cases must be resolved by routing alone, never by
	// reaching a handler. A nil dereference here means the route was matched.
	r := newRouter(nil, nil, nil, stubUI{})

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "unclaimed root goes to the console",
			method:     http.MethodGet,
			path:       "/",
			wantStatus: http.StatusOK,
			wantBody:   stubUIBody,
		},
		{
			name:       "unclaimed client route goes to the console",
			method:     http.MethodGet,
			path:       "/worksheets/42",
			wantStatus: http.StatusOK,
			wantBody:   stubUIBody,
		},
		{
			name:       "HEAD on an existing API route stays a 405",
			method:     http.MethodHead,
			path:       "/health",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "GET on a POST-only session route stays a 405",
			method:     http.MethodGet,
			path:       "/session/v1/login-request",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "GET on the telemetry route stays a 405",
			method:     http.MethodGet,
			path:       "/telemetry/send",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			// /statements now answers GET as well, so the 405 case moves to a
			// route that is still POST-only.
			name:       "GET on a POST-only cancel route stays a 405",
			method:     http.MethodGet,
			path:       "/api/v2/statements/abc/cancel",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "unknown API paths answer as the API, not the console",
			method:     http.MethodGet,
			path:       "/api/v2/nope",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unknown nested API paths answer as the API",
			method:     http.MethodGet,
			path:       "/api/v2/databases/DB/schemas/S/nope",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if tt.wantStatus != http.StatusOK {
				if got := rec.Body.String(); got == stubUIBody {
					t.Errorf("console answered %s %s; it must not shadow API routes", tt.method, tt.path)
				}
			}
		})
	}
}

func TestAPINotFoundRespondsAsJSON(t *testing.T) {
	r := newRouter(nil, nil, nil, stubUI{})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v2/nope", nil))

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if body := rec.Body.String(); body == "" {
		t.Error("expected a JSON error body, got an empty response")
	}
}

// TestRouterWithoutConsole covers a binary whose console failed to initialise:
// the API must keep working and unclaimed paths must 404 rather than panic.
func TestRouterWithoutConsole(t *testing.T) {
	r := newRouter(nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
