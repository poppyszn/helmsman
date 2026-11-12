# Helmsman install kit

Everything you need to install [Helmsman](#what-is-helmsman) on a server and to add hosts to it: compose files, configuration examples, a reverse proxy example, and an installer for the agent. It holds no application source code and no secrets.

## What is Helmsman

A small internal developer portal. It shows the status of your apps and environments, tails container logs live, restarts containers, runs migrations and seeds through GitHub Actions, and controls who can do what with GitHub organization and team membership. Every action is audited. A tiny agent on each app server reads the Docker socket and connects out to the portal, so app servers open no inbound ports.

## Add a host (the agent)

On the host that runs your apps, with Docker and Docker Compose v2:

1. In the portal, open **Agents**, create an agent, and keep the **join token** (single use, valid for one hour). The portal also shows the same files this installer writes.
2. Run the installer from this repository:

   ```bash
   git clone https://github.com/OWNER/helmsman-install.git
   cd helmsman-install/agent
   sudo ./install.sh
   ```

   It asks for the portal address and the token (typed without echo; never printed), writes `/opt/helmsman-agent/{agent.yaml,compose.agent.yml,.env}`, starts the agent and waits for it to join. The token is removed from `.env` once the agent has joined.
3. In the portal, **Agents > Containers** lists what runs on the host. Tick the services to expose and it drafts the `allow:` entries for `agent.yaml` and the catalog entries.

Non-interactive: `HELMSMAN_URL=https://portal.example.com HELMSMAN_JOIN_TOKEN=... sudo -E ./install.sh`. More in [docs/agent.md](docs/agent.md).

## Run the portal (the server)

The server runs from one compose file with Postgres, TLS and a backup sidecar. Follow [docs/server-install.md](docs/server-install.md): DNS, two GitHub registrations (an OAuth App for sign-in, a GitHub App for migrations and seeds), a `.env`, and one `docker compose up`. A reverse proxy example for nginx is in `deploy/nginx/`; Caddy is bundled.

## What is in here

```
agent/                 install.sh, compose.agent.yml, agent.example.yaml, env.example
deploy/server/         compose.yml, env.example, preflight.sh, apps.yaml, caddy/, db/
deploy/nginx/          reverse proxy example for a server that already runs nginx
deploy/loki, alloy/    optional log history (Loki and Grafana Alloy) configuration
deploy/backup/         nightly database backup sidecar
docs/                  server and agent guides
SOURCE_COMMIT          the Helmsman commit these files were generated from
```

## Images

| Image | What | Visibility |
|---|---|---|
| `ghcr.io/poppyszn/helmsman-agent` | the agent | public: no login needed |
| `ghcr.io/poppyszn/helmsman-server` | the portal | pulled with a registry login unless made public |

If an image is private, `docker login ghcr.io` with a token that has `read:packages`, pull, then `docker logout ghcr.io`.

## Security notes

- This repository contains no credentials. Keep your `.env` files, GitHub App key and join tokens out of any repository.
- The agent only enforces what is listed in its own `agent.yaml`, and the portal cannot make it run commands: it can report container status, stream logs and restart listed services, nothing else.
- Report security problems privately to the repository owner rather than in a public issue.

These files are generated from the Helmsman project. Edits are welcome as issues, but they are overwritten when the kit is regenerated.
