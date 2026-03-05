-- +goose Up
-- +goose StatementBegin
CREATE TABLE searches
(
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status         VARCHAR(50) NOT NULL DEFAULT 'queued',
    private        BOOLEAN NOT NULL,
    term           VARCHAR(255) NOT NULL,
    repo           VARCHAR(50) NOT NULL,
    results_size   BIGINT DEFAULT NULL,
    completed_at   TIMESTAMP DEFAULT NULL,
    created_at     TIMESTAMP NOT NULL DEFAULT now(),
    updated_at     TIMESTAMP NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMP DEFAULT NULL,
    user_id        UUID,

    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_searches_completed_at ON searches(completed_at) WHERE completed_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS searches;
-- +goose StatementEnd
