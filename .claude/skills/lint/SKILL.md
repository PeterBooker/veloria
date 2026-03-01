---
name: lint
description: Run golangci-lint and static analysis on Go code. Use before pushing or to check code quality.
disable-model-invocation: true
argument-hint: [package-path]
allowed-tools: Bash(golangci-lint *, go install ./..., go vet *), Read, Glob, Grep
---

# Go Linting

Run golangci-lint to check code quality and catch issues before CI.

## Usage

- `/lint` - Lint all packages
- `/lint ./internal/repo/...` - Lint a specific package

## Steps

1. **Ensure binary is current**
   ```bash
   go install ./...
   ```

2. **Run golangci-lint**
   ```bash
   golangci-lint run $ARGUMENTS 2>&1
   ```

   If no arguments provided, run on all packages:
   ```bash
   golangci-lint run ./... 2>&1
   ```

3. **If golangci-lint is not installed**, install it:
   ```bash
   go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
   ```
   Then re-run step 2.

4. **Parse and report findings**

   Group issues by severity and file:
   ```
   ## Lint Results

   ### Errors
   - [list of errors with file:line references]

   ### Warnings
   - [list of warnings with file:line references]

   ### Summary
   - Total issues: N
   - Files affected: N
   ```

5. **Offer to fix auto-fixable issues**
   If issues are auto-fixable, offer to run:
   ```bash
   golangci-lint run --fix $ARGUMENTS 2>&1
   ```

## Configuration

The project uses `.golangci.yml` in the repo root. Current exclusions:
- `internal/codesearch` - excluded from linting and formatting

## CI Alignment

The CI pipeline runs:
- `go vet ./...`
- `gosec` (see `/security-scan` skill)

Running `/lint` locally before pushing catches most CI failures.
