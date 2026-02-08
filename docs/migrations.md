# Migrations

Veloria uses [Goose](https://github.com/pressly/goose) for database migrations. Migration SQL files live in the `migrations/` directory and are applied via the `veloria-migrate` CLI.

## Where migrations live

- SQL migration files: `migrations/*.sql`
- Migration runner: `cmd/veloria-migrate/main.go`

## Database configuration

`veloria-migrate` reads the same environment variables as the server via `internal/config` (e.g. from `.env`).

Required variables:

```
DB_HOST
DB_PORT
DB_DATABASE
DB_USERNAME
DB_PASSWORD
```

## Common commands

From the repository root:

```
# Show migration status

go run ./cmd/veloria-migrate status

# Apply all pending migrations

go run ./cmd/veloria-migrate up

# Apply a single migration

go run ./cmd/veloria-migrate up-by-one

# Roll back one migration

go run ./cmd/veloria-migrate down

# Roll back all migrations

go run ./cmd/veloria-migrate reset
```

You can also build the binary and run it directly:

```
go build -o veloria-migrate ./cmd/veloria-migrate
./veloria-migrate status
```

## Creating a new migration

```
go run ./cmd/veloria-migrate create add_example_table sql
```

This creates a new timestamped file in `migrations/` with the Goose headers for `Up` and `Down` sections. Edit that file to add your SQL.

## File format

Each SQL migration uses Goose directives:

```
-- +goose Up
-- +goose StatementBegin
-- SQL here
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQL here
-- +goose StatementEnd
```

## Notes

- Migrations are not auto-applied by the server process; use `veloria-migrate` to apply them.
- If you use `gen_random_uuid()` defaults, ensure `pgcrypto` is enabled in the database:

```
CREATE EXTENSION IF NOT EXISTS pgcrypto;
```
