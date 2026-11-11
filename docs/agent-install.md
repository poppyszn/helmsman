# Agent install

The agent is a small Go container that runs on each app server. It dials out to the IDP over WSS, reads the local Docker socket, and can do three things: report container status, stream logs, and restart a container. It cannot run commands, and it opens no inbound ports.

## 1. Create the agent in the UI

1. Sign in as an administrator and open **Agents**.
2. Enter an id (for example `billing-host-1`) and a name, then **Create agent**.
3. Copy the join token. It is shown once and is valid for 1 hour. If it expires before the agent joins, use **New join token** on the same row.

The id is what `catalog/apps.yaml` refers to in `agent:`. See [catalog.md](catalog.md).

## 2. Write `agent.yaml`

Start from [examples/agent.yaml](../examples/agent.yaml). List only the compose projects and services the portal should see.

```yaml
idp_url: wss://idp.example.com/agent/ws
allow:
  - compose_project: billing
    services: [api, worker]
    actions: [logs, restart]
    health_urls:
      - http://api:8080/health
```

Rules the agent enforces itself, whatever the server asks:

- A service that is not listed is invisible: it never appears in snapshots and its logs cannot be read.
- `actions` is a subset of `logs` and `restart`. An empty or missing list allows nothing.
- `health_urls` must match the catalog's `health.url` exactly for checks with `via: agent`.
- An empty `allow` list means the agent reports nothing and refuses everything.
- `idp_url` must be `wss://`. The agent refuses `ws://` unless `IDP_ALLOW_INSECURE=true` is set, which is for local development only.

## 3. Run it

Copy [deploy/compose.agent.yml](../deploy/compose.agent.yml) to the server, next to `agent.yaml`, and set these in `.env`:

```bash
AGENT_VERSION=<release tag>
IDP_JOIN_TOKEN=<token from step 1>
DOCKER_GID=$(getent group docker | cut -d: -f3)
```

```bash
docker compose -f compose.agent.yml up -d
docker compose -f compose.agent.yml logs -f idp-agent
```

You should see `joined` and then `connected`. The agent stores its long-lived credential in the `agent-data` volume and ignores the join token from then on. You can remove `IDP_JOIN_TOKEN` from `.env` after the first successful start.

The container runs as a non-root user with the Docker group added (`group_add`). The Docker socket is powerful, which is why the agent only exposes the fixed set of calls above and enforces its own allowlist.

Resource use is small: the limits in the compose file are `mem_limit: 64m` and `cpus: 0.25`, and a running agent stays well under 40 MB of resident memory.

## 4. Point the catalog at it

```yaml
environments:
  - env: staging
    agent: billing-host-1
    container: { compose_project: billing, service: api }
```

Containers are matched by compose labels, never by container id, so a redeploy that recreates the container keeps working.

## Container settings on the app side

Set log rotation on your app containers, or Docker keeps logs without limit:

```yaml
services:
  api:
    logging: { driver: json-file, options: { max-size: "10m", max-file: "3" } }
```

Optionally label images with the git commit so the portal can show the deployed revision:

```dockerfile
ARG GIT_SHA
LABEL org.opencontainers.image.revision=$GIT_SHA
```

Logs are read through Docker, so the container must use the `json-file`, `local` or `journald` logging driver. With `awslogs`, `none` or similar, the portal says logs are not available through Docker.

## How the connection behaves

| Situation | What happens |
| --- | --- |
| Server restarts or the network drops | The agent reconnects with exponential backoff and jitter, 1 s to 60 s. |
| Agent silent for 60 s | The environment shows as `unknown` and log and restart actions are disabled with a reason. |
| Docker daemon down or socket missing | The agent stays connected and reports Docker as unavailable. It retries every 5 s. |
| Container recreated by a deploy | Matched by labels. Open log streams reattach and show a marker line. |
| Credential copied to a second host (cloned image) | The second connection is rejected and an alert appears on the Agents page. Create a separate agent per host. |
| Agent revoked in the UI | The socket closes at once and the credential stops working. |
| Credential rejected and `IDP_JOIN_TOKEN` still set | The agent tries the join token again. Join tokens are single use and expire after 1 hour. |

## Upgrading

Pull the new image and recreate the container. The server accepts the current and the previous protocol version, and the Agents page flags agents that are behind.

## Local development

`make up` starts an agent as part of the dev stack, with a fixed dev join token registered by `make seed-dev`. It talks to the server over `ws://` on the compose network, which is why the dev compose file sets `IDP_ALLOW_INSECURE=true`. Do not copy that setting to a real host.
