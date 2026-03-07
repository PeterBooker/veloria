# Veloria

A code search engine for the WordPress ecosystem. Veloria downloads, indexes, and enables full-text search across WordPress plugins, themes, and core releases from WordPress.org.

## Features

- **Trigram-based code search** (forked from google/codesearch)
- **Three repository types**: Plugins, Themes, and Core releases
- **Dual-source architecture**: WordPress.org and AspireCloud/FAIR API
- **Automatic updates**: Polls APIs for recently updated extensions
- **Hot-swap indexing**: Atomic index swaps without downtime
- **Search moderation**: Automatically flags searches containing URLs or offensive content
- **Search persistence**: Results stored in S3 with sharing, CSV export, and OG image previews
- **OAuth authentication**: GitHub, GitLab, and Atlassian login with role-based access
- **MCP server**: Model Context Protocol endpoint for AI-assisted code exploration
- **RESTful JSON API**: Search, metadata, and management endpoints with rate limiting
- **Maintenance mode**: Toggle via CLI with health check bypass
- **Full observability**: OpenTelemetry traces, metrics, and logs via Grafana stack

## Quick Start

### Prerequisites

- Go 1.26.0
- Node.js 22+ and npm (for building frontend assets)
- PostgreSQL 17+
- Docker (optional)

### Installation

```bash
# Clone the repository
git clone https://github.com/PeterBooker/veloria.git
cd veloria

# Build frontend assets (Tailwind CSS, htmx, ECharts)
go generate ./assets/...

# Build the binary
go build -o veloria ./cmd/veloria
```

### Configuration

```bash
cp .env.example .env
```

See [Configuration Reference](docs/configuration.md) for all options.

### Running

```bash
# Start dev stack (PostgreSQL, MinIO, Mailpit, Grafana observability)
docker compose up -d

# Run database migrations
./veloria migrate up

# Run the server (default command)
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
| [Testing](docs/testing.md) | Test infrastructure, fakes, and patterns |
| [Contributing](.github/CONTRIBUTING.md) | Contribution workflow and PR requirements |
| [Security Policy](.github/SECURITY.md) | Vulnerability reporting and response policy |
| [Code of Conduct](.github/CODE_OF_CONDUCT.md) | Community behavior standards |

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

- **Language**: Go 1.26.0
- **Database**: PostgreSQL 17+ (GORM, goose migrations)
- **Search**: Trigram indexing (forked from google/codesearch)
- **Storage**: S3/MinIO (search results, protobuf serialization)
- **HTTP**: chi router, httprate rate limiting
- **Auth**: OAuth2 via goth (GitHub, GitLab, Atlassian), gorilla/sessions
- **Templating**: templ (server-side), htmx + ECharts (client-side)
- **Frontend**: Tailwind CSS v4, lightningcss
- **CLI**: Kong (subcommands: serve, index, migrate, wipe, maintenance, user, reindex, stats, version)
- **Config**: caarlos0/env with go-playground/validator
- **Resilience**: sony/gobreaker circuit breaker, token bucket rate limiting
- **Observability**: OpenTelemetry → Grafana Alloy → Tempo (traces), Loki (logs), Prometheus (metrics), Grafana (dashboards)
- **Logging**: zap
- **Error Tracking**: Sentry

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](.github/CONTRIBUTING.md) before opening a pull request.

## Sponsorship

If Veloria is useful to your team, consider sponsoring maintenance and roadmap work:

- GitHub Sponsors: https://github.com/sponsors/PeterBooker

## License

MIT
