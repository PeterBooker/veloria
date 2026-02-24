# @veloria/mcp

MCP server for searching WordPress plugin, theme, and core source code via [Veloria](https://veloria.dev).

Provides AI agents with tools to search across the entire WordPress extension ecosystem — every plugin, theme, and core release indexed and searchable via regex.

## Quick Start

### Claude Desktop

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "veloria": {
      "command": "npx",
      "args": ["-y", "@veloria/mcp"],
      "env": {
        "VELORIA_URL": "https://veloria.dev"
      }
    }
  }
}
```

### Claude Code

```bash
claude mcp add veloria -- npx -y @veloria/mcp
```

### Other MCP Clients

Any client that supports the stdio transport:

```bash
VELORIA_URL=https://veloria.dev npx @veloria/mcp
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `VELORIA_URL` | `http://localhost:9071` | Veloria instance URL |

## Tools

### `search_code`

Search WordPress extension source code. Returns a summary of matches on the first call, including a `search_id` for paginating through detailed results without re-running the search.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `query` | string | yes | | Search term (regex supported) |
| `repo` | string | | `plugins` | `plugins`, `themes`, or `cores` |
| `search_id` | string | | | ID from a previous search for pagination |
| `file_match` | string | | | Regex to include only matching filenames |
| `exclude_file_match` | string | | | Regex to exclude matching filenames |
| `case_sensitive` | boolean | | `false` | Case-sensitive search |
| `context_lines` | number | | `0` | Lines of context around each match (0-5) |
| `limit` | number | | `25` | Matches per page (1-100) |
| `offset` | number | | `0` | Pagination offset |

### `list_extensions`

List available WordPress extensions. Use this to discover valid slugs before searching.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `repo` | string | | `plugins` | `plugins`, `themes`, or `cores` |
| `search` | string | | | Filter by name or slug |
| `limit` | number | | `25` | Results per page (1-100) |
| `offset` | number | | `0` | Pagination offset |

## Supported Platforms

Pre-built binaries are available for:

- macOS (Apple Silicon, Intel)
- Linux (x64, ARM64)
- Windows (x64)

## Building from Source

Requires Go 1.25+:

```bash
go build -o veloria-mcp ./cmd/veloria-mcp/
```

## License

MIT
