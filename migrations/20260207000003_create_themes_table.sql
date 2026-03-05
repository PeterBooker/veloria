-- +goose Up
-- +goose StatementBegin
CREATE TABLE themes
(
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name               VARCHAR(255) NOT NULL,
    slug               VARCHAR(255) NOT NULL UNIQUE,
    version            VARCHAR(50) NOT NULL,
    requires           VARCHAR(50) NOT NULL,
    tested             VARCHAR(50) NOT NULL,
    requires_php       VARCHAR(50) NOT NULL,
    required_plugins   JSONB NOT NULL DEFAULT '[]'::jsonb,
    rating             SMALLINT NOT NULL DEFAULT 0,
    active_installs    BIGINT NOT NULL DEFAULT 0,
    downloaded         BIGINT NOT NULL DEFAULT 0,
    updated_at         TIMESTAMP NOT NULL DEFAULT now(),
    created_at         TIMESTAMP NOT NULL DEFAULT now(),
    deleted_at         TIMESTAMP DEFAULT NULL,
    closed_at          TIMESTAMP DEFAULT NULL,
    short_description  TEXT,
    download_link      TEXT NOT NULL,
    tags               JSONB NOT NULL DEFAULT '[]'::jsonb,
    file_count         INTEGER NOT NULL DEFAULT 0,
    total_size         BIGINT NOT NULL DEFAULT 0,
    largest_files      JSONB NOT NULL DEFAULT '[]'::jsonb
);

CREATE INDEX idx_themes_closed_at ON themes (closed_at) WHERE closed_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS themes;
-- +goose StatementEnd
