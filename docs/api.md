# REST API Reference

All API endpoints are under `/api/v1/`. Requests must include `Content-Type: application/json`.

Rate limiting is enabled by default: 100 requests/minute for general API routes, 10 requests/minute for search.

## Response Format

All responses follow a consistent envelope:

**Success:**
```json
{
  "status": 200,
  "data": { ... }
}
```

**Error:**
```json
{
  "status": 400,
  "message": "invalid UUID"
}
```

## Plugins

### GET /api/v1/plugin/{id}

Get a single plugin by UUID.

**Parameters:**
- `id` (path) — Plugin UUID

**Response:** Full plugin object.

**Errors:**
- `400` — Invalid UUID format
- `404` — Plugin not found
- `503` — Plugins unavailable (no database)

### GET /api/v1/plugins/

List all plugins with pagination.

**Query Parameters:**
- `page` (int, default: 1) — Page number
- `per_page` (int, default: 100) — Items per page

**Response:**
```json
{
  "status": 200,
  "data": {
    "items": [
      {
        "id": "uuid",
        "name": "Plugin Name",
        "slug": "plugin-slug",
        "version": "1.0.0",
        "updated_at": "2026-02-22T00:00:00Z",
        "indexed": true
      }
    ],
    "total": 50000,
    "indexed": 48000,
    "page": 1,
    "per_page": 100
  }
}
```

## Themes

### GET /api/v1/theme/{id}

Get a single theme by UUID.

**Parameters:**
- `id` (path) — Theme UUID

**Errors:**
- `400` — Invalid UUID format
- `404` — Theme not found
- `503` — Themes unavailable

### GET /api/v1/themes/

List all themes with pagination. Same query parameters and response format as plugins.

## Cores

### GET /api/v1/core/{id}

Get a single core (WordPress version) by UUID.

**Parameters:**
- `id` (path) — Core UUID

**Errors:**
- `400` — Invalid UUID format
- `404` — Core not found
- `503` — Cores unavailable

### GET /api/v1/cores/

List all cores with pagination. Same query parameters and response format as plugins.

## Search

### POST /api/v1/search/

Create a new code search. This is an asynchronous operation — the search is queued and results are polled via the GET endpoint.

**Request Body:**
```json
{
  "term": "wp_enqueue_script",
  "repo": "plugins",
  "file_match": "\\.php$",
  "exclude_file_match": "vendor/",
  "case_sensitive": false,
  "public": true
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `term` | string | yes | Search term (regex supported) |
| `repo` | string | yes | Extension type: `plugins`, `themes`, or `cores` |
| `file_match` | string | no | Regex to include only matching filenames |
| `exclude_file_match` | string | no | Regex to exclude matching filenames |
| `case_sensitive` | bool | no | Case-sensitive search (default: false) |
| `public` | bool | no | Whether the search is publicly visible (default: true) |

**Response:**
```json
{
  "status": 200,
  "data": {
    "id": "uuid",
    "term": "wp_enqueue_script",
    "repo": "plugins",
    "status": "queued",
    "created_at": "2026-02-22T00:00:00Z"
  }
}
```

**Errors:**
- `400` — Missing or invalid fields
- `503` — Search unavailable

### GET /api/v1/search/{id}

Get search status and results.

**Parameters:**
- `id` (path) — Search UUID

**Response:** The search object, including results if the search is completed. Results are stored in S3 and loaded on demand.

Search status values: `queued`, `in_progress`, `completed`, `failed`.

**Errors:**
- `400` — Invalid UUID format
- `404` — Search not found

### GET /api/v1/searches/

List recent searches with pagination. Same pagination parameters as other list endpoints.

## Health & Monitoring

### GET /up

Liveness check. Returns `200 OK` with an empty body. No authentication or content-type required.

### GET /health

Readiness check. Returns health status of dependent services (database, S3, etc.).

### GET /metrics

Prometheus-format metrics endpoint.

## MCP (Model Context Protocol)

### /mcp

Streamable HTTP endpoint for the Model Context Protocol. Provides code search tools for AI agents. Rate limited to 100 requests/minute when rate limiting is enabled.

## Web Routes

These serve the HTML web interface:

| Method | Path | Description |
|---|---|---|
| GET | `/` | Home page |
| GET | `/about` | About page |
| GET | `/privacy` | Privacy policy |
| GET | `/terms` | Terms of service |
| GET | `/docs` | Documentation page |
| GET | `/data-sources` | Data sources overview |
| GET | `/data-sources/{type}` | Data source detail (plugins/themes/cores) |
| GET | `/data-sources/{type}/items` | Paginated items partial (htmx) |
| GET | `/data-sources/plugins/{slug}` | Plugin detail page |
| GET | `/data-sources/themes/{slug}` | Theme detail page |
| GET | `/data-sources/cores/{version}` | Core detail page |
| GET | `/searches` | Public search list |
| GET | `/search/{uuid}` | Search result page |
| GET | `/search/{uuid}/context` | Search context page |
| GET | `/search/{uuid}/extensions` | Search extensions partial (htmx) |
| GET | `/search/{uuid}/extension/{slug}` | Extension-specific results page |
| GET | `/search/{uuid}/export` | Export search results as CSV |
| GET | `/search/{uuid}/og.png` | OG image for search (rate limited) |
| POST | `/search` | Submit search (web form) |
| GET | `/searches/own` | Current user's searches |
| GET | `/my-searches` | Redirect to `/searches/own` |
| POST | `/search/{uuid}/report` | Report a search (requires login) |
| GET | `/login` | OAuth login page |
| GET | `/logout` | Logout |
| GET | `/auth/{provider}` | Begin OAuth flow |
| GET | `/auth/{provider}/callback` | OAuth callback |

## Admin Routes (Require Authentication + Admin Role)

| Method | Path | Description |
|---|---|---|
| GET | `/admin/reports` | View flagged search reports |
| POST | `/admin/reports/{id}/resolve` | Resolve a report |
| POST | `/admin/search/{uuid}/visibility` | Toggle search visibility |
| POST | `/admin/reindex` | Queue ad-hoc reindex (form: `repo_type`, `slug`) |
| POST | `/admin/maintenance` | Toggle maintenance mode |
