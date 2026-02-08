# Development Guide

## Prerequisites

- Go 1.24+
- PostgreSQL 14+
- Make (optional)

## Project Structure

```
veloria/
├── cmd/
│   ├── veloria/       # Main API server
│   └── veloria-indexer/    # Indexer utility
├── internal/
│   ├── api/              # HTTP handlers
│   │   ├── plugin/
│   │   ├── theme/
│   │   ├── search/
│   │   └── user/
│   ├── client/           # HTTP client utilities
│   ├── config/           # Configuration management
│   ├── db/               # Database connection
│   ├── index/            # Trigram indexing (codesearch)
│   ├── manager/          # Repository orchestration
│   ├── repo/             # Data repositories
│   └── router/           # chi router setup
└── docs/                 # Documentation
```

## Getting Started

### 1. Clone and Install Dependencies

```bash
git clone https://github.com/your-org/veloria.git
cd veloria
go mod download
```

### 2. Set Up PostgreSQL

Create a database and apply migrations (see [Migrations](migrations.md) for details):

```bash
go run ./cmd/veloria-migrate up
```

### 3. Configure Environment

Create a `.env` file:

```bash
ENV=development
DB_HOST=localhost
DB_PORT=5432
DB_DATABASE=veloria
DB_USERNAME=postgres
DB_PASSWORD=
DATA_DIR=/tmp/veloria-data
```

### 4. Prepare Test Data

For local development, create the data directory structure:

```bash
mkdir -p /tmp/veloria-data/{plugins,themes,cores}/{source,index}
```

Download a plugin for testing:

```bash
cd /tmp/veloria-data/plugins/source
curl -O https://downloads.wordpress.org/plugin/hello-dolly.zip
unzip hello-dolly.zip
rm hello-dolly.zip
```

### 5. Build and Run

```bash
# Build both binaries
go build -o veloria ./cmd/veloria
go build -o veloria-indexer ./cmd/veloria-indexer

# Index the test plugin
./veloria-indexer -repo=plugins -slug=hello-dolly -zipurl=file:///tmp/veloria-data/plugins/source/hello-dolly.zip

# Run the server
./veloria
```

### 6. Test the API

```bash
curl -X POST http://localhost:9071/api/v1/search \
  -H "Content-Type: application/json" \
  -d '{"term": "Hello", "repo": "plugins"}'
```

## Building

```bash
# Development build
go build ./cmd/veloria
go build ./cmd/veloria-indexer

# Production build (with optimizations)
CGO_ENABLED=0 go build -ldflags="-s -w" -o veloria ./cmd/veloria
CGO_ENABLED=0 go build -ldflags="-s -w" -o veloria-indexer ./cmd/veloria-indexer
```

## Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific package tests
go test ./internal/index/...

# Run with race detector
go test -race ./...
```

## Code Quality

```bash
# Format code
go fmt ./...

# Vet for common issues
go vet ./...

# Run staticcheck (install: go install honnef.co/go/tools/cmd/staticcheck@latest)
staticcheck ./...
```

## Key Packages

### internal/repo

The repository layer uses Go generics to eliminate duplication:

```go
// Repository is a generic type for managing extensions
type Repository[T Extension] struct {
    List map[string]T
    mu   sync.RWMutex
    // ...
}

// Extension interface implemented by Plugin, Theme, Core
type Extension interface {
    GetSlug() string
    GetName() string
    GetVersion() string
    GetDownloadLink() string
    GetActiveInstalls() int
    // Index management methods...
}
```

### internal/index

Trigram indexing uses the [google/codesearch](https://github.com/google/codesearch) library:

1. **Index Creation**: `index.Create()` builds a trigram index from source files
2. **Search**: `index.Search()` queries the index and greps matching files
3. **Hot-swap**: Indexes are versioned with timestamps for safe updates

### internal/manager

The Manager orchestrates all repositories and provides a unified search interface:

```go
m.Search("plugins", "wp_enqueue_script", "\\.php$", false)
```

## Adding a New Extension Type

1. Define the type in `internal/repo/`:
   ```go
   type MyExtension struct {
       *IndexedExtension `gorm:"-" json:"-"`
       // fields...
   }
   ```

2. Implement the `Extension` interface

3. Create a typed repository:
   ```go
   type MyExtensionRepo struct {
       *Repository[*MyExtension]
   }
   ```

4. Add to the Manager and router

## Debugging

### Enable Debug Logging

The application uses zerolog. Set log level via code or compile-time configuration.

### Inspect Indexes

Indexes are stored as binary files in `$DATA_DIR/<type>/index/<slug>.<timestamp>/`. Use the codesearch CLI tools to inspect:

```bash
go install github.com/google/codesearch/cmd/cindex@latest
go install github.com/google/codesearch/cmd/csearch@latest

# Search an index directly
CSEARCHINDEX=/tmp/veloria-data/plugins/index/hello-dolly.1234567890/.csearchindex \
  csearch "Hello"
```

### Database Queries

GORM debug mode can be enabled in `internal/db/db.go` for SQL logging.

## Common Issues

### "no indexes loaded"

- Ensure `DATA_DIR` points to a directory with indexed content
- Run the indexer on some plugins/themes first
- Check file permissions

### "context canceled"

- Normal during shutdown - the app uses context cancellation for graceful termination
- If occurring unexpectedly, check for timeout issues

### "mmap: too many open files"

- Increase ulimit: `ulimit -n 65536`
- This occurs when many indexes are loaded simultaneously
