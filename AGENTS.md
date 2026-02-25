# Claude Code Agent Instructions for Veloria

**System:** Veloria - Code Search Engine for the WordPress Ecosystem
**Tech Stack:** Go 1.25, PostgreSQL 17, MinIO/S3, google/codesearch
**Status:** Production - High-Performance Search Service

---

## Critical Context

You are working on a **production code search engine** that indexes and searches the **entire WordPress plugin and theme ecosystem**.

**Performance is the #1 Priority:**
- Resource usage (CPU, Memory, Disk) is a critical concern
- Search operations must remain fast under concurrent load
- Memory-mapped indexes require careful lifecycle management
- Connection pools and caches must be properly sized

**Quality Requirements:**
- Idiomatic Go code following standard conventions
- Thread-safe operations (this is a highly concurrent system)
- Graceful degradation under load
- Comprehensive error handling with Sentry integration

**Never:**
- Block search operations during index updates
- Hold locks longer than necessary (especially write locks)
- Create memory leaks (unclosed indexes, forgotten goroutines)
- Skip graceful shutdown handling
- Ignore context cancellation

---

## First Steps for Any Task

### 1. Read the Documentation

**ALWAYS start by reading:**
- [docs/architecture.md](docs/architecture.md) - System design and component overview
- [docs/api.md](docs/api.md) - REST API documentation
- [docs/configuration.md](docs/configuration.md) - Environment variables
- [docs/development.md](docs/development.md) - Local setup and debugging

**For database work:**
- [docs/migrations.md](docs/migrations.md) - Migration procedures
- Review existing migrations in `migrations/` directory

**Use Task tool with Explore agent** when you need to:
- Understand how a feature works across multiple packages
- Find all usages of a type or function
- Explore the concurrency model

### 2. Use TodoWrite for Complex Tasks

For any task with **3+ steps or affecting multiple packages**, create a todo list:

```typescript
TodoWrite({
  todos: [
    {content: "Read existing index implementation", status: "in_progress", activeForm: "Reading index code"},
    {content: "Profile memory usage", status: "pending", activeForm: "Profiling memory"},
    {content: "Implement optimization", status: "pending", activeForm: "Implementing optimization"},
    {content: "Run benchmarks", status: "pending", activeForm: "Running benchmarks"},
    {content: "Write tests", status: "pending", activeForm: "Writing tests"}
  ]
})
```

### 3. Use EnterPlanMode for Performance-Critical Changes

Use EnterPlanMode when:
- Modifying index loading or search operations
- Changing concurrency patterns (mutex usage, goroutines)
- Altering database queries or connection handling
- Modifying the hot-swap index mechanism
- Adding new background tasks

**Don't use for:**
- Simple bug fixes
- Documentation updates
- Adding new API endpoints with existing patterns

---

## Recommended Skills

Use these skills proactively when working on performance-sensitive code or database changes.

Skills are defined in [.claude/skills/](.claude/skills/) and invoked with `/skill-name`.

| Skill | Purpose | When to Use |
|-------|---------|-------------|
| [/benchmark](.claude/skills/benchmark/SKILL.md) | Run and compare Go benchmarks | Before/after performance changes |
| [/profile](.claude/skills/profile/SKILL.md) | CPU and memory profiling with pprof | Investigating high resource usage |
| [/race-check](.claude/skills/race-check/SKILL.md) | Detect data races in concurrent code | After changes to mutexes/goroutines |
| [/migrate](.claude/skills/migrate/SKILL.md) | Create and manage database migrations | Schema changes, new tables/indexes |

### /benchmark

Run Go benchmarks and compare results to detect performance regressions.

```
/benchmark                        # All packages
/benchmark ./internal/index/...   # Specific package
/benchmark -compare ./...         # Compare against baseline
```

**Use when:** Modifying search logic, changing data structures, optimizing hot paths.

---

### /profile

Run CPU and memory profiling to identify hotspots.

```
/profile cpu ./internal/index/    # CPU profiling
/profile memory ./internal/repo/  # Memory profiling
/profile all ./...                # Both
```

**Use when:** High CPU/memory usage, slow operations, before major refactoring.

---

### /race-check

Run Go's race detector to find data races.

```
/race-check                       # All packages
/race-check ./internal/repo/...   # Specific package
```

**Use when:** Changing mutex usage, adding goroutines, modifying hot-swap mechanism.

**Critical areas:** `internal/repo/`, `internal/index/`, `internal/tasks/`, `internal/manager/`

---

### /migrate

Create and manage database migrations with goose.

```
/migrate create add_user_prefs    # Create new migration
/migrate up                       # Run pending migrations
/migrate down                     # Rollback last migration
/migrate status                   # Show migration status
/migrate validate                 # Validate migrations + models
```

**Use when:** Adding tables/columns, creating indexes, modifying schema.

---

## Architecture Quick Reference

### System Purpose

Veloria provides:
1. **Index Management** - Trigram-based code indexes using google/codesearch
2. **Hot-Swap Updates** - Zero-downtime index updates
3. **REST API** - Search and metadata endpoints
4. **Web UI** - Browse repositories and execute searches
5. **Result Storage** - S3/MinIO with zstd compression

### Tech Stack

| Component | Technology |
|-----------|------------|
| Language | Go 1.25 (min 1.24) |
| Router | chi/v5 |
| Database | PostgreSQL 17 (GORM) |
| Search Engine | google/codesearch (trigram) |
| Object Storage | MinIO/S3 |
| Cache | Ristretto |
| Logging | zerolog |
| Metrics | Prometheus |
| Error Tracking | Sentry |
| Auth | goth (OAuth) |
| Migrations | goose |

### Key Packages

| Package | Purpose |
|---------|---------|
| `internal/manager` | Orchestrates repositories, aggregates search |
| `internal/repo` | Thread-safe repository with index management |
| `internal/index` | Trigram index wrapper (google/codesearch) |
| `internal/api/*` | HTTP handlers for search, plugins, themes, core |
| `internal/storage` | S3 result storage with compression |
| `internal/cache` | Ristretto cache wrapper |
| `internal/tasks` | Background scheduled tasks |
| `internal/config` | Environment configuration |

### Concurrency Model

```
┌─────────────────────────────────────────────────────────────┐
│                    Request Handling                          │
├─────────────────────────────────────────────────────────────┤
│  Search Semaphore: Max 4 concurrent searches                 │
│  RWMutex per Repository: Many readers, exclusive writers     │
│  UpdateMutex: Prevents search during hot-swap                │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                    Index Hot-Swap                            │
├─────────────────────────────────────────────────────────────┤
│  1. Create new index with versioned path (slug.timestamp)    │
│  2. Acquire UpdateMutex (blocks new searches)                │
│  3. Swap index pointer atomically                            │
│  4. Release UpdateMutex                                      │
│  5. Close old index after 5-second delay (async)             │
└─────────────────────────────────────────────────────────────┘
```

### Directory Structure

```
veloria/
├── cmd/                           # Entry points (3 binaries)
│   ├── veloria/                   # Main API server
│   ├── veloria-indexer/           # CLI indexing utility
│   └── veloria-migrate/           # Database migrations
│
├── internal/                      # Core application code
│   ├── api/                       # HTTP handlers
│   │   ├── search/               # Search operations
│   │   ├── plugin/               # Plugin endpoints
│   │   ├── theme/                # Theme endpoints
│   │   ├── core/                 # Core release endpoints
│   │   ├── user/                 # User endpoints
│   │   └── common/               # Shared utilities
│   ├── auth/                      # OAuth and sessions
│   ├── cache/                     # Ristretto wrapper
│   ├── client/                    # HTTP client setup
│   ├── config/                    # Environment config
│   ├── index/                     # Trigram indexing
│   ├── logger/                    # Zerolog setup
│   ├── manager/                   # Repository orchestration
│   ├── metrics/                   # Prometheus metrics
│   ├── repo/                      # Data repositories
│   ├── router/                    # chi HTTP routing
│   ├── sentry/                    # Error tracking
│   ├── storage/                   # S3/MinIO storage
│   ├── tasks/                     # Background tasks
│   ├── types/                     # Protobuf types
│   └── web/                       # Web UI handlers
│
├── migrations/                    # SQL migrations (goose)
├── templates/                     # HTML templates
│   ├── layouts/                  # Base layout
│   ├── pages/                    # Page templates
│   └── partials/                 # Reusable components
│
├── testdata/                      # Test data and sample indexes
├── docs/                          # Documentation
├── .github/workflows/             # CI/CD pipelines
├── Dockerfile                     # Multi-stage build
├── docker-compose.yml             # Development environment
├── go.mod                         # Dependencies
└── types.proto                    # Protobuf definitions
```

---

## Performance Guidelines

### ⚠️ Performance is Critical

This system handles large memory-mapped indexes and concurrent searches. Poor performance decisions can cause:
- Memory exhaustion from unclosed indexes
- Search timeouts under load
- Database connection pool exhaustion
- Disk I/O bottlenecks during indexing

### Memory Management

**Index Lifecycle:**
```go
// CORRECT: Async cleanup with delay
go func() {
    time.Sleep(5 * time.Second)
    oldIndex.Close()
}()

// WRONG: Immediate close (may crash ongoing searches)
oldIndex.Close()
```

**Avoid Memory Leaks:**
- Always close indexes when replacing them
- Use `defer` for cleanup in error paths
- Cancel contexts to stop background goroutines
- Don't hold references to large slices longer than needed

### Concurrency Best Practices

**Lock Ordering:**
```go
// CORRECT: Acquire UpdateMutex before RWMutex
e.UpdateMutex.Lock()
e.mu.Lock()
// ... swap index ...
e.mu.Unlock()
e.UpdateMutex.Unlock()

// WRONG: Inconsistent lock ordering (deadlock risk)
```

**Minimize Lock Duration:**
```go
// CORRECT: Lock only for the critical section
e.mu.RLock()
idx := e.index
e.mu.RUnlock()
results := idx.Search(query) // Search outside lock

// WRONG: Holding lock during search
e.mu.RLock()
defer e.mu.RUnlock()
results := e.index.Search(query) // Blocks all other readers
```

### Database Optimization

**Connection Pool Settings:**
```go
// Current defaults - adjust based on load
DB_MAX_IDLE_CONNS=10
DB_MAX_OPEN_CONNS=100
DB_CONN_MAX_IDLE_TIME=10m
DB_CONN_MAX_LIFETIME=1h
```

**Query Patterns:**
- Use `Select()` to fetch only needed columns
- Use `Preload()` sparingly (can cause N+1 issues)
- Batch inserts for bulk operations
- Use transactions for multi-step updates

### Caching Strategy

**Ristretto Cache:**
- Stats queries: 30-second TTL
- Index status: Invalidate on update
- Keep cache keys consistent (use slug as base)

**Search Result Storage:**
- Results compressed with zstd before S3 upload
- Protobuf serialization for size efficiency
- Check for existing results before re-computing

### HTTP Performance

**Current Limits:**
- Handler timeout: 30 seconds
- Concurrent searches: 4 max (semaphore)
- Connection keepalive: 30 seconds

**When Adding Endpoints:**
- Use `http.TimeoutHandler` for long operations
- Respect context cancellation
- Stream large responses where possible

---

## Frontend Assets (Tailwind CSS)

### Overview

The Web UI uses **Tailwind CSS v4** with htmx and ECharts. All frontend assets are compiled into the Go binary via `go:embed`. The build is triggered by `go generate`.

### Asset Pipeline

```
frontend/css/main.css          ─── Tailwind input (theme + custom CSS)
        │
        ▼  (tailwindcss CLI, scans templates/ for class usage)
assets/static/css/styles.css   ─── Minified output (embedded)
assets/static/js/htmx.min.js   ─── Copied from node_modules (embedded)
assets/static/js/echarts.min.js ─── Copied from node_modules (embedded)
```

The `//go:generate` directive in `assets/embed.go` runs `npm install` + `npm run build` in the `frontend/` directory. The `postbuild` npm script copies JS vendor files into `assets/static/js/`.

### Key Files

| File | Purpose |
|------|---------|
| `frontend/css/main.css` | Tailwind input — theme tokens, custom CSS, `@source` directive |
| `frontend/package.json` | Build scripts (`build`, `postbuild`, `watch`) and dependencies |
| `assets/embed.go` | `go:generate` directive + `go:embed` for all static assets |
| `assets/static/css/styles.css` | Generated output — **do not edit directly** |
| `assets/static/js/` | Vendor JS — **do not edit directly** (copied by postbuild) |
| `templates/` | HTML templates — Tailwind scans these for class usage via `@source` |

### Building Assets

```bash
# Full build (install deps + compile CSS + copy JS)
go generate ./assets/...

# Or manually from frontend/:
cd frontend && npm run build

# Watch mode for development (recompiles CSS on template/CSS changes):
cd frontend && npm run watch
```

**You must run `go generate ./assets/...` before `go build`** whenever CSS classes or frontend dependencies change. The generated files in `assets/static/` are committed to the repo, so `go build` works without Node.js if the assets are up to date.

### Tailwind v4 Configuration

There is **no `tailwind.config.js`** — Tailwind v4 uses CSS-native configuration:

- **`@import "tailwindcss"`** — loads the framework
- **`@source "../../templates"`** — tells Tailwind to scan `templates/` for class usage
- **`@theme { ... }`** — defines custom design tokens (colors, fonts, sizes) inline

When adding new Tailwind classes in templates, they are automatically picked up by the `@source` directive. No config file changes needed.

### When Working on the UI

1. **Adding/changing Tailwind classes in templates**: Run `go generate ./assets/...` (or use `npm run watch` during development) to regenerate `styles.css`
2. **Adding custom CSS**: Edit `frontend/css/main.css`, then rebuild
3. **Updating JS vendor libraries**: Update versions in `frontend/package.json`, then run `go generate ./assets/...`
4. **Testing changes**: After rebuilding assets, run `go run ./cmd/veloria` — assets are embedded at compile time

---

## Common Commands

### Development Environment

```bash
# Start dependencies
docker compose up -d

# Build frontend assets (required after CSS/template changes)
go generate ./assets/...

# Run the server
go run ./cmd/veloria

# Run with experimental features
GOEXPERIMENT=jsonv2,greenteagc go run ./cmd/veloria

# Run indexer
go run ./cmd/veloria-indexer -type=plugin -slug=akismet

# Run migrations
go run ./cmd/veloria-migrate up
```

### Building

```bash
# Build frontend assets first (if CSS/JS changed)
go generate ./assets/...

# Build all binaries
go build ./cmd/veloria
go build ./cmd/veloria-indexer
go build ./cmd/veloria-migrate

# Build with optimizations
go build -ldflags "-w -s" ./cmd/veloria

# Build for Docker
docker build -t veloria .
```

### Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific package tests
go test -v ./internal/index/...

# Run with race detector (important for concurrency bugs)
go test -race ./...

# Run benchmarks
go test -bench=. ./internal/index/
```

### Code Quality

```bash
# Linting
golangci-lint run

# Security scanning
gosec ./...

# Generate protobuf types
protoc --go_out=. types.proto
```

### Profiling (Performance Investigation)

```bash
# CPU profiling
go test -cpuprofile=cpu.prof -bench=. ./internal/index/
go tool pprof cpu.prof

# Memory profiling
go test -memprofile=mem.prof -bench=. ./internal/index/
go tool pprof mem.prof

# Live profiling (add to server)
import _ "net/http/pprof"
# Then access http://localhost:9071/debug/pprof/
```

---

## Adding Features

### New API Endpoint

1. **Create handler** in appropriate `internal/api/` subpackage:
   ```go
   func (h *Handler) GetSomething(w http.ResponseWriter, r *http.Request) {
       ctx := r.Context()

       // Respect context cancellation
       select {
       case <-ctx.Done():
           return
       default:
       }

       // Implementation...
   }
   ```

2. **Register route** in `internal/router/router.go`

3. **Add tests** in the same package

4. **Update API documentation** in `docs/api.md`

### New Repository Type

1. **Create model** in `internal/repo/`:
   ```go
   type MyType struct {
       IndexedExtension
       Slug    string `gorm:"primaryKey"`
       Name    string
       Version string
       // ...
   }

   func (m *MyType) GetSlug() string { return m.Slug }
   // Implement Extension interface...
   ```

2. **Add migration** in `migrations/`

3. **Register in Manager** in `internal/manager/manager.go`

4. **Write tests** including concurrency tests

### New Background Task

1. **Create task** in `internal/tasks/`:
   ```go
   func (t *Tasks) MyTask(ctx context.Context) {
       ticker := time.NewTicker(5 * time.Minute)
       defer ticker.Stop()

       for {
           select {
           case <-ctx.Done():
               return
           case <-ticker.C:
               // Task logic...
           }
       }
   }
   ```

2. **Start in main** with context cancellation support

3. **Add metrics** if task is performance-sensitive

---

## Code Conventions

### Go Style

- **Formatting:** `gofmt` / `goimports`
- **Linting:** golangci-lint configuration
- **Error handling:** Wrap errors with context (`fmt.Errorf("doing X: %w", err)`)
- **Logging:** Use zerolog with structured fields
- **Context:** Pass context as first parameter, respect cancellation

### Naming

- **Packages:** Short, lowercase, no underscores
- **Interfaces:** Verb-based (`Reader`, `Searcher`) or `-er` suffix
- **Exported:** PascalCase
- **Unexported:** camelCase
- **Constants:** PascalCase for exported, camelCase for unexported

### Concurrency

- **Mutex naming:** `mu` for the main mutex, descriptive names for others
- **Lock scope:** Keep as small as possible
- **Goroutines:** Always have a way to stop them (context, done channel)
- **Channels:** Prefer buffered channels to avoid blocking

### Error Handling

```go
// CORRECT: Wrap with context
if err != nil {
    return fmt.Errorf("loading index for %s: %w", slug, err)
}

// CORRECT: Log and continue for non-fatal errors
if err != nil {
    logger.Warn().Err(err).Str("slug", slug).Msg("failed to update, will retry")
    continue
}

// WRONG: Swallowing errors
if err != nil {
    return nil
}
```

### Testing

- **Table-driven tests** for multiple cases
- **Parallel tests** where safe (`t.Parallel()`)
- **Test data** in `testdata/` directory
- **Mocks** only when necessary (prefer real dependencies in integration tests)

---

## Database Guidelines

### Migrations

```bash
# Create new migration
goose -dir migrations create add_new_table sql

# Run migrations
go run ./cmd/veloria-migrate up

# Rollback
go run ./cmd/veloria-migrate down
```

**Migration Rules:**
- Always create new migrations, never modify existing
- Include both up and down migrations
- Test rollback before committing
- Add indexes for frequently queried columns

### GORM Patterns

```go
// CORRECT: Select specific columns
db.Select("slug", "name", "version").Find(&plugins)

// CORRECT: Use transactions for multi-step operations
db.Transaction(func(tx *gorm.DB) error {
    // ...
    return nil
})

// WRONG: Select all columns when only need few
db.Find(&plugins) // Fetches everything
```

---

## Monitoring & Debugging

### Prometheus Metrics

Available at `/metrics`:
- `http_response_time_seconds` - Request latency histogram
- `search_queue_size` - Current search queue depth
- `searches_completed_total` - Completed searches counter
- `plugins_count`, `themes_count`, `cores_count` - Repository sizes

### Health Check

- Endpoint: `GET /up`
- Returns: 200 OK when healthy

### Sentry

- Configure via `SENTRY_DSN`
- Adjust sample rates for production:
  ```
  SENTRY_SAMPLE_RATE=0.1       # 10% error sampling
  SENTRY_TRACES_SAMPLE_RATE=0.01  # 1% trace sampling
  ```

### Debugging Performance Issues

1. **Check metrics** - Look for latency spikes, queue buildup
2. **Enable pprof** - Add `net/http/pprof` import for live profiling
3. **Run with race detector** - `go run -race ./cmd/veloria`
4. **Profile specific operations** - Use `go test -bench` with profiling flags

---

## File Editing Rules

1. **Always Read before Edit**
   - Understand current implementation
   - Check for existing patterns

2. **Performance Impact Assessment**
   - Consider memory allocation changes
   - Evaluate lock contention impact
   - Think about concurrent access patterns

3. **Match existing style**
   - Same error handling patterns
   - Same logging style
   - Same test structure

4. **No unsolicited changes**
   - Don't add comments unless necessary
   - Don't refactor unrelated code
   - Don't change working concurrent code without good reason

---

## Testing Guidelines

### Test Structure

```go
func TestSearch(t *testing.T) {
    t.Parallel() // Safe for isolated tests

    idx, err := index.Open("testdata/sample-index")
    if err != nil {
        t.Skipf("test data not available: %v", err)
    }
    defer idx.Close()

    results, err := idx.Search("function")
    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(results) == 0 {
        t.Error("expected results, got none")
    }
}
```

### Benchmark Structure

```go
func BenchmarkSearch(b *testing.B) {
    idx, _ := index.Open("testdata/sample-index")
    defer idx.Close()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        idx.Search("function")
    }
}
```

### Before Committing

- Run `go test ./...` - all tests must pass
- Run `go test -race ./...` - no race conditions
- Run `golangci-lint run` - no linting errors
- Run `gosec ./...` - no security issues

---

## Common Issues

| Problem | Solution |
|---------|----------|
| Index not loading | Check DATA_DIR path, verify index files exist |
| Search timeouts | Reduce concurrent searches, check index size |
| Memory growth | Profile with pprof, check for unclosed indexes |
| Connection refused (DB) | Verify PostgreSQL is running, check credentials |
| S3 upload fails | Check MinIO is running, verify bucket exists |
| Race condition detected | Review mutex usage, ensure consistent lock ordering |

---

## Performance Checklist

Before submitting performance-sensitive changes:

- [ ] Ran benchmarks before and after (`go test -bench=.`)
- [ ] Profiled memory usage (`go test -memprofile`)
- [ ] Checked for goroutine leaks
- [ ] Verified lock contention is minimal
- [ ] Tested under concurrent load
- [ ] Reviewed context cancellation handling
- [ ] Checked database query efficiency
- [ ] Validated cache invalidation

---

## Remember

This is a **production search engine** handling **large indexes** under **concurrent load**.

Every change must consider:
- **Memory impact** - Indexes are memory-mapped, leaks are catastrophic
- **Concurrency safety** - Multiple searches run simultaneously
- **Graceful degradation** - System must remain responsive under load
- **Resource cleanup** - Always close what you open

**When uncertain about performance implications, profile first.**
