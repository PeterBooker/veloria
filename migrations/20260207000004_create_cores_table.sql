-- +goose Up
-- +goose StatementBegin
CREATE TABLE cores
(
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           VARCHAR(255) NOT NULL,
    version        VARCHAR(255) NOT NULL UNIQUE,
    updated_at     TIMESTAMP NOT NULL DEFAULT now(),
    created_at     TIMESTAMP NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMP DEFAULT NULL,
    file_count     INTEGER NOT NULL DEFAULT 0,
    total_size     BIGINT NOT NULL DEFAULT 0,
    largest_files  JSONB NOT NULL DEFAULT '[]'::jsonb
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS cores;
-- +goose StatementEnd
