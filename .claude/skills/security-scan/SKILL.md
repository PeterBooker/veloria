---
name: security-scan
description: Run gosec and govulncheck to find security vulnerabilities. Use before releases or after dependency changes.
disable-model-invocation: true
argument-hint: "package-path"
---

# Security Scanning

Run Go security scanners to find vulnerabilities in code and dependencies.

## Usage

- `/security-scan` - Full scan (gosec + govulncheck)
- `/security-scan ./internal/api/...` - Scan specific package with gosec

## Steps

1. **Run gosec (static analysis)**
   ```bash
   gosec -exclude-generated -exclude-dir=internal/codesearch $ARGUMENTS 2>&1
   ```

   If no arguments provided:
   ```bash
   gosec -exclude-generated -exclude-dir=internal/codesearch ./... 2>&1
   ```

   If gosec is not installed:
   ```bash
   go install github.com/securego/gosec/v2/cmd/gosec@latest
   ```

2. **Run govulncheck (dependency vulnerabilities)**
   ```bash
   govulncheck ./... 2>&1
   ```

   If govulncheck is not installed:
   ```bash
   go install golang.org/x/vuln/cmd/govulncheck@latest
   ```

3. **Report findings**
   ```
   ## Security Scan Results

   ### gosec (Code Analysis)
   | Severity | File | Line | Issue |
   |----------|------|------|-------|

   ### govulncheck (Dependencies)
   | Module | Vulnerability | Severity | Fixed In |
   |--------|--------------|----------|----------|

   ### Summary
   - Code issues: N (high: N, medium: N, low: N)
   - Vulnerable dependencies: N
   ```

4. **For each finding**, suggest a specific fix or mitigation.

## CI Alignment

The CI pipeline runs:
```bash
gosec -exclude-generated -exclude-dir=internal/codesearch ./...
```

A separate `vulnerability.yml` workflow checks for dependency vulnerabilities.

Running `/security-scan` locally catches both before pushing.

## Common gosec Rules

| Rule | Description | Common Fix |
|------|-------------|------------|
| G101 | Hardcoded credentials | Use env vars |
| G104 | Unhandled errors | Add error checks |
| G304 | File path from variable | Validate/sanitize path |
| G401 | Weak crypto (MD5/SHA1) | Use SHA-256+ |
| G501 | Insecure TLS | Use TLS 1.2+ |
