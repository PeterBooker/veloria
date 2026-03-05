-- +goose Up
CREATE TABLE index_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_type     VARCHAR(20)  NOT NULL,
    slug          VARCHAR(255) NOT NULL,
    status        VARCHAR(20)  NOT NULL, -- success, failed, skipped
    error_message TEXT,
    duration_ms   BIGINT       NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_index_events_created_at ON index_events (created_at DESC);
CREATE INDEX idx_index_events_status     ON index_events (status, created_at DESC);
CREATE INDEX idx_index_events_repo_slug  ON index_events (repo_type, slug, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS index_events;
