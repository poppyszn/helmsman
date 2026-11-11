-- +goose Up
-- M5: optional integrations an admin connects from the UI. Today there is one, kind 'loki', which
-- turns on log history. The password is stored encrypted with IDP_ENCRYPTION_KEY, never in config.
CREATE TABLE integrations (
    kind             TEXT PRIMARY KEY CHECK (kind IN ('loki')),
    enabled          BOOLEAN NOT NULL DEFAULT false,
    config           JSONB NOT NULL DEFAULT '{}',
    secret_enc       BYTEA,
    updated_by       BIGINT REFERENCES users(id),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_check_at    TIMESTAMPTZ,
    last_check_ok    BOOLEAN,
    last_check_error TEXT
);

-- +goose Down
DROP TABLE integrations;
