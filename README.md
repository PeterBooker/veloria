# Veloria

A code search engine for the WordPress ecosystem. Veloria downloads, indexes, and enables full-text search across WordPress plugins, themes, and core releases from WordPress.org.

## Features

- **Trigram-based code search** using [google/codesearch](https://github.com/google/codesearch)
- **Three repository types**: Plugins, Themes, and Core releases
- **Automatic updates**: Polls WordPress.org APIs for recently updated extensions
- **Hot-swap indexing**: Update indexes without downtime
- **RESTful JSON API**: Simple interface for search and metadata queries

## Quick Start

### Prerequisites

- Go 1.24+
- Node.js 18+ and npm (for building frontend assets)
- PostgreSQL 14+
- Docker (optional)

### Installation

```bash
# Clone the repository
git clone https://github.com/your-org/veloria.git
cd veloria

# Build frontend assets (Tailwind CSS, htmx, ECharts)
go generate ./assets/...

# Build the binaries
go build -o veloria ./cmd/veloria
go build -o veloria-indexer ./cmd/veloria-indexer
```

### Configuration

Create a `.env` file (or set environment variables):

```bash
ENV=development
PORT=9071
DATA_DIR=/etc/veloria/data
DB_HOST=localhost
DB_PORT=5432
DB_DATABASE=veloria
DB_USERNAME=postgres
DB_PASSWORD=
```

See [Configuration Reference](docs/configuration.md) for all options.

### Running

```bash
# Start PostgreSQL (using Docker)
docker compose up -d

# Run the server
./veloria
```

The API will be available at `http://localhost:9071`.

## Usage

The core component exposes a REST API, which is used by the frontend.

## Documentation

| Document | Description |
|----------|-------------|
| [Architecture](docs/architecture.md) | System design, data flow, and component overview |
| [API Reference](docs/api.md) | Complete API endpoint documentation |
| [Configuration](docs/configuration.md) | Environment variables and settings |
| [Development Guide](docs/development.md) | Setup, building, testing, and debugging |

## Project Structure

```
veloria/
├── cmd/
│   ├── veloria/            # Main API server
│   └── veloria-indexer/    # Indexing utility
├── internal/
│   ├── api/               # HTTP handlers
│   ├── config/            # Configuration
│   ├── index/             # Trigram indexing
│   ├── manager/           # Repository orchestration
│   ├── repo/              # Data repositories (generic)
│   └── router/            # HTTP routing
├── assets/                # Embedded static assets (go:embed)
│   └── static/            # CSS, JS, fonts (generated — do not edit)
├── frontend/              # Frontend build (Tailwind v4, npm)
│   └── css/main.css       # Tailwind input + theme tokens
├── templates/             # HTML templates (Go html/template)
└── docs/                  # Documentation
```

## How It Works

1. **Fetch metadata** from WordPress.org APIs (plugins, themes, core releases)
2. **Download ZIP files** and extract source code to `DATA_DIR/<type>/source/<slug>/`
3. **Build trigram indexes** at `DATA_DIR/<type>/index/<slug>.<timestamp>/`
4. **Search queries** use the trigram index to find candidate files, then grep for actual matches
5. **Results** include file paths, line numbers, and matching content

See [Architecture](docs/architecture.md) for detailed diagrams and explanations.

## Development

```bash
# Build frontend assets (after CSS or template changes)
go generate ./assets/...

# Watch mode (auto-rebuilds CSS on changes)
cd frontend && npm run watch

# Run tests
go test ./...

# Format code
go fmt ./...

# Check for issues
go vet ./...
```

### Protobuf

Regenerate the search results protobuf types (run from the repo root):

```bash
protoc --go_out=internal/types --go_opt=paths=source_relative types.proto
```

See [Development Guide](docs/development.md) for complete instructions.

## Tech Stack

- **Language**: Go 1.24+
- **Database**: PostgreSQL (metadata storage)
- **Search**: google/codesearch (trigram indexing)
- **HTTP Router**: chi
- **ORM**: GORM
- **Monitoring**: Prometheus metrics, Sentry error tracking

## License

MIT
