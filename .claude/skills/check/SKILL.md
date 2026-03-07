---
name: check
description: Run pre-push quality checks (vet + lint + tests with race detector). Use before pushing code.
disable-model-invocation: true
argument-hint: "package-path"
---

# Pre-Push Quality Checks

Run the full quality gate: vet, lint, and tests with race detection. Stops at the first failure category.

## Usage

- `/check` - Check all packages
- `/check ./internal/repo/...` - Check specific package

## Steps

1. **Run go vet**
   ```bash
   go vet $ARGUMENTS 2>&1
   ```
   If no arguments: `go vet ./... 2>&1`

   If vet fails, report errors and stop.

2. **Run golangci-lint**
   ```bash
   golangci-lint run $ARGUMENTS 2>&1
   ```
   If no arguments: `golangci-lint run ./... 2>&1`

   If lint fails, report errors and stop.

3. **Run tests with race detector**
   ```bash
   go test -race -timeout 5m $ARGUMENTS 2>&1
   ```
   If no arguments: `go test -race -timeout 5m ./... 2>&1`

4. **Report results**
   ```
   ## Pre-Push Check Results

   | Step | Status |
   |------|--------|
   | go vet | pass/fail |
   | golangci-lint | pass/fail |
   | go test -race | pass/fail |

   ### Issues
   - [details of any failures]
   ```

## CI Alignment

This mirrors what CI runs on every push. Running `/check` locally before pushing avoids CI failures and saves time.
