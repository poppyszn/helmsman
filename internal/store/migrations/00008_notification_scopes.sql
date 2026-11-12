-- +goose Up
-- A notification channel can be limited to chosen apps and environment names. Empty means all of them.
ALTER TABLE notification_channels ADD COLUMN apps TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE notification_channels ADD COLUMN envs TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE notification_channels DROP COLUMN envs;
ALTER TABLE notification_channels DROP COLUMN apps;
