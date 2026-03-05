-- +goose Up
-- +goose StatementBegin
CREATE TABLE oauth_identities (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider       VARCHAR(50) NOT NULL,
    provider_id    VARCHAR(255) NOT NULL,
    access_token   VARCHAR(500),
    refresh_token  VARCHAR(500),
    expires_at     TIMESTAMP,
    created_at     TIMESTAMP NOT NULL DEFAULT now(),
    updated_at     TIMESTAMP NOT NULL DEFAULT now(),

    UNIQUE(provider, provider_id)
);

CREATE INDEX idx_oauth_identities_user_id ON oauth_identities(user_id);
CREATE INDEX idx_oauth_identities_provider ON oauth_identities(provider, provider_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS oauth_identities;
-- +goose StatementEnd
