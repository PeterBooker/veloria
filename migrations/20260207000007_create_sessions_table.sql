-- +goose Up
-- +goose StatementBegin
CREATE TABLE sessions (
    id          VARCHAR(128) PRIMARY KEY,
    data        BYTEA NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT now(),
    updated_at  TIMESTAMP NOT NULL DEFAULT now(),
    expires_at  TIMESTAMP NOT NULL
);

CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS sessions;
-- +goose StatementEnd
