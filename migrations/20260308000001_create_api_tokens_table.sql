-- +goose Up
-- +goose StatementBegin
CREATE TABLE api_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         VARCHAR(100) NOT NULL,
    token_hash   VARCHAR(64) NOT NULL UNIQUE,
    suffix       VARCHAR(8) NOT NULL,
    last_used_at TIMESTAMP,
    expires_at   TIMESTAMP,
    created_at   TIMESTAMP NOT NULL DEFAULT now(),

    CONSTRAINT uq_user_token_name UNIQUE(user_id, name)
);

CREATE INDEX idx_api_tokens_user_id ON api_tokens(user_id);
CREATE INDEX idx_api_tokens_token_hash ON api_tokens(token_hash);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS api_tokens;
-- +goose StatementEnd
