-- +goose Up
-- Additive only: M2 agent state. Nothing from 00001 is changed.
ALTER TABLE agents
    ADD COLUMN protocol_version INT,
    ADD COLUMN docker_version   TEXT,
    ADD COLUMN allowlist_digest TEXT,
    ADD COLUMN docker_ok        BOOLEAN,
    ADD COLUMN alert            TEXT,
    ADD COLUMN alert_at         TIMESTAMPTZ;
CREATE UNIQUE INDEX agents_credential_idx ON agents(credential_hash) WHERE credential_hash IS NOT NULL;
CREATE UNIQUE INDEX agents_join_idx ON agents(join_token_hash) WHERE join_token_hash IS NOT NULL;

-- at is server receive time and drives ordering; agent_ts is what the agent clock said.
ALTER TABLE container_events ADD COLUMN agent_ts TIMESTAMPTZ, ADD COLUMN detail TEXT;
CREATE INDEX container_events_container_idx ON container_events(agent_id, container, at DESC);

-- +goose Down
DROP INDEX container_events_container_idx;
ALTER TABLE container_events DROP COLUMN agent_ts, DROP COLUMN detail;
DROP INDEX agents_join_idx;
DROP INDEX agents_credential_idx;
ALTER TABLE agents DROP COLUMN protocol_version, DROP COLUMN docker_version, DROP COLUMN allowlist_digest,
    DROP COLUMN docker_ok, DROP COLUMN alert, DROP COLUMN alert_at;
