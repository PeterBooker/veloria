# API Reference

Veloria exposes a RESTful JSON API for searching WordPress code.

## Base URL

```
http://localhost:9071/api/v1
```

## Authentication

Currently no authentication is required. Rate limiting is enabled by default.

## Endpoints

### Health Check

```
GET /up
```

Returns `200 OK` if the service is running.

### Metrics

```
GET /metrics
```

Returns Prometheus metrics in text format.

---

## Search

### Create Search

Performs a code search across the specified repository.

```
POST /api/v1/search
```

#### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `term` | string | Yes | The search term or pattern |
| `repo` | string | No | Repository to search: `plugins`, `themes`, or `cores`. Defaults to `plugins` |
| `file_match` | string | No | Regex pattern to filter filenames |
| `case_sensitive` | boolean | No | Enable case-sensitive search. Defaults to `false` |

#### Example Request

```bash
curl -X POST http://localhost:9071/api/v1/search \
  -H "Content-Type: application/json" \
  -d '{
    "term": "wp_enqueue_script",
    "repo": "plugins",
    "file_match": "\\.php$",
    "case_sensitive": false
  }'
```

#### Response

```json
{
  "term": "wp_enqueue_script",
  "repo": "plugins",
  "results": [
    {
      "slug": "woocommerce",
      "name": "WooCommerce",
      "version": "8.5.2",
      "active_installs": 5000000,
      "matches": [
        {
          "filename": "woocommerce/includes/class-wc-frontend-scripts.php",
          "matches": [
            {
              "line": "        wp_enqueue_script( 'wc-cart', ... );",
              "line_number": 245,
              "before": [],
              "after": []
            }
          ]
        }
      ],
      "total_matches": 42
    }
  ],
  "total": 1
}
```

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `term` | string | The search term used |
| `repo` | string | The repository searched |
| `results` | array | Array of extension results |
| `results[].slug` | string | Extension slug (or version for cores) |
| `results[].name` | string | Extension display name |
| `results[].version` | string | Current version |
| `results[].active_installs` | integer | Active install count (0 for cores) |
| `results[].matches` | array | File matches within this extension |
| `results[].matches[].filename` | string | Relative file path |
| `results[].matches[].matches` | array | Line matches within the file |
| `results[].matches[].matches[].line` | string | The matching line content |
| `results[].matches[].matches[].line_number` | integer | 1-indexed line number |
| `results[].total_matches` | integer | Total line matches in this extension |
| `total` | integer | Total extensions with matches |

Results are sorted by `active_installs` descending, then by `slug` alphabetically.

---

### View Search

Retrieves a previously saved search by ID.

```
GET /api/v1/search/{id}
```

#### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | UUID | The search record ID |

#### Example Request

```bash
curl http://localhost:9071/api/v1/search/550e8400-e29b-41d4-a716-446655440000
```

---

## Plugins

### View Plugin

Retrieves metadata for a single plugin.

```
GET /api/v1/plugin/{id}
```

#### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | UUID | The plugin's internal ID |

#### Response

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "WooCommerce",
  "slug": "woocommerce",
  "version": "8.5.2",
  "requires": "6.4",
  "tested": "6.5",
  "requires_php": "7.4",
  "rating": 92,
  "active_installs": 5000000,
  "downloaded": 350000000,
  "short_description": "An eCommerce toolkit that helps you sell anything.",
  "download_link": "https://downloads.wordpress.org/plugin/woocommerce.8.5.2.zip",
  "tags": {
    "ecommerce": "ecommerce",
    "store": "store"
  }
}
```

---

## Themes

### View Theme

```
GET /api/v1/theme/{id}
```

*Currently not implemented - returns 404.*

---

## Cores

### View Core

```
GET /api/v1/core/{id}
```

*Currently not implemented - returns 404.*

---

## Error Responses

All endpoints return errors in plain text format with appropriate HTTP status codes.

| Status Code | Description |
|-------------|-------------|
| 400 | Bad Request - Invalid input or missing required fields |
| 404 | Not Found - Resource doesn't exist |
| 500 | Internal Server Error - Server-side error |

### Example Error Response

```
HTTP/1.1 400 Bad Request
Content-Type: text/plain

term is a required field
```

---

## Rate Limiting

Rate limiting is enabled by default. The service uses chi middleware for request throttling. Configure via the `HTTP_RATE_LIMIT_ENABLED` environment variable.

## Request Timeout

All requests have a 5-second timeout configured via middleware.
