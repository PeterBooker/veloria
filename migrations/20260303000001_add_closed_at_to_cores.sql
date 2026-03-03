-- +goose Up
ALTER TABLE cores ADD COLUMN closed_at TIMESTAMP DEFAULT NULL;
CREATE INDEX idx_cores_closed_at ON cores (closed_at) WHERE closed_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_cores_closed_at;
ALTER TABLE cores DROP COLUMN IF EXISTS closed_at;
