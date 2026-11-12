-- +goose Up
-- People who sign in without any role binding are listed for an admin to grant access. Dismissing one hides it
-- until that person signs in again.
ALTER TABLE users ADD COLUMN access_dismissed_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users DROP COLUMN access_dismissed_at;
