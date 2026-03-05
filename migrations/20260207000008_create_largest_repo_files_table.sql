-- +goose Up
-- +goose StatementBegin
CREATE TABLE largest_repo_files
(
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_type VARCHAR(10) NOT NULL,
    slug      VARCHAR(255) NOT NULL,
    name      VARCHAR(255) NOT NULL,
    path      TEXT NOT NULL,
    size      BIGINT NOT NULL
);

CREATE INDEX idx_largest_repo_files_lookup ON largest_repo_files (repo_type, size DESC);
CREATE UNIQUE INDEX idx_largest_repo_files_unique ON largest_repo_files (repo_type, slug, path);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS largest_repo_files;
-- +goose StatementEnd
