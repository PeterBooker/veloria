# Configuration

All configuration is via environment variables. In development mode (`ENV=development`), a `.env` file in the project root is loaded automatically.

Configuration is parsed using [caarlos0/env/v10](https://github.com/caarlos0/env) struct tags and validated with [go-playground/validator/v10](https://github.com/go-playground/validator).

## Application

| Variable | Default | Description |
|---|---|---|
| `NAME` | `Veloria Core` | Application display name |
| `PORT` | `9071` | HTTP server port |
| `ENV` | `development` | Environment (`development` or `production`) |
| `DATA_DIR` | `/etc/veloria/data` | Root directory for source files and indexes |
| `APP_DEBUG` | `false` | Enable debug-level logging |
| `APP_URL` | (empty) | Public URL for the application (enables TLS in production) |
| `REDIRECT_DOMAINS` | (empty) | Comma-separated legacy domains to redirect to `APP_URL` |

## HTTP Server

| Variable | Default | Description |
|---|---|---|
| `HTTP_TIMEOUT` | `2500` | HTTP client timeout in milliseconds |
| `HTTP_HANDLER_TIMEOUT` | `30s` | Max time for a handler to complete |
| `HTTP_READ_TIMEOUT` | `30s` | Max time to read request |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Max time to read request headers |
| `HTTP_WRITE_TIMEOUT` | `30s` | Max time to write response |
| `HTTP_IDLE_TIMEOUT` | `60s` | Max idle time for keep-alive connections |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` | Max time for graceful shutdown |
| `HTTP_RATE_LIMIT_ENABLED` | `true` | Enable rate limiting on API routes |
| `HTTP_LOGGING_ENABLED` | `true` | Enable access logging |

## Database (PostgreSQL)

| Variable | Default | Description |
|---|---|---|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_DATABASE` | `fundy` | Database name |
| `DB_USERNAME` | `root` | Database user |
| `DB_PASSWORD` | (empty) | Database password |
| `DB_SSLMODE` | (auto) | SSL mode. In development defaults to `disable`, otherwise `require`. Accepts: `disable`, `require`, `verify-full`, `verify-ca`, `prefer`, `allow` |
| `DB_TIMEZONE` | `UTC` | Database timezone |
| `DB_CONNECT_TIMEOUT` | `5` | Connection timeout in seconds |
| `DB_PING_TIMEOUT` | `3s` | Ping timeout after connection |
| `DB_MAX_IDLE_CONNS` | `10` | Max idle connections in pool |
| `DB_MAX_OPEN_CONNS` | `100` | Max open connections in pool |
| `DB_CONN_MAX_IDLE_TIME` | `10m` | Max idle time before closing a connection |
| `DB_CONN_MAX_LIFETIME` | `1h` | Max lifetime of a connection |

## S3 / MinIO (Search Result Storage)

| Variable | Default | Description |
|---|---|---|
| `S3_ENDPOINT` | `localhost:9000` | S3-compatible endpoint |
| `S3_BUCKET` | `veloria-searches` | Bucket for search results |
| `S3_ACCESS_KEY` | `minioadmin` | Access key |
| `S3_SECRET_KEY` | `minioadmin` | Secret key |
| `S3_USE_SSL` | `false` | Use HTTPS for S3 connections |
| `S3_REGION` | `us-east-1` | S3 region |
| `S3_ENSURE_BUCKET` | `false` | Auto-create bucket on startup (auto-enabled in development) |
| `S3_INIT_TIMEOUT` | `5s` | Timeout for S3 initialization |

## OAuth / Authentication

| Variable | Default | Description |
|---|---|---|
| `OAUTH_BASE_URL` | (empty) | Base URL for OAuth callbacks |
| `GITHUB_CLIENT_ID` | (empty) | GitHub OAuth client ID |
| `GITHUB_CLIENT_SECRET` | (empty) | GitHub OAuth client secret |
| `GITLAB_CLIENT_ID` | (empty) | GitLab OAuth client ID |
| `GITLAB_CLIENT_SECRET` | (empty) | GitLab OAuth client secret |
| `ATLASSIAN_CLIENT_ID` | (empty) | Atlassian OAuth client ID |
| `ATLASSIAN_CLIENT_SECRET` | (empty) | Atlassian OAuth client secret |
| `SESSION_SECRET` | (empty) | Secret for session cookie encryption. Required in production. |

## Indexer

| Variable | Default | Description |
|---|---|---|
| `INDEXER_CONCURRENCY` | `1` | Number of parallel indexing goroutines |
| `SEARCH_CONCURRENCY` | `24` | Max concurrent search fan-out workers |

## AspireCloud API

| Variable | Default | Description |
|---|---|---|
| `ASPIRE_CLOUD_API_KEY` | (empty) | Bearer token for AspireCloud/WordPress API calls |

## Sentry (Error Tracking)

| Variable | Default | Description |
|---|---|---|
| `SENTRY_DSN` | (empty) | Sentry DSN (disabled when empty) |
| `SENTRY_SAMPLE_RATE` | `0.0` | Error sample rate (0.0 to 1.0) |
| `SENTRY_TRACES_SAMPLE_RATE` | `0.0` | Performance trace sample rate |

## TLS (Production)

| Variable | Default | Description |
|---|---|---|
| `ACME_EMAIL` | (empty) | Email for ACME/Let's Encrypt registration |
| `CLOUDFLARE_API_TOKEN` | (empty) | Cloudflare API token for DNS-01 challenges. Required when `APP_URL` is set in production. |

## Data Directory Structure

When `EnsureDirs()` runs at startup, it creates:

```
$DATA_DIR/
├── plugins/
│   ├── source/    # Extracted plugin source files
│   └── index/     # Search indexes (one directory per slug)
├── themes/
│   ├── source/
│   └── index/
├── cores/
│   ├── source/
│   └── index/
└── certs/         # TLS certificates (production)
```
