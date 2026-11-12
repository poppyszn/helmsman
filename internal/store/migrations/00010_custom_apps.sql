-- +goose Up
-- External tools an admin adds to the Apps page (Grafana, Sentry, the wiki...), so every tool is one click away
-- from one place. Links only: Helmsman does not call them.
CREATE TABLE custom_apps (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    url         TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    tags        TEXT[] NOT NULL DEFAULT '{}',
    created_by  BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX custom_apps_name ON custom_apps (lower(name));

-- +goose Down
DROP TABLE custom_apps;
