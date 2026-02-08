-- +goose Up
-- +goose StatementBegin
ALTER TABLE plugins
    ALTER COLUMN version TYPE VARCHAR(255),
    ALTER COLUMN requires TYPE VARCHAR(255),
    ALTER COLUMN tested TYPE VARCHAR(255),
    ALTER COLUMN requires_php TYPE VARCHAR(255);

ALTER TABLE themes
    ALTER COLUMN version TYPE VARCHAR(255),
    ALTER COLUMN requires TYPE VARCHAR(255),
    ALTER COLUMN tested TYPE VARCHAR(255),
    ALTER COLUMN requires_php TYPE VARCHAR(255);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE plugins
    ALTER COLUMN version TYPE VARCHAR(50),
    ALTER COLUMN requires TYPE VARCHAR(50),
    ALTER COLUMN tested TYPE VARCHAR(50),
    ALTER COLUMN requires_php TYPE VARCHAR(50);

ALTER TABLE themes
    ALTER COLUMN version TYPE VARCHAR(50),
    ALTER COLUMN requires TYPE VARCHAR(50),
    ALTER COLUMN tested TYPE VARCHAR(50),
    ALTER COLUMN requires_php TYPE VARCHAR(50);
-- +goose StatementEnd
