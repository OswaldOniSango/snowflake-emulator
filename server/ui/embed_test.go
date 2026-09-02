package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const testIndex = "<!doctype html><title>Mallard Console</title>"

func builtBundle() fstest.MapFS {
	return fstest.MapFS{
		indexFile:                 {Data: []byte(testIndex)},
		"assets/index-a1b2c3.js":  {Data: []byte("console.log(1)")},
		"assets/index-a1b2c3.css": {Data: []byte(":root{}")},
		"favicon.ico":             {Data: []byte("icon")},
		"nested/page/asset.txt":   {Data: []byte("nested")},
	}
}

func newHandler(t *testing.T, root fstest.MapFS) http.Handler {
	t.Helper()
	h, err := handlerFor(root)
	if err != nil {
		t.Fatalf("handlerFor() returned error: %v", err)
	}
	return h
}

// do performs a request against h. Callers own the returned body and must close
// it; the bodyclose linter only recognises a Close on a syntactically visible
// path, so deferring inside a helper would not satisfy it.
func do(t *testing.T, h http.Handler, method, target string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec.Result()
}

func bodyOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return string(b)
}

func TestHandlerServesBundle(t *testing.T) {
	h := newHandler(t, builtBundle())

	tests := []struct {
		name        string
		target      string
		wantStatus  int
		wantBody    string
		wantCaching string
	}{
		{
			name:        "root serves the entry point",
			target:      "/",
			wantStatus:  http.StatusOK,
			wantBody:    testIndex,
			wantCaching: "no-cache",
		},
		{
			name:        "fingerprinted assets are immutable",
			target:      "/assets/index-a1b2c3.js",
			wantStatus:  http.StatusOK,
			wantBody:    "console.log(1)",
			wantCaching: "public, max-age=31536000, immutable",
		},
		{
			name:       "unfingerprinted files are served without immutable caching",
			target:     "/favicon.ico",
			wantStatus: http.StatusOK,
			wantBody:   "icon",
		},
		{
			name:        "unknown client route falls back to the entry point",
			target:      "/worksheets/42",
			wantStatus:  http.StatusOK,
			wantBody:    testIndex,
			wantCaching: "no-cache",
		},
		{
			name:        "directory paths fall back rather than listing",
			target:      "/assets",
			wantStatus:  http.StatusOK,
			wantBody:    testIndex,
			wantCaching: "no-cache",
		},
		{
			name:       "path traversal cannot escape the bundle",
			target:     "/../../go.mod",
			wantStatus: http.StatusOK,
			wantBody:   testIndex,
		},
		{
			// A stale fingerprint must not be answered with markup: a <script>
			// tag receiving HTML with a 200 surfaces as a confusing MIME error.
			name:       "a missing asset is a 404, not the entry point",
			target:     "/assets/index-deadbee.js",
			wantStatus: http.StatusNotFound,
			wantBody:   "404 page not found\n",
		},
		{
			name:       "index.html is served in place rather than redirected",
			target:     "/index.html",
			wantStatus: http.StatusOK,
			wantBody:   testIndex,
		},
		{
			name:       "nested files are served from the bundle",
			target:     "/nested/page/asset.txt",
			wantStatus: http.StatusOK,
			wantBody:   "nested",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := do(t, h, http.MethodGet, tt.target)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if got := bodyOf(t, resp); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
			if tt.wantCaching != "" {
				if got := resp.Header.Get("Cache-Control"); got != tt.wantCaching {
					t.Errorf("Cache-Control = %q, want %q", got, tt.wantCaching)
				}
			}
		})
	}
}

func TestHandlerHeadRequestOmitsBody(t *testing.T) {
	h := newHandler(t, builtBundle())

	resp := do(t, h, http.MethodHead, "/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := bodyOf(t, resp); got != "" {
		t.Errorf("HEAD body = %q, want empty", got)
	}
}

func TestHandlerRejectsWriteMethods(t *testing.T) {
	h := newHandler(t, builtBundle())

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			resp := do(t, h, method, "/")
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
			}
			if got := resp.Header.Get("Allow"); got != "GET, HEAD" {
				t.Errorf("Allow = %q, want %q", got, "GET, HEAD")
			}
		})
	}
}

// TestHandlerAssetCachingIsNotLeaked guards against the immutable header
// escaping onto anything other than a fingerprinted file.
func TestHandlerAssetCachingIsNotLeaked(t *testing.T) {
	h := newHandler(t, builtBundle())

	for _, target := range []string{"/", "/favicon.ico", "/worksheets/42", "/assets/missing.js"} {
		t.Run(target, func(t *testing.T) {
			resp := do(t, h, http.MethodGet, target)
			defer func() { _ = resp.Body.Close() }()

			if got := resp.Header.Get("Cache-Control"); strings.Contains(got, "immutable") {
				t.Errorf("Cache-Control = %q; immutable must be reserved for fingerprinted assets", got)
			}
		})
	}
}

func TestHandlerWithoutBundleRejectsWriteMethods(t *testing.T) {
	// The method contract must not depend on whether the frontend was built.
	h := newHandler(t, fstest.MapFS{".gitkeep": {Data: []byte{}}})

	resp := do(t, h, http.MethodPost, "/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHandlerWithoutBundleExplainsHowToBuild(t *testing.T) {
	// A fresh clone: dist/ holds only the placeholder that keeps go:embed happy.
	h := newHandler(t, fstest.MapFS{".gitkeep": {Data: []byte{}}})

	resp := do(t, h, http.MethodGet, "/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if got := bodyOf(t, resp); got != notBuiltMessage {
		t.Errorf("body = %q, want the build hint", got)
	}
}

// TestHandlerOnEmbeddedBundle guards the real embedded tree: whether or not the
// frontend has been built in this working tree, constructing the handler must
// succeed and answering the root must not error.
func TestHandlerOnEmbeddedBundle(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	resp := do(t, h, http.MethodGet, "/")
	defer func() { _ = resp.Body.Close() }()

	wantStatus := http.StatusServiceUnavailable
	if IsBuilt() {
		wantStatus = http.StatusOK
	}
	if resp.StatusCode != wantStatus {
		t.Errorf("status = %d, want %d (IsBuilt() = %v)", resp.StatusCode, wantStatus, IsBuilt())
	}
}
