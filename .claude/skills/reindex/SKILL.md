---
name: reindex
description: Trigger reindexing of a specific WordPress extension. Use to rebuild the search index for a plugin, theme, or core version.
disable-model-invocation: true
argument-hint: [repo] [slug]
allowed-tools: Bash(veloria *, go install ./..., curl *), Read, Glob, Grep
---

# Reindex Extension

Trigger reindexing of a specific WordPress plugin, theme, or core version.

## Usage

- `/reindex plugins akismet` - Reindex the Akismet plugin
- `/reindex themes flavor` - Reindex the flavor theme
- `/reindex cores 6.7.1` - Reindex WordPress core 6.7.1

## Steps

1. **Parse arguments**
   - `$ARGUMENTS[0]` or `$0`: Repository type (`plugins`, `themes`, or `cores`)
   - `$ARGUMENTS[1]` or `$1`: Extension slug (or version for cores)
   - Both are required

2. **Ensure binary is current**
   ```bash
   go install ./...
   ```

3. **Check if the server is running**
   ```bash
   curl -s http://localhost:8585/api/health 2>&1
   ```

4. **If server is running**, trigger reindex via API:
   ```bash
   curl -s -X POST "http://localhost:8585/api/reindex/$0/$1" 2>&1
   ```

5. **If server is not running**, use the index CLI directly:
   - First, determine the zip URL for the extension
   - Then run:
     ```bash
     veloria index --repo=$0 --slug=$1 --zipurl="$ZIP_URL" 2>&1
     ```

6. **Report results**
   ```
   ## Reindex Results

   - Extension: $0/$1
   - Method: API / CLI
   - Status: success / failed
   - Details: [output from command]
   ```

## CLI vs API

| Method | When | Notes |
|--------|------|-------|
| API (`/api/reindex`) | Server running | Handles zip URL lookup, DB update, and hot-swap |
| CLI (`veloria index`) | Server not running | Requires explicit `--zipurl`, no DB update |

The API method is preferred as it handles the full pipeline including database updates and live index hot-swap.
