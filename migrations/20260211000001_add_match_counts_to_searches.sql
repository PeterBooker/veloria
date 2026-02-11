-- +goose Up
-- +goose StatementBegin
ALTER TABLE searches
    ADD COLUMN total_matches INT DEFAULT NULL,
    ADD COLUMN total_extensions INT DEFAULT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE searches
    DROP COLUMN IF EXISTS total_matches,
    DROP COLUMN IF EXISTS total_extensions;
-- +goose StatementEnd
