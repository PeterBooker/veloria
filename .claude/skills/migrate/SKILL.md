---
name: migrate
description: Create and manage database migrations using goose. Use for schema changes, new tables, or index optimization.
disable-model-invocation: true
argument-hint: [create|up|down|status|validate] [name]
allowed-tools: Bash(go install ./..., veloria *, goose *, ls *), Read, Glob, Grep, Write
---

# Database Migration Management

Create, run, and validate database migrations using goose.

## Usage

- `/migrate create add_user_preferences` - Create new migration
- `/migrate up` - Run pending migrations
- `/migrate down` - Rollback last migration
- `/migrate status` - Show migration status
- `/migrate validate` - Validate migrations and GORM models

## Commands

### create [name]

Create a new migration file.

1. **Generate migration file**
   ```bash
   goose -dir migrations create $1 sql
   ```

2. **Open the created file** and add template:
   ```sql
   -- +goose Up
   -- +goose StatementBegin

   -- +goose StatementEnd

   -- +goose Down
   -- +goose StatementBegin

   -- +goose StatementEnd
   ```

3. **Remind about migration rules:**
   - Always include reversible DOWN migration
   - Add indexes for columns in WHERE/JOIN clauses
   - Use appropriate column types
   - Never modify existing migrations

### up

Run all pending migrations.

1. **Ensure binary is current**
   ```bash
   go install ./...
   ```

2. **Check current status**
   ```bash
   veloria migrate status
   ```

3. **Run migrations**
   ```bash
   veloria migrate up
   ```

4. **Verify success**
   ```bash
   veloria migrate status
   ```

### down

Rollback the last migration.

1. **Show current status** to confirm which migration will be rolled back

2. **Run rollback**
   ```bash
   veloria migrate down
   ```

3. **Verify rollback** by checking status again

### status

Show current migration status.

```bash
veloria migrate status
```

Display:
- Applied migrations with timestamps
- Pending migrations
- Current database version

### validate

Validate migrations and check GORM model alignment.

1. **List all migration files**
   ```bash
   ls -la migrations/*.sql
   ```

2. **Read each migration** and check for:
   - Both UP and DOWN sections present
   - StatementBegin/StatementEnd blocks
   - No destructive operations without confirmation
   - Proper index creation

3. **Read GORM models** in `internal/repo/`:
   - `plugin.go` - Plugin model
   - `theme.go` - Theme model
   - `core.go` - Core model
   - Check for any models in other files

4. **Compare schema**
   - Verify model fields match migration columns
   - Check for missing indexes on foreign keys
   - Identify any mismatches

5. **Report findings**
   ```
   ## Migration Validation Report

   ### Migrations
   | File | UP | DOWN | Status |
   |------|----|----- |--------|

   ### Model Alignment
   | Model | Table | Issues |
   |-------|-------|--------|

   ### Recommendations
   - [specific issues found]
   ```

## Migration Best Practices

### DO
- Create new migrations for all schema changes
- Include reversible DOWN migrations
- Add indexes for frequently queried columns
- Use transactions for multi-statement migrations
- Test rollback before committing

### DON'T
- Modify existing migrations (create new ones instead)
- Use TEXT for small string fields (use VARCHAR with limit)
- Forget indexes on foreign key columns
- Skip DOWN migration for "simple" changes

## Index Guidelines

Add indexes for:
- Foreign key columns (`user_id`, `plugin_id`)
- Columns in WHERE clauses (`slug`, `status`)
- Columns in ORDER BY (`created_at`, `active_installs`)
- Composite indexes for common query patterns

Example:
```sql
-- +goose Up
CREATE INDEX idx_searches_user_status ON searches(user_id, status);

-- +goose Down
DROP INDEX idx_searches_user_status;
```

## Existing Tables

Current schema includes:
- `users` - User accounts with OAuth
- `plugins` - WordPress plugin metadata
- `themes` - WordPress theme metadata
- `cores` - WordPress core releases
- `searches` - Search history and results
- `oauth_identities` - OAuth provider credentials
- `sessions` - User sessions
- `largest_repo_files` - File size statistics

Review existing migrations in `migrations/` before creating new ones.
