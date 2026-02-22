# Architecture

This document covers the high-level design of Veloria, a code search engine for WordPress extensions (plugins, themes, and cores).

## Binaries

| Binary | Entry Point | Purpose |
|---|---|---|
| `veloria` | `cmd/veloria/` | Main server: HTTP API, web UI, background indexer |
| `veloria-indexer` | `cmd/veloria-indexer/` | CLI tool that downloads a ZIP, extracts source, and builds a search index. Invoked as a subprocess by the server. |
| `veloria-migrate` | `cmd/veloria-migrate/` | Runs database migrations via [goose](https://github.com/pressly/goose) |
| `veloria-converter` | `cmd/veloria-converter/` | One-time utility for migrating data from WPDirectory's BoltDB format |

## Package Layout

```
internal/
├── app/          # Application lifecycle (New, Start, Shutdown)
├── api/          # Shared JSON response helpers (WriteJSON, APIError, pagination)
├── admin/        # Admin web handlers (reindex)
├── auth/         # OAuth providers, session management
├── cache/        # Cache interface + Ristretto implementation
├── client/       # Shared HTTP client with User-Agent
├── codesearch/   # Low-level regexp search (port of Google codesearch)
├── config/       # Environment-based configuration
├── core/         # Core (WordPress versions) web + API handlers
├── index/        # Search index: build, read, search operations
├── logger/       # Zerolog setup + access logging middleware
├── manager/      # Orchestrates all extension stores; owns the updater loop
├── metrics/      # Prometheus metric collectors
├── plugin/       # Plugin web + API handlers
├── repo/         # Extension stores, data models, API client, SVN discovery
├── report/       # Search report (flagging) handlers
├── router/       # Chi router setup and middleware
├── search/       # Search web + API handlers, search model
├── sentry/       # Sentry error tracking setup
├── server/       # HTTP server with TLS (certmagic) support
├── storage/      # S3/MinIO client for search result storage
├── tasks/        # Scheduled background tasks (e.g. cleanup)
├── testutil/     # Test fixtures, hand-written fakes
├── theme/        # Theme web + API handlers
├── types/        # Protobuf-generated types for search results
├── user/         # User model
├── web/          # Shared web deps, templates, interfaces
```

## Core Concepts

### Extension Types

Veloria manages three types of WordPress extensions, identified by `repo.ExtensionType`:

- `TypePlugins` ("plugins")
- `TypeThemes` ("themes")
- `TypeCores` ("cores")

Each type has a concrete store (`PluginStore`, `ThemeStore`, `CoreStore`) that embeds the generic `ExtensionStore[T]`.

### Interface Hierarchy

The type system uses layered interfaces to keep boundaries clean:

```
Extension           (7 methods)  -- data contract for handlers and templates
    ^
    |
Indexable           (9 methods)  -- adds index wiring (GetIndexedExtension, SetIndexedExtension)
    ^
    |
ExtensionStore[T]   (generic)   -- in-memory store with search, load, index management
    ^
    |
DataSource          (10 methods) -- what the Manager needs from each store
```

**`Extension`** (`internal/repo/extension.go`) is the narrow read-only interface used everywhere outside the indexing subsystem:

```go
type Extension interface {
    GetSlug() string
    GetSource() string
    GetName() string
    GetVersion() string
    GetDownloadLink() string
    GetActiveInstalls() int
    GetDownloaded() int
}
```

**`Indexable`** extends `Extension` with index lifecycle methods. It is the generic constraint on `ExtensionStore[T Indexable]`.

**`DataSource`** (`internal/repo/datasource.go`) abstracts what the Manager needs from each store:

```go
type DataSource interface {
    Type() ExtensionType
    Load() error
    Stats() (total int, indexed int)
    IndexStatus() map[string]bool
    Search(term string, opt *index.SearchOptions, progressFn func(searched, total int)) ([]*SearchResult, error)
    PrepareUpdates() []IndexTask
    ResumeUnindexed() []IndexTask
    GetExtension(slug string) (Extension, bool)
    MakeReindexTaskBySlug(slug string) (IndexTask, bool)
    ResolveSourceDir(slug string) (string, error)
}
```

### Manager

The `Manager` (`internal/manager/manager.go`) is the central coordinator:

- Holds a `map[ExtensionType]DataSource` — one entry per extension type
- Runs a single background updater loop that collects `IndexTask` items from all sources and executes them through a shared semaphore (controlled by `INDEXER_CONCURRENCY`)
- Accepts ad-hoc reindex requests via a buffered channel (`adhocCh`)
- Exposes `Search()`, `GetExtension()`, `ResolveSourceDir()`, `Stats()`, `IndexStatus()`, `SubmitReindex()` — all dispatching to the correct `DataSource` by type string

The Manager satisfies several interfaces consumed by different subsystems:

| Interface | Package | Methods |
|---|---|---|
| `Searcher` | `manager` | `Search(repoType, term, params)` |
| `RepoStatsProvider` | `manager` | `Stats()`, `IndexStatus()` (per-type, on DataSource directly) |
| `SearchService` | `web` | `Search(repoType, term, params)` |
| `ReindexService` | `web` | `SubmitReindex(repoType, slug)` |
| `SourceResolver` | `web` | `ResolveSourceDir(repoType, slug)` |
| `StatsProvider` | `web` | `Stats(repoType)`, `IndexStatus(repoType)` |

### Web Dependencies

The `web.Deps` struct (`internal/web/deps.go`) is the shared dependency container for all web handlers. Instead of holding a concrete `*Manager`, it holds four narrow interfaces:

```go
type Deps struct {
    Templates  *Templates
    DB         *gorm.DB
    Search     SearchService      // Search(repoType, term, params)
    Reindex    ReindexService     // SubmitReindex(repoType, slug)
    Sources    SourceResolver     // ResolveSourceDir(repoType, slug)
    Stats      StatsProvider      // Stats(repoType), IndexStatus(repoType)
    S3         storage.ResultStorage
    Cache      cache.Cache
    Config     *config.Config
    Progress   *ProgressStore
    // ...
}
```

Any of these may be nil when the corresponding subsystem is unavailable (e.g., no database connection). Handlers check for nil before use.

### Router

The router (`internal/router/router.go`) uses a `RouterDeps` struct to receive all dependencies:

```go
type RouterDeps struct {
    Logger  *zerolog.Logger
    DB      *gorm.DB
    Search  manager.Searcher
    Stats   map[string]manager.RepoStatsProvider
    S3      storage.ResultStorage
    WebDeps *web.Deps
    Session *auth.SessionStore
    Auth    *auth.Handler
    Options Options
}
```

### API Client

The `APIClient` (`internal/repo/api_client.go`) wraps the HTTP client, API key, and circuit breaker for AspireCloud API calls. It is injected into store constructors via `StoreConfig`, eliminating package-level globals:

```go
apiClient := repo.NewAPIClient(c.AspireCloudAPIKey)
pr := repo.NewPluginStore(ctx, db, cfg, logger, cache, apiClient)
```

The circuit breaker (sony/gobreaker) trips after 5 consecutive failures and recovers after 30 seconds.

## Data Flow

### Startup

1. `app.New()` loads config, connects to PostgreSQL and S3/MinIO
2. Creates an `APIClient` with the AspireCloud API key
3. Creates three stores: `PluginStore`, `ThemeStore`, `CoreStore`
4. `manager.NewManager()` loads all stores concurrently from DB + disk indexes, then starts the background updater
5. Builds the router with all dependencies and starts the HTTP server

### Indexing Loop

The updater loop in `Manager.startUpdater()`:

1. On first run, resumes any extensions saved to DB but not yet indexed
2. Calls `PrepareUpdates()` on each DataSource to discover new/changed extensions
3. Runs all returned `IndexTask` items through a shared semaphore
4. Drains any ad-hoc reindex requests from `adhocCh`
5. Waits 5 minutes (`UpdateInterval`) or until an ad-hoc request arrives

Each `IndexTask.Run()` closure handles the full pipeline: download ZIP, extract source, invoke `veloria-indexer` subprocess to build the index, and swap the new index into the store.

### Search

1. HTTP request arrives at search handler
2. Handler calls `SearchService.Search(repoType, term, params)`
3. Manager dispatches to the correct DataSource
4. `ExtensionStore.Search()` fans out across all indexed extensions using `SEARCH_CONCURRENCY` workers
5. Each worker calls `IndexedExtension.SearchCompiled()` on the extension's in-memory index
6. Results are collected, sorted by active installs (descending), and returned

### Discovery

Veloria discovers extensions through two mechanisms:

- **SVN scanning** (`internal/repo/svn.go`): Periodically lists `plugins.svn.wordpress.org` and `themes.svn.wordpress.org` to find new slugs
- **Incremental API updates**: Fetches recently-updated extensions from the AspireCloud API (`FetchPluginsUpdatedWithinLastHour`, etc.)

New slugs are fetched individually via `FetchPluginInfo`/`FetchThemeInfo` and saved to the database. A full SVN scan runs every 7 days.

## Graceful Degradation

Veloria is designed to run in degraded mode when dependencies are unavailable:

- **No database**: Server starts without search, repos, or auth. Health endpoint still works.
- **No S3**: Server starts without search result storage. Search is disabled.
- **No manager**: Web UI shows "search disabled" messaging. API list endpoints still work (direct DB queries).
- **No auth config**: Server starts without OAuth/session support. Admin routes are hidden.
