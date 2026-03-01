# Contributing to Veloria

Thanks for your interest in contributing.

## Before You Start

- Read [docs/architecture.md](docs/architecture.md) and [docs/development.md](docs/development.md).
- Check open issues before starting work to avoid duplicate effort.
- For large changes, open an issue first so the approach can be aligned early.

## Local Setup

1. Install prerequisites from [docs/development.md](docs/development.md).
2. Start local services:
   ```bash
   docker compose up -d
   ```
3. Copy env file and adjust values if needed:
   ```bash
   cp .env.example .env
   ```
4. Build frontend assets:
   ```bash
   go generate ./assets/...
   ```
5. Run migrations and start the app:
   ```bash
   go run ./cmd/veloria migrate up
   go run ./cmd/veloria
   ```

## Development Expectations

- Keep changes focused and scoped to one problem.
- Follow existing code style and package patterns.
- Respect performance constraints for search/index paths.
- Avoid unrelated refactors in the same PR.

## Required Checks

Run these before opening a pull request:

```bash
go test ./...
go test -race ./...
go vet ./...
```

If you changed templates, frontend CSS, or frontend dependencies:

```bash
go generate ./assets/...
```

If you changed performance-sensitive code in `internal/index`, `internal/repo`, `internal/manager`, or `internal/tasks`, include benchmark output in your PR:

```bash
go test -bench=. ./internal/index/...
```

## Pull Request Guidelines

- Use a clear title and explain the user-visible impact.
- Link related issues using `Fixes #<id>` where applicable.
- Include tests for behavior changes.
- Update docs when behavior, config, or API changes.
- Keep PRs reviewable; split broad work into multiple PRs when possible.

## Commit Messages

Use concise, descriptive commit messages. Imperative style is preferred, for example:

- `repo: fix index swap lock ordering`
- `api/search: validate regex options early`

## Reporting Security Issues

Do not open public issues for vulnerabilities. Follow [SECURITY.md](SECURITY.md).
