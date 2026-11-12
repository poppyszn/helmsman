-- +goose Up
-- Notifications: channels (email recipients or a webhook) that receive the events an admin chooses, a
-- log of deliveries with retries, and the small state the watcher keeps so a restart does not repeat
-- what was already announced. SMTP settings are one more integration kind. Additive.
ALTER TABLE integrations DROP CONSTRAINT integrations_kind_check;
ALTER TABLE integrations ADD CONSTRAINT integrations_kind_check CHECK (kind IN ('loki', 'smtp'));

CREATE TABLE notification_channels (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('email', 'webhook')),
    config     JSONB NOT NULL DEFAULT '{}',
    secret_enc BYTEA,
    events     TEXT[] NOT NULL DEFAULT '{}',
    tier       TEXT NOT NULL DEFAULT 'all' CHECK (tier IN ('all', 'prod', 'nonprod')),
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_by BIGINT REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX notification_channels_name ON notification_channels (lower(name));

CREATE TABLE notification_deliveries (
    id              BIGSERIAL PRIMARY KEY,
    channel_id      BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    event_type      TEXT NOT NULL,
    dedupe_key      TEXT NOT NULL,
    title           TEXT NOT NULL,
    payload         JSONB NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts        INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ
);
CREATE INDEX notification_deliveries_due ON notification_deliveries (status, next_attempt_at);
CREATE INDEX notification_deliveries_recent ON notification_deliveries (created_at DESC);
CREATE INDEX notification_deliveries_key ON notification_deliveries (channel_id, dedupe_key, created_at DESC);

CREATE TABLE notification_state (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE notification_state, notification_deliveries, notification_channels;
DELETE FROM integrations WHERE kind = 'smtp';
ALTER TABLE integrations DROP CONSTRAINT integrations_kind_check;
ALTER TABLE integrations ADD CONSTRAINT integrations_kind_check CHECK (kind IN ('loki'));
