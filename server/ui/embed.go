// Package ui serves the compiled Mallard console from the emulator binary.
//
// The console is a static Vite bundle built into dist/ and embedded at compile
// time, so a released binary or container image needs no extra assets. When the
// bundle is absent — a fresh clone where nobody has run the frontend build —
// every UI route answers with a build hint instead of failing to compile.
package ui

import (
	"bytes"
	"embed"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// dist always contains at least .gitkeep so that this package compiles without
// a frontend build. The all: prefix is what makes that dotfile match.
//
//go:embed all:dist
var embedded embed.FS

const (
	distDir   = "dist"
	indexFile = "index.html"
	assetsDir = "assets"

	notBuiltMessage = `The web console has not been built.

Run "make ui-build" (or "npm --prefix _web ci && npm --prefix _web run build")
and restart the emulator.

The REST API at /api/v2 and the /health endpoint are unaffected.
`
)

// IsBuilt reports whether a compiled console was embedded at build time.
func IsBuilt() bool {
	_, err := fs.Stat(embedded, path.Join(distDir, indexFile))
	return err == nil
}

// Handler returns an http.Handler serving the console. It always returns a
// usable handler: when no bundle was embedded, the handler explains how to
// build one rather than 404ing. An error means the embedded tree itself is
// unreadable, which is a build-time mistake rather than a runtime condition.
func Handler() (http.Handler, error) {
	root, err := fs.Sub(embedded, distDir)
	if err != nil {
		return nil, err
	}
	return handlerFor(root)
}

// handlerFor builds the handler over an arbitrary bundle root, so that tests
// can exercise both the built and the not-built shapes without depending on
// whether the frontend happens to be compiled in this working tree.
func handlerFor(root fs.FS) (http.Handler, error) {
	index, err := fs.ReadFile(root, indexFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return notBuilt{}, nil
	case err != nil:
		return nil, err
	}

	return &console{root: root, index: index}, nil
}

// console serves the bundle, falling back to index.html so that client-side
// routes survive a page reload.
type console struct {
	root  fs.FS
	index []byte
}

func (c *console) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r) {
		return
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	underAssets := strings.HasPrefix(name, assetsDir+"/")

	if file, ok := c.open(name); ok {
		defer func() { _ = file.Close() }()
		if underAssets {
			// Vite fingerprints everything under assets/, so these never change.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		serveFile(w, r, name, file)
		return
	}

	// A miss under assets/ means a stale fingerprint, not a client-side route.
	// Falling back to HTML there would hand a <script> tag a 200 of markup and
	// surface as a confusing MIME error instead of a plain missing file.
	if underAssets {
		http.NotFound(w, r)
		return
	}

	c.serveIndex(w, r)
}

// open returns name as a regular file inside the bundle. Directories, missing
// entries and traversal attempts all report false.
func (c *console) open(name string) (fs.File, bool) {
	if name == "" || name == "." {
		return nil, false
	}
	file, err := c.root.Open(name)
	if err != nil {
		return nil, false
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		_ = file.Close()
		return nil, false
	}
	return file, true
}

func (c *console) serveIndex(w http.ResponseWriter, r *http.Request) {
	// The entry point must not be cached: after a redeploy it is the only file
	// that can point browsers at the new asset hashes.
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, indexFile, time.Time{}, bytes.NewReader(c.index))
}

// serveFile writes a bundle file. Files are served directly rather than through
// http.FileServer, which would add redirects — /index.html to /, and a trailing
// slash back to the bare path — that carry whatever headers were already set.
func serveFile(w http.ResponseWriter, r *http.Request, name string, file fs.File) {
	if seeker, ok := file.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, time.Time{}, seeker)
		return
	}

	// fs.File is not required to be seekable; buffer such files instead.
	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "failed to read console asset", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(content))
}

// notBuilt answers every UI route when no frontend bundle was embedded.
type notBuilt struct{}

func (notBuilt) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Mirror the built handler's method contract, so that a route does not
	// change shape depending on whether the frontend was compiled.
	if !allowMethod(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(notBuiltMessage))
}

// allowMethod reports whether the request may be served, answering 405 itself
// when it may not. The console is static, so only reads make sense.
func allowMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}
