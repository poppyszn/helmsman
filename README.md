# Helmsman

A lightweight internal developer portal. One place to see every app in every environment, tail container logs, restart containers, run migrations and seeds through GitHub Actions, and follow deploys, all behind GitHub login with per-app, per-environment, per-action access control and an append-only audit log.

Helmsman does not build or deploy your apps and does not replace your pipelines. It sits next to what you already run: a small agent on each app host reports what is running, and your own GitHub Actions workflows do the deploying.

## What it does

- **Apps and environments.** A catalog of apps (`apps.yaml`, reviewed in git) with live status per environment: healthy, degraded, down, or unknown. Filter by tag, organization or tier; keep links to your other tools next to the apps. Every view has its own address (`/apps/<app>/<environment>/<tab>`), so a link lands on the same place.
- **Logs.** Live tail of any container, with JSON, key=value, nginx and plain lines laid out for reading and a level filter. Optional history for 14 days from Loki, including browser logs from Grafana Faro.
- **Restarts.** One click, with an audit trail, a type-the-name guard for production, and a lock against double clicks.
- **Migrations and seeds.** Started through `workflow_dispatch` on your repository's own workflow, tracked to completion with job logs. Optional second-person approval per environment.
- **Deployments.** Recent runs of each app's deploy workflow with their jobs, steps and logs, and a warning when what runs is not the latest successful deploy. A timeline per environment merges deploys, runs, restarts and health changes.
- **Access.** GitHub organizations and teams, mapped to roles per app and environment. Newcomers who sign in without a role appear for an admin to grant, with the role's permissions shown.
- **Notifications.** Email and signed webhooks for failures, outages, crash loops, offline hosts, deployments and other workflows, limited per channel to the apps, environments and events you choose.
- **Adding hosts and apps.** Creating an agent shows the files to put on the host. With discovery on, the portal lists what runs there and drafts the entries to add.
- **Operating it.** A setup check page, a production stack with backups and a restore drill, a least-privilege database role, and an optional break-glass admin.

What is planned is in the [roadmap](docs/roadmap.md).

## Quick start

You need Docker Engine with Compose v2 and `make`. Nothing else is installed on the host.

```bash
make up
```

- UI: http://localhost:5173
- API: http://localhost:8080 (`/healthz`, `/readyz`)
- Demo apps: http://localhost:9001/health and http://localhost:9002/health

`make up` seeds the dev data and starts a real agent, which joins within about a minute. Sign in through the fake GitHub picker (the accounts are in the [runbook](docs/runbook.md#dev-stack-accounts)), open **Apps**, then **Demo App**: status, live logs, restart, runs, deployments and the timeline all work against the fake GitHub. Run `make help` for every target.

## Running it for real

Follow the [server setup guide](docs/server-setup.md): a server with a public address, two GitHub registrations (an OAuth App for sign-in, a GitHub App for runs), one `.env`, one `docker compose up`. Then [add hosts and apps](docs/server-setup.md#8-connect-an-app).

## Documentation

Start with the [docs index](docs/README.md). The most used pages:

| Page | For |
|---|---|
| [Server setup](docs/server-setup.md) | Putting Helmsman on a server and connecting your first app |
| [Agent install](docs/agent-install.md) | Adding a host |
| [Catalog reference](docs/catalog.md) | Every field of `apps.yaml` |
| [Notifications](docs/notifications.md) | Email and webhook alerts |
| [Log history](docs/logs.md) | Loki, Alloy and browser logs |
| [Runbook](docs/runbook.md) | Operating it: access, runs, backups, restores |
| [Security checklist](docs/security.md) | Each control, where it lives and its test |
| [Architecture](docs/architecture.md) | How it is built |
| [Roadmap](docs/roadmap.md) | What comes next |

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md): how to set up, the shape of a change, the rules that do not bend, recipes for adding a route, migration, catalog field or page, and the docs style. [AGENTS.md](AGENTS.md) is the same constraints written for AI coding agents.

## Layout

```
cmd/            server, agent, breakglass-init
internal/       api, rbac, auth, catalog, store (+ migrations), agenthub, agent, health, actions, gh, notify, ...
pkg/protocol/   agent and server messages
web/            the React UI
deploy/         compose files (dev, server, agent, logs), proxy config, backups
e2e/            Playwright tests
docs/           everything above, and more
```

The longer map is in [docs/architecture.md](docs/architecture.md).
