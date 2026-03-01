---
name: integration-test
description: Run integration tests that require Docker (Postgres, MinIO via testcontainers). Use to validate database and storage behavior.
disable-model-invocation: true
argument-hint: [package-path]
allowed-tools: Bash(go test *, go install ./..., docker *), Read, Glob, Grep
---

# Integration Tests

Run integration tests that use testcontainers-go for Postgres and MinIO.

## Usage

- `/integration-test` - Run all integration tests
- `/integration-test ./internal/repo/...` - Run integration tests in a specific package

## Prerequisites

- Docker must be running (`docker info` should succeed)

## Steps

1. **Verify Docker is available**
   ```bash
   docker info > /dev/null 2>&1 && echo "Docker OK" || echo "Docker not available"
   ```
   If Docker is not available, inform the user and stop.

2. **Ensure binary is current**
   ```bash
   go install ./...
   ```

3. **Run integration tests**

   For a specific package:
   ```bash
   go test -tags integration -v -timeout 10m $ARGUMENTS 2>&1
   ```

   For all packages:
   ```bash
   go test -tags integration -v -timeout 10m ./... 2>&1
   ```

4. **Parse results**

   Report:
   ```
   ## Integration Test Results

   ### Passed
   - [list of passed tests]

   ### Failed
   - [list of failed tests with error details]

   ### Summary
   - Total: N | Passed: N | Failed: N
   - Duration: Ns
   ```

5. **On failure**, read the failing test files to understand what's being tested and suggest fixes.

## Test Infrastructure

Integration tests use the `//go:build integration` build tag and live alongside unit tests:

| File | Container | Purpose |
|------|-----------|---------|
| `internal/testutil/postgres.go` | PostgreSQL | Database integration tests |
| `internal/testutil/minio.go` | MinIO | S3-compatible storage tests |

## Tips

- First run is slower due to container image pulls
- Tests create and destroy containers automatically via testcontainers-go
- If a test hangs, check for orphaned containers: `docker ps --filter "label=org.testcontainers"`
