# The agent

A small container that runs on each app server. It connects out to the portal over WSS, reads the local Docker socket, and can do three things: report container status, stream logs, and restart a container. It cannot run commands and opens no inbound ports.

## Install

See the [README](../README.md#add-a-host-the-agent). In short: create an agent in the portal, run `agent/install.sh` on the host with the join token.

## Files on the host (`/opt/helmsman-agent`)

- `agent.yaml`: what the agent may expose. Nothing is exposed until you list it.
- `compose.agent.yml`: the agent's container.
- `.env`: `AGENT_VERSION`, `DOCKER_GID`, and `IDP_JOIN_TOKEN` until the agent has joined.

```yaml
# agent.yaml
idp_url: wss://portal.example.com/agent/ws
discovery: true              # report names (not logs) of services that are not exposed, so the portal can draft entries
allow:
  - compose_project: billing   # the compose project label, exactly
    services: [api, worker]    # the service labels
    actions: [logs, restart]   # nothing else exists
    health_urls:               # exact URLs the portal may ask this agent to probe
      - http://api:8080/health
```

Find the project and service names with:

```bash
docker ps --format '{{.Names}}  {{.Label "com.docker.compose.project"}}  {{.Label "com.docker.compose.service"}}'
```

After changing `agent.yaml`, recreate the agent: `docker compose -f compose.agent.yml --env-file .env up -d --force-recreate`.

## Rules the agent enforces itself

- A service that is not listed is invisible: it never appears in snapshots and its logs cannot be read, whatever the portal asks.
- `actions` is a subset of `logs` and `restart`. An empty list allows nothing.
- With `discovery: true` it reports only the names, image and state of services it does not expose (at most 200). Turn it off with `discovery: false`.
- A join token works once. A second agent using the same credential is rejected and flagged in the portal.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `could not pull the agent image` | The image is private: `docker login ghcr.io`, pull, then `docker logout ghcr.io`. |
| Never joins, logs a join error | The token expired or was used. Create a new agent (or a new token) in the portal. |
| Joins, then keeps disconnecting | The portal's reverse proxy must pass WebSocket upgrades on `/agent/ws`. |
| `permission denied` on the Docker socket | `DOCKER_GID` in `.env` must be the group of `/var/run/docker.sock`: `stat -c %g /var/run/docker.sock`. |
| A service does not appear | Its compose project and service names must match the labels exactly, and be listed under `allow`. |
