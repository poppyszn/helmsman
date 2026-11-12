-- +goose Up
-- Container discovery: an agent that opts in (discovery: true in agent.yaml) reports the compose service
-- names it can see but does not expose, so the portal can offer to set them up. Names, image and state
-- only. Replaced on every snapshot.
CREATE TABLE agent_discovered (
    agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    compose_project TEXT NOT NULL,
    service         TEXT NOT NULL,
    image           TEXT NOT NULL DEFAULT '',
    state           TEXT NOT NULL DEFAULT '',
    seen_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, compose_project, service)
);

-- +goose Down
DROP TABLE agent_discovered;
