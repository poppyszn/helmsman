-- +goose Up
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    github_id     BIGINT NOT NULL UNIQUE,
    login         TEXT NOT NULL,
    name          TEXT,
    email         TEXT,
    avatar_url    TEXT,
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE,
    disabled_at   TIMESTAMPTZ,
    last_login_at TIMESTAMPTZ,
    gh_token_enc  BYTEA,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_orgs (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org        TEXT NOT NULL,
    teams      TEXT[] NOT NULL DEFAULT '{}',
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, org)
);

CREATE TABLE sessions (
    id_hash    BYTEA PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    ip         TEXT,
    user_agent TEXT,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx ON sessions(user_id);

CREATE TABLE roles (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    permissions TEXT[] NOT NULL
);

CREATE TABLE role_bindings (
    id           BIGSERIAL PRIMARY KEY,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'team')),
    subject      TEXT NOT NULL,
    role_id      BIGINT NOT NULL REFERENCES roles(id),
    app_id       TEXT NOT NULL DEFAULT '*',
    env          TEXT NOT NULL DEFAULT '*',
    created_by   BIGINT REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (subject_type, subject, role_id, app_id, env)
);

CREATE TABLE apps (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    repo        TEXT NOT NULL,
    catalog_rev TEXT NOT NULL,
    deleted_at  TIMESTAMPTZ
);

CREATE TABLE app_envs (
    id                 BIGSERIAL PRIMARY KEY,
    app_id             TEXT NOT NULL REFERENCES apps(id),
    env                TEXT NOT NULL,
    tier               TEXT NOT NULL CHECK (tier IN ('nonprod', 'prod')),
    agent_id           TEXT NOT NULL,
    spec               JSONB NOT NULL,
    catalog_rev        TEXT NOT NULL,
    deleted_at         TIMESTAMPTZ,
    UNIQUE (app_id, env)
);

CREATE TABLE agents (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    credential_hash  BYTEA,
    join_token_hash  BYTEA,
    join_expires_at  TIMESTAMPTZ,
    version          TEXT,
    hostname         TEXT,
    last_seen_at     TIMESTAMPTZ,
    revoked_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE container_state (
    agent_id        TEXT NOT NULL REFERENCES agents(id),
    compose_project TEXT NOT NULL,
    service         TEXT NOT NULL,
    container_id    TEXT NOT NULL,
    image           TEXT,
    revision        TEXT,
    state           TEXT,
    health          TEXT,
    started_at      TIMESTAMPTZ,
    restart_count   INT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, compose_project, service)
);

CREATE TABLE container_events (
    id        BIGSERIAL PRIMARY KEY,
    agent_id  TEXT NOT NULL,
    container TEXT NOT NULL,
    event     TEXT NOT NULL,
    at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX container_events_at_idx ON container_events(at);

CREATE TABLE health_checks (
    id         BIGSERIAL PRIMARY KEY,
    app_env_id BIGINT NOT NULL REFERENCES app_envs(id),
    status     TEXT NOT NULL,
    http_code  INT,
    latency_ms INT,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX health_checks_env_idx ON health_checks(app_env_id, checked_at DESC);

CREATE TABLE action_runs (
    id             UUID PRIMARY KEY,
    correlation_id UUID NOT NULL UNIQUE,
    requested_by   BIGINT NOT NULL REFERENCES users(id),
    app_env_id     BIGINT NOT NULL REFERENCES app_envs(id),
    action         TEXT NOT NULL,
    inputs         JSONB NOT NULL DEFAULT '{}',
    status         TEXT NOT NULL CHECK (status IN (
        'pending_approval','rejected','dispatching','queued','in_progress',
        'waiting','succeeded','failed','cancelled','unknown')),
    approved_by    BIGINT REFERENCES users(id),
    gh_run_id      BIGINT,
    gh_run_url     TEXT,
    log_ref        TEXT,
    error          TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ
);
CREATE INDEX action_runs_env_idx ON action_runs(app_env_id, created_at DESC);

CREATE TABLE audit_events (
    id         BIGSERIAL PRIMARY KEY,
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_type TEXT NOT NULL CHECK (actor_type IN ('user', 'agent', 'system')),
    actor_id   TEXT NOT NULL,
    action     TEXT NOT NULL,
    target     TEXT,
    details    JSONB NOT NULL DEFAULT '{}',
    result     TEXT NOT NULL,
    ip         TEXT
);
CREATE INDEX audit_events_at_idx ON audit_events(at DESC);

-- Append-only: block UPDATE, DELETE and TRUNCATE for every role, including the owner.
-- M4 additionally splits the app role (INSERT, SELECT only) from the migration owner.
-- +goose StatementBegin
CREATE FUNCTION audit_events_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER audit_events_no_update_delete
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_immutable();
CREATE TRIGGER audit_events_no_truncate
    BEFORE TRUNCATE ON audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION audit_events_immutable();

INSERT INTO roles (name, permissions) VALUES
 ('viewer',    ARRAY['app.view','deploy.view']),
 ('developer', ARRAY['app.view','deploy.view','logs.view','action.restart','action.migrate','action.seed']),
 ('operator',  ARRAY['app.view','deploy.view','logs.view','action.restart','action.migrate']),
 ('approver',  ARRAY['action.approve','app.view','deploy.view']),
 ('admin',     ARRAY['app.view','deploy.view','logs.view','action.restart','action.migrate','action.seed',
                      'action.approve','admin.access','admin.agents','audit.view']);

-- +goose Down
DROP TABLE audit_events, action_runs, health_checks, container_events, container_state,
    agents, app_envs, apps, role_bindings, roles, sessions, user_orgs, users;
DROP FUNCTION audit_events_immutable();
