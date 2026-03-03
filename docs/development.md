# Development Guide

## Prerequisites

- **Go 1.26.0**
- **Node.js 22+ and npm** (for frontend assets)
- **PostgreSQL 17+** (via Docker or native install)
- **MinIO** (S3-compatible storage, via Docker)
- **Veloria binary** built from `./cmd/veloria` (single binary with subcommands)

## Quick Start

### 1. Start Infrastructure

```bash
docker compose up -d
```

This starts PostgreSQL, MinIO, and Mailpit. See `docker-compose.yml` for port mappings.

### 2. Configure Environment

Copy the example env file and adjust values:

```bash
cp .env.example .env
```

Key values for local development:

```env
ENV=development
DB_HOST=localhost
DB_PORT=5432
DB_DATABASE=veloria
DB_USERNAME=postgres
DB_PASSWORD=postgres
S3_ENDPOINT=localhost:9000
S3_ACCESS_KEY=minioadmin
S3_SECRET_KEY=minioadmin
```

In development mode (`ENV=development`), the `.env` file is loaded automatically and `DB_SSLMODE` defaults to `disable`.

### 3. Run Migrations

```bash
go run ./cmd/veloria migrate up
```

The migrate command loads your existing environment config (`.env` in development), builds the DB connection string, and runs migrations without manual DSN input.

### 4. Build and Run

```bash
# Build frontend assets (required after CSS/template/dependency changes)
go generate ./assets/...

# Build all binaries
go build ./...

# Run the server
go run ./cmd/veloria

# Run with debug logging
APP_DEBUG=true go run ./cmd/veloria
```

The server starts on port 9071 by default (configurable via `PORT` env var).

## Project Structure

```
cmd/
└── veloria/           # Single binary with subcommands (serve, index, migrate, version)

internal/              # All application packages (see architecture.md)
migrations/            # SQL migration files (embedded in binary)
docs/                  # Developer documentation
```

The server invokes itself as a subprocess (`veloria index`) for indexing, so there is no separate indexer binary to build or put on `PATH`.

## Common Tasks

### Adding a Migration

Create a new migration file following the naming convention:

```bash
go run ./cmd/veloria migrate create <description> sql
```

This creates a file like `migrations/20260222000001_<description>.sql` with `-- +goose Up` and `-- +goose Down` sections.

### Adding a New API Endpoint

1. Create a handler function in the appropriate package (`internal/plugin/`, `internal/theme/`, `internal/core/`, `internal/search/`)
2. The handler should accept its dependencies as parameters (DB, stats provider, etc.) and return `http.Handler`
3. Use `api.WriteJSON()` and `api.WriteSuccessJSON()` for consistent JSON responses
4. Use `api.APIError` helpers (`api.ErrBadRequest`, `api.ErrNotFound`, etc.) for error responses
5. Register the route in `internal/router/router.go`

### Adding a New Web Page

1. Create a template in `templates/`
2. Add a handler in the appropriate package that takes `*web.Deps`
3. Use `d.PageData(r)` to get common template data (user, search status, etc.)
4. Register the route in `internal/router/router.go`

### Working with the Extension Stores

The stores follow a consistent pattern. Each concrete store (`PluginStore`, `ThemeStore`, `CoreStore`) embeds `ExtensionStore[T]` and implements `DataSource`:

```go
// Creating a store (done in app.go)
pr := repo.NewPluginStore(ctx, db, cfg, logger, cache, apiClient)

// The store satisfies DataSource
var ds repo.DataSource = pr
```

Key methods on `DataSource`:
- `Load()` — load from DB + disk indexes (called during startup)
- `PrepareUpdates()` — discover new/changed extensions, return index tasks
- `Search(term, opts, progressFn)` — search across all indexed extensions
- `GetExtension(slug)` — look up a single extension by slug
- `MakeReindexTaskBySlug(slug)` — create an ad-hoc reindex task

### Modifying Web Dependencies

If a handler needs access to a new capability from the Manager:

1. Define a narrow interface in `internal/web/interfaces.go` (1-2 methods)
2. Add the interface as a field on `web.Deps`
3. Implement the interface on `*Manager`
4. Wire it in `app.New()` when constructing `web.NewDeps()`

## Code Style

- **Error handling**: Use `fmt.Errorf("context: %w", err)` for wrapping. Return errors to callers; don't log and return.
- **Logging**: Use the `*zerolog.Logger` passed via constructor. Debug-level for routine operations, Info for lifecycle events, Error for failures.
- **Concurrency**: Use explicit variable capture in goroutine closures. Use `sync.RWMutex` for read-heavy shared state.
- **Interfaces**: Define at the consumer site. Keep them narrow (1-3 methods). Use hand-written fakes for testing.
- **Configuration**: All config comes from environment variables via struct tags. No config files.

## Useful Commands

```bash
# Run all unit tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run integration tests (requires Docker services running)
go test -tags integration ./...

# Run tests for a specific package
go test ./internal/manager/

# Check for issues
go vet ./...

# Build all binaries
go build ./...
```
