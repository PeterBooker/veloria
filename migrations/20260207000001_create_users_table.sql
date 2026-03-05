-- +goose Up
-- +goose StatementBegin
CREATE TABLE users
(
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           VARCHAR(255) NOT NULL,
    email          VARCHAR(255) NOT NULL UNIQUE,
    password_hash  VARCHAR(255),
    is_admin       BOOLEAN NOT NULL DEFAULT FALSE,
    avatar_url     VARCHAR(500),
    updated_at     TIMESTAMP NOT NULL DEFAULT now(),
    created_at     TIMESTAMP NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMP DEFAULT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
