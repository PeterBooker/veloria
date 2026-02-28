-- +goose Up
CREATE TABLE datasources (
    repo_type         VARCHAR(20) PRIMARY KEY,
    last_full_scan_at TIMESTAMPTZ,
    last_update_at    TIMESTAMPTZ
);

-- Seed rows for each repo type so UPSERTs work cleanly.
INSERT INTO datasources (repo_type) VALUES ('plugins'), ('themes'), ('cores');

-- +goose Down
DROP TABLE IF EXISTS datasources;
