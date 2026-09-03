.PHONY: all build test test-unit test-integration test-e2e test-all test-coverage lint fmt ci clean run docker-build docker-up docker-down docker-test docker-logs ui-install ui-dev ui-build ui-test ui-lint ui-typecheck ui-clean ui-distclean

# npm lives in _web/ — the leading underscore keeps node_modules invisible to the
# Go toolchain, which otherwise compiles Go files shipped inside npm packages.
NPM := npm --prefix _web

# Default target
all: build

# Build all packages, with the web console embedded.
# For a Node-free Go build, use: CGO_ENABLED=1 go build -o snowflake-emulator ./cmd/server
build: ui-build
	go build ./...

# Run unit tests (pkg/ only) with race detection
test:
	go test -v -race ./pkg/...

# Alias for test
test-unit: test

# Run integration tests
test-integration:
	go test -v -race ./tests/integration/...

# Run e2e tests
test-e2e:
	go test -v -race ./tests/e2e/...

# Run all tests (unit + integration + e2e)
test-all:
	go test -v -race ./...

# Run tests with coverage (80%+ threshold enforced in CI)
test-coverage:
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out

# Run linter
lint:
	golangci-lint run --timeout=5m

# Format code
fmt:
	gofmt -w .

# CI target: mirrors the GitHub Actions workflow
ci: lint test-all ui-lint ui-typecheck ui-test ui-build

# Clean build artifacts (keeps server/ui/dist/.gitkeep so go:embed still compiles).
# Frontend dependencies survive; use ui-distclean to drop those too.
clean: ui-clean
	rm -f coverage.out
	go clean ./...

# --- Web console ------------------------------------------------------------

# Reinstall only when the manifest changes; the timestamp bump makes the
# directory a valid Make target.
_web/node_modules: _web/package.json _web/package-lock.json
	$(NPM) ci
	@touch _web/node_modules

# Install frontend dependencies
ui-install: _web/node_modules

# Vite dev server on :5173, proxying /api and /health to the emulator on :8080
ui-dev: _web/node_modules
	$(NPM) run dev

# Produce server/ui/dist, which cmd/server embeds
ui-build: _web/node_modules
	$(NPM) run build

# Frontend unit tests
ui-test: _web/node_modules
	$(NPM) run test

# Frontend linting
ui-lint: _web/node_modules
	$(NPM) run lint

# Frontend type checking
ui-typecheck: _web/node_modules
	$(NPM) run typecheck

# Remove build output, keeping the placeholder go:embed needs
ui-clean:
	rm -rf _web/dist
	find server/ui/dist -mindepth 1 ! -name .gitkeep -delete

# Also remove installed dependencies (forces a full npm ci next build)
ui-distclean: ui-clean
	rm -rf _web/node_modules

# Run the server (default port 8080, in-memory DB)
run:
	go run cmd/server/main.go

# Run with persistent DB (usage: make run-persistent DB_PATH=/path/to/file.db)
run-persistent:
	DB_PATH=$(DB_PATH) go run cmd/server/main.go

# Docker targets
docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# Run Docker integration test (builds, starts, tests, stops)
docker-test: docker-build
	docker compose up -d
	@echo "Waiting for emulator to be ready..."
	@for i in $$(seq 1 30); do \
		if curl -s http://localhost:8080/health > /dev/null 2>&1; then \
			echo "Emulator is ready"; \
			break; \
		fi; \
		echo "Waiting... ($$i/30)"; \
		sleep 1; \
	done
	go run ./example/docker
	docker compose down
