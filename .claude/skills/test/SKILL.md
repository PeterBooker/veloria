---
name: test
description: Run Go unit tests. Use after code changes to verify correctness.
disable-model-invocation: false
argument-hint: "package-path"
---

# Run Unit Tests

Run Go unit tests with optional package targeting and verbose output.

## Usage

- `/test` - Run all unit tests
- `/test ./internal/repo/...` - Run tests in specific package
- `/test -v ./internal/manager/...` - Verbose output

## Steps

1. **Run tests**

   If `$ARGUMENTS` specifies a package, use that:
   ```bash
   go test -timeout 5m $ARGUMENTS 2>&1
   ```

   Otherwise, run all packages:
   ```bash
   go test -timeout 5m ./... 2>&1
   ```

2. **Parse results**

   Report:
   ```
   ## Test Results

   ### Passed
   - [list of passed tests]

   ### Failed
   - [list of failed tests with error details]

   ### Summary
   - Total: N | Passed: N | Failed: N | Skipped: N
   - Duration: Ns
   ```

3. **On failure**, read the failing test and corresponding source files to understand the failure, then suggest a fix.

## Tips

- Use `-v` for verbose output when debugging specific test failures
- Use `-run TestName` to run a single test
- Use `-short` to skip long-running tests during iteration
- For race detection, use `/race-check` instead
- For integration tests (testcontainers), use `/integration-test` instead
