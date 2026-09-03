// Package main provides the entry point for the Snowflake emulator server.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nnnkkk7/snowflake-emulator/pkg/connection"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"github.com/nnnkkk7/snowflake-emulator/pkg/query"
	"github.com/nnnkkk7/snowflake-emulator/pkg/session"
	"github.com/nnnkkk7/snowflake-emulator/pkg/stage"
	"github.com/nnnkkk7/snowflake-emulator/pkg/warehouse"
	"github.com/nnnkkk7/snowflake-emulator/server/apierror"
	"github.com/nnnkkk7/snowflake-emulator/server/handlers"
	"github.com/nnnkkk7/snowflake-emulator/server/types"
	"github.com/nnnkkk7/snowflake-emulator/server/ui"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = ":memory:"
	}

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Failed to close database: %v", err)
		}
	}()

	connMgr := connection.NewManager(db)

	repo, err := metadata.NewRepository(connMgr)
	if err != nil {
		log.Printf("Failed to create repository: %v", err)
		return
	}

	// The documented quickstart DSN connects to TEST_DB/PUBLIC, and execution
	// contexts are validated against the catalog, so the namespace must exist.
	if err := repo.EnsureDefaultNamespace(context.Background()); err != nil {
		log.Printf("Failed to create default namespace: %v", err)
		return
	}

	sessionMgr := session.NewManager(24 * time.Hour)
	stmtMgr := query.NewStatementManager(1 * time.Hour)

	// Statements are recorded so the console's history outlives the manager's
	// own short TTL, and survives a restart when DB_PATH names a file.
	stmtMgr.SetHistoryStore(repo, query.DefaultHistoryRetention, dbPath != ":memory:")

	executor := query.NewExecutor(connMgr, repo)

	// Initialize stage manager for COPY INTO support
	stageDir := os.Getenv("STAGE_DIR")
	if stageDir == "" {
		stageDir = "./stages"
	}
	stageMgr := stage.NewManager(repo, stageDir)

	// Initialize processors and wire to executor.
	// Due to circular dependency (processors need executor, executor needs processors),
	// we create processors first, then configure executor with them.
	copyProcessor := query.NewCopyProcessor(stageMgr, repo, executor)
	mergeProcessor := query.NewMergeProcessor(executor)
	executor.Configure(
		query.WithCopyProcessor(copyProcessor),
		query.WithMergeProcessor(mergeProcessor),
	)
	warehouseMgr := warehouse.NewManager()

	sessionHandler := handlers.NewSessionHandler(sessionMgr, repo)
	queryHandler := handlers.NewQueryHandler(executor, sessionMgr)
	restAPIHandler := handlers.NewRestAPIv2HandlerWithWarehouse(executor, stmtMgr, repo, warehouseMgr)
	taskScheduler := query.NewTaskScheduler(repo, executor, time.Second)
	taskScheduler.Start(context.Background())
	defer taskScheduler.Stop()

	uiHandler, err := ui.Handler()
	if err != nil {
		// A broken console must not take the API down with it.
		log.Printf("Web console unavailable: %v", err)
		uiHandler = nil
	} else if !ui.IsBuilt() {
		log.Printf("Web console not built; run 'make ui-build' to enable it")
	}

	r := newRouter(sessionHandler, queryHandler, restAPIHandler, uiHandler)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("Starting Snowflake Emulator on port %s", port) //nolint:gosec // G706: port is from env var at startup, not attacker-controlled
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err) //nolint:gocritic // exitAfterDefer: intentional - OS cleans up on exit
	}
}

// newRouter wires every route. It is separated from main so that the precedence
// between the API and the web console — which must never shadow each other —
// can be covered by tests. A nil uiHandler leaves the console unmounted.
func newRouter(
	sessionHandler *handlers.SessionHandler,
	queryHandler *handlers.QueryHandler,
	restAPIHandler *handlers.RestAPIv2Handler,
	uiHandler http.Handler,
) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	r.Post("/session/v1/login-request", sessionHandler.Login)
	r.Post("/session/token-request", sessionHandler.TokenRequest)
	r.Post("/session/heartbeat", sessionHandler.Heartbeat)
	r.Post("/session/renew", sessionHandler.RenewSession)
	r.Post("/session/logout", sessionHandler.Logout)
	r.Post("/session/use", sessionHandler.UseContext)
	r.Post("/session", sessionHandler.CloseSession) // gosnowflake sends POST /session?delete=true

	r.Post("/queries/v1/query-request", queryHandler.ExecuteQuery)
	r.Post("/queries/v1/abort-request", queryHandler.AbortQuery)

	// REST API v2 endpoints
	r.Route("/api/v2", func(r chi.Router) {
		// Without this the subrouter inherits the console's catch-all and
		// answers unknown API paths with HTML instead of an API error.
		r.NotFound(apiNotFound)

		// Statement endpoints
		r.Post("/statements", restAPIHandler.SubmitStatement)
		r.Get("/statements", restAPIHandler.ListStatements)
		r.Get("/statements/{handle}", restAPIHandler.GetStatement)
		r.Post("/statements/{handle}/cancel", restAPIHandler.CancelStatement)

		// Shows what a statement becomes on its way to DuckDB, without running it
		r.Post("/translate", restAPIHandler.TranslateStatement)

		// Database endpoints
		r.Get("/databases", restAPIHandler.ListDatabases)
		r.Post("/databases", restAPIHandler.CreateDatabase)
		r.Get("/databases/{database}", restAPIHandler.GetDatabase)
		r.Put("/databases/{database}", restAPIHandler.AlterDatabase)
		r.Delete("/databases/{database}", restAPIHandler.DeleteDatabase)

		// Schema endpoints
		r.Get("/databases/{database}/schemas", restAPIHandler.ListSchemas)
		r.Post("/databases/{database}/schemas", restAPIHandler.CreateSchema)
		r.Get("/databases/{database}/schemas/{schema}", restAPIHandler.GetSchema)
		r.Delete("/databases/{database}/schemas/{schema}", restAPIHandler.DeleteSchema)

		// Table endpoints
		// Everything a schema contains, for the console's object explorer
		r.Get("/databases/{database}/schemas/{schema}/objects", restAPIHandler.ListSchemaObjects)

		r.Get("/databases/{database}/schemas/{schema}/tables", restAPIHandler.ListTables)
		r.Post("/databases/{database}/schemas/{schema}/tables", restAPIHandler.CreateTable)
		r.Get("/databases/{database}/schemas/{schema}/tables/{table}", restAPIHandler.GetTable)
		r.Put("/databases/{database}/schemas/{schema}/tables/{table}", restAPIHandler.AlterTable)
		r.Delete("/databases/{database}/schemas/{schema}/tables/{table}", restAPIHandler.DeleteTable)

		// Warehouse endpoints
		r.Get("/warehouses", restAPIHandler.ListWarehouses)
		r.Post("/warehouses", restAPIHandler.CreateWarehouse)
		r.Get("/warehouses/{warehouse}", restAPIHandler.GetWarehouse)
		r.Delete("/warehouses/{warehouse}", restAPIHandler.DeleteWarehouse)
		r.Post("/warehouses/{warehouse}:resume", restAPIHandler.ResumeWarehouse)
		r.Post("/warehouses/{warehouse}:suspend", restAPIHandler.SuspendWarehouse)
	})

	// Telemetry endpoint - accept and ignore (gosnowflake sends telemetry data)
	r.Post("/telemetry/send", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	})

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			log.Printf("Failed to write health response: %v", err)
		}
	})

	// The console answers only paths no route claimed. Registering it as the
	// NotFound handler rather than a "/*" route is deliberate: a catch-all
	// route also swallows method mismatches, which would turn HEAD /health
	// into a 200 page of HTML instead of a 405.
	if uiHandler != nil {
		r.NotFound(uiHandler.ServeHTTP)
	}

	return r
}

// apiNotFound answers unknown /api/v2 paths in the API's own error shape.
func apiNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	resp := types.StatementResponse{
		Code:     apierror.CodeInvalidParameter,
		SQLState: types.SQLState42000,
		Message:  "Unknown API endpoint: " + r.URL.Path,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to write API 404 response: %v", err)
	}
}
