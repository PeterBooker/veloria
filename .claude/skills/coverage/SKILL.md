---
name: coverage
description: Run tests with coverage analysis and identify untested code paths. Use to find gaps before releases.
disable-model-invocation: true
argument-hint: "package-path"
---

# Test Coverage Analysis

Run Go tests with coverage profiling to identify untested code paths.

## Usage

- `/coverage` - Coverage for all packages
- `/coverage ./internal/repo/...` - Coverage for specific package

## Steps

1. **Run tests with coverage**

   If `$ARGUMENTS` specifies a package:
   ```bash
   go test -coverprofile=coverage.out -covermode=atomic -timeout 5m $ARGUMENTS 2>&1
   ```

   Otherwise, run all packages:
   ```bash
   go test -coverprofile=coverage.out -covermode=atomic -timeout 5m ./... 2>&1
   ```

2. **Generate coverage summary**
   ```bash
   go tool cover -func=coverage.out 2>&1
   ```

3. **Identify low-coverage packages**

   Parse the output and flag packages below 50% coverage.

4. **Report findings**
   ```
   ## Coverage Results

   ### Package Summary
   | Package | Coverage | Status |
   |---------|----------|--------|

   ### Overall
   - Total coverage: N%

   ### Low Coverage (< 50%)
   - [packages needing attention]

   ### Uncovered Functions
   - [key uncovered functions in critical packages]
   ```

5. **Cleanup**
   ```bash
   rm -f coverage.out
   ```

## Critical Packages

Pay special attention to coverage in:
- `internal/repo/` - Data access layer
- `internal/manager/` - Orchestration logic
- `internal/index/` - Search indexing
- `internal/search/` - Search API handlers

## Tips

- For HTML coverage visualization: `go tool cover -html=coverage.out -o coverage.html`
- Integration test coverage requires `-tags integration` and Docker
- Test files (`_test.go`) are excluded from coverage automatically
