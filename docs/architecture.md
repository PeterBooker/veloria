# Architecture

Veloria is a code search engine for the WordPress ecosystem. It downloads, indexes, and enables full-text search across WordPress plugins, themes, and core releases.

## System Overview

```
┌─────────────────────────────────────────────────────────────┐
│                      HTTP Clients                           │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                    chi HTTP Router                          │
│              (Middleware + API Routes)                      │
└─────────────────────────┬───────────────────────────────────┘
                          │
          ┌───────────────┴───────────────┐
          ▼                               ▼
┌──────────────────┐              ┌───────────────┐
│   API Handlers   │              │    Manager    │
│  (search, etc)   │              │ (orchestrator)│
└──────────────────┘              └───────┬───────┘
                                          │
        ┌──────────────┬──────────────────┼──────────────────┐
        ▼              ▼                  ▼                  ▼
┌────────────┐  ┌────────────┐    ┌────────────┐     ┌──────────┐
│ PluginRepo │  │ ThemeRepo  │    │  CoreRepo  │     │ Extension│
│ Repository │  │ Repository │    │ Repository │     │  Types   │
└──────┬─────┘  └──────┬─────┘    └──────┬─────┘     └──────────┘
       │               │                 │
       └───────────────┴─────────────────┘
                       │
    ┌──────────────────┴──────────────────┐
    ▼                                     ▼
┌──────────────────┐          ┌──────────────────────┐
│   PostgreSQL     │          │   Trigram Index      │
│   (Metadata)     │          │ (google/codesearch)  │
└──────────────────┘          └──────────────────────┘
                                      │
                           ┌──────────┴────────────┐
                           ▼                       ▼
                   ┌────────────────┐      ┌──────────────┐
                   │ source/        │      │ index/       │
                   │ <slug>/        │      │ <slug>.<ts>/ │
                   │ (extracted     │      │ (trigrams)   │
                   │  source code)  │      └──────────────┘
                   └────────────────┘
```

## Core Components

### Manager

The Manager (`internal/manager/`) orchestrates all repository operations:

- Initializes Plugin, Theme, and Core repositories
- Loads data from the database and indexes from disk
- Starts background update workers
- Provides a unified search interface across all repositories

### Generic Repository

The Repository (`internal/repo/repository.go`) is a generic type that provides:

- Thread-safe extension storage with RWMutex
- Database loading via callback functions
- Index loading from versioned directories
- Background update workers with graceful shutdown
- Search aggregation across all extensions

### Extension Types

Three extension types implement the `Extension` interface:

| Type | Key Field | Source |
|------|-----------|--------|
| Plugin | `slug` | WordPress.org Plugins API |
| Theme | `slug` | WordPress.org Themes API |
| Core | `version` | WordPress.org Releases page |

Each extension type embeds `*IndexedExtension` which provides:
- Thread-safe index management
- Search functionality
- Hot-swap index updates

### Trigram Indexing

The search system uses [google/codesearch](https://github.com/google/codesearch) for trigram-based indexing:

1. **Source extraction**: ZIP files are downloaded and text files extracted to `source/<slug>/`
2. **Index creation**: Trigram index created at `index/<slug>.<timestamp>/`
3. **Search**: Query patterns are converted to trigram queries for candidate file selection
4. **Verification**: Candidate files are grepped with standard regex for actual matches

Index versioning (`<slug>.<timestamp>`) prevents mmap conflicts during hot updates.

## Data Flow

### Search Request Flow

```
POST /api/v1/search
    │
    ▼
search.CreateSearchV1()
    │
    ▼
manager.Search(repo, term, fileMatch, caseInsensitive)
    │
    ▼
Repository.Search(term, options)
    │
    ▼
For each extension with index:
    extension.Search(term, options)
        │
        ▼
    index.Search(term, options)
        │
        ├─► Query trigram index for candidate files
        │
        └─► Grep each file with regex
    │
    ▼
Aggregate results, sort by popularity
    │
    ▼
Return SearchResponse JSON
```

### Update/Indexing Flow

```
Background ticker (every 5 minutes)
    │
    ▼
Repository.processUpdates()
    │
    ▼
Fetch updates from WordPress.org API
    │
    ▼
Save/update metadata in PostgreSQL
    │
    ▼
For each extension:
    veloria-indexer -repo=<type> -slug=<slug> -zipurl=<url>
        │
        ├─► Download ZIP
        ├─► Extract text files to source/<slug>/
        ├─► Create index at index/<slug>.<timestamp>/
        └─► Output INDEX_READY:<path>
    │
    ▼
Open new index, swap with old
    │
    ▼
Clean up old index directory (async)
```

## Directory Structure

```
/etc/veloria/data/           # Default DATA_DIR
├── plugins/
│   ├── source/            # Extracted plugin source code
│   │   ├── woocommerce/
│   │   ├── jetpack/
│   │   └── ...
│   └── index/             # Trigram indexes
│       ├── woocommerce.1234567890/
│       ├── jetpack.1234567890/
│       └── ...
├── themes/
│   ├── source/
│   └── index/
└── cores/
    ├── source/
    │   ├── 6.8.1/
    │   └── ...
    └── index/
```

## Concurrency Model

- **Repository RWMutex**: Protects the extension map for concurrent read access during search
- **Extension RWMutex**: Protects individual extension indexes during hot-swap updates
- **Context cancellation**: Background workers respond to context cancellation for graceful shutdown
- **Index versioning**: Prevents file conflicts when updating indexes while searches are in progress

## External Dependencies

| Dependency | Purpose |
|------------|---------|
| PostgreSQL | Metadata storage for plugins, themes, cores, users, searches |
| google/codesearch | Trigram indexing and query execution |
| WordPress.org APIs | Plugin/theme metadata and ZIP downloads |
| Sentry | Error tracking and performance monitoring |
| Prometheus | Metrics collection |
