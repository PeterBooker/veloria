-- +goose Up

ALTER TABLE plugins
    ADD COLUMN IF NOT EXISTS retry_count     SMALLINT    NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS indexed_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS index_status    VARCHAR(20) NOT NULL DEFAULT 'pending';

ALTER TABLE themes
    ADD COLUMN IF NOT EXISTS retry_count     SMALLINT    NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS indexed_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS index_status    VARCHAR(20) NOT NULL DEFAULT 'pending';

ALTER TABLE cores
    ADD COLUMN IF NOT EXISTS retry_count     SMALLINT    NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS indexed_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS index_status    VARCHAR(20) NOT NULL DEFAULT 'pending';

-- Backfill: mark extensions that already have indexes as indexed.
UPDATE plugins SET index_status = 'indexed', indexed_at = updated_at WHERE file_count > 0;
UPDATE themes  SET index_status = 'indexed', indexed_at = updated_at WHERE file_count > 0;
UPDATE cores   SET index_status = 'indexed', indexed_at = updated_at WHERE file_count > 0;

-- Partial indexes for querying non-indexed extensions efficiently.
CREATE INDEX IF NOT EXISTS idx_plugins_index_status ON plugins (index_status) WHERE index_status != 'indexed';
CREATE INDEX IF NOT EXISTS idx_themes_index_status  ON themes  (index_status) WHERE index_status != 'indexed';
CREATE INDEX IF NOT EXISTS idx_cores_index_status   ON cores   (index_status) WHERE index_status != 'indexed';

-- +goose Down

DROP INDEX IF EXISTS idx_plugins_index_status;
DROP INDEX IF EXISTS idx_themes_index_status;
DROP INDEX IF EXISTS idx_cores_index_status;

ALTER TABLE plugins DROP COLUMN IF EXISTS retry_count, DROP COLUMN IF EXISTS last_attempt_at, DROP COLUMN IF EXISTS indexed_at, DROP COLUMN IF EXISTS index_status;
ALTER TABLE themes  DROP COLUMN IF EXISTS retry_count, DROP COLUMN IF EXISTS last_attempt_at, DROP COLUMN IF EXISTS indexed_at, DROP COLUMN IF EXISTS index_status;
ALTER TABLE cores   DROP COLUMN IF EXISTS retry_count, DROP COLUMN IF EXISTS last_attempt_at, DROP COLUMN IF EXISTS indexed_at, DROP COLUMN IF EXISTS index_status;
