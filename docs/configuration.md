# Configuration

Veloria is configured via environment variables. You can set these directly or use a `.env` file in the working directory.

## Environment Variables

### Application

| Variable | Default | Description |
|----------|---------|-------------|
| `NAME` | `Veloria Core` | Application name used in logs |
| `PORT` | `9071` | HTTP server port |
| `ENV` | `development` | Environment: `development`, `staging`, or `production` |
| `VERSION` | `1.0.0` | Application version |
| `DATA_DIR` | `/etc/veloria/data` | Base directory for source files and indexes |

### HTTP Server

| Variable | Default | Description |
|----------|---------|-------------|
| `HTTP_TIMEOUT` | `2500` | HTTP client timeout in milliseconds |
| `HTTP_RATE_LIMIT_ENABLED` | `true` | Enable request rate limiting |
| `HTTP_LOGGING_ENABLED` | `true` | Enable request logging |

### Database (PostgreSQL)

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_DATABASE` | `fundy` | Database name |
| `DB_USERNAME` | `root` | Database username |
| `DB_PASSWORD` | (empty) | Database password |

### Sentry (Error Tracking)

| Variable | Default | Description |
|----------|---------|-------------|
| `SENTRY_DSN` | (empty) | Sentry DSN. Leave empty to disable |
| `SENTRY_SAMPLE_RATE` | `0.0` | Error sample rate (0.0 - 1.0) |
| `SENTRY_TRACES_SAMPLE_RATE` | `0.0` | Performance tracing sample rate (0.0 - 1.0) |

## Data Directory Structure

The `DATA_DIR` contains all source code and search indexes:

```
$DATA_DIR/
├── plugins/
│   ├── source/           # Extracted plugin source code
│   │   ├── woocommerce/
│   │   ├── jetpack/
│   │   └── ...
│   └── index/            # Trigram search indexes
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

## Environment-Specific Behavior

### Development

- Fetches extensions from local source directories
- Use `FetchLocalPlugins()` and `FetchLocalThemes()` for testing
- Intended for local development with pre-downloaded test data

### Staging / Production

- Fetches updates from WordPress.org APIs
- Polls for recently updated extensions every 5 minutes
- Automatically downloads and indexes new versions

## Example .env File

```bash
# Application
ENV=production
PORT=9071
DATA_DIR=/var/lib/veloria/data

# Database
DB_HOST=db.example.com
DB_PORT=5432
DB_DATABASE=veloria
DB_USERNAME=veloria_user
DB_PASSWORD=secure_password_here

# Sentry (optional)
SENTRY_DSN=https://key@sentry.io/project
SENTRY_SAMPLE_RATE=0.1
SENTRY_TRACES_SAMPLE_RATE=0.05
```

## Indexer Configuration

The `veloria-indexer` binary uses command-line flags:

| Flag | Description |
|------|-------------|
| `-repo` | Repository type: `plugins`, `themes`, or `cores` |
| `-slug` | Extension slug (or version for cores) |
| `-zipurl` | URL to download the extension ZIP |

Example:

```bash
veloria-indexer -repo=plugins -slug=woocommerce -zipurl=https://downloads.wordpress.org/plugin/woocommerce.8.5.2.zip
```

The indexer outputs `INDEX_READY:<path>` to stdout when complete, which the main service uses to hot-swap indexes.
