# Helmsman

A lightweight internal developer portal. One place to see every app in every environment, tail container logs, restart containers, run migrations and seeds through GitHub Actions, and follow deploys, all behind GitHub login with per-app, per-environment, per-action access control.

Status: **M4**. GitHub login, app catalog, roles and bindings, the audit log, the agent, live status, log tailing, restarts, migrations and seeds through GitHub Actions with production approvals, and the Deployments tab all work. The production stack (`deploy/server/compose.yml`, Caddy or nginx, nightly backups with a restore drill, an SSH deploy pipeline, least-privilege database role, optional break-glass admin) and the security checklist are in place. Remaining before a pilot: your host and domain, and your real catalog.

## Requirements

Docker Engine with Compose v2, and `make`. Nothing else is installed on the host.

## Quick start

```bash
make up
```

- UI: http://localhost:5173
- API: http://localhost:8080 (`/healthz`, `/readyz`)
- Demo apps: http://localhost:9001/health and http://localhost:9002/health

`make up` also seeds the dev data and starts a real agent. The agent joins within about a minute of the first start. Sign in through the fake GitHub picker (see the runbook for the dev accounts), open **Apps**, then **Demo App**, to see status, tail logs and restart the container. The **runs** tab starts migrations and seeds against the fake GitHub, which paces a run over about 16 seconds. The **deployments** tab lists the deploy workflow runs.

Other targets: `make test`, `make down`, `make logs`, `make build`, `make e2e` (Playwright end-to-end tests), `make e2e-server` (checks of a live deployment), `make backup-test` (backup and restore drill), `make up-logs` (adds Loki and Alloy for log history), `make up-github` (runs against a real GitHub App). Run `make help` for the list.

## Layout

See the directory layout in `docs/catalog.md` and the code comments. Key parts: `cmd/server`, `cmd/agent`, `internal/*`, `pkg/protocol`, `web/`, `deploy/`, `catalog/apps.yaml`.

## Docs

- [Agent install](docs/agent-install.md)
- [Catalog reference](docs/catalog.md)
- [Log history](docs/logs.md): Loki and Alloy, nginx and browser (Faro) logs, filters
- [Mail viewer](docs/mail.md): read the emails your dev environments send, from a Mailpit, inside Helmsman
- [Server setup](docs/server-setup.md): put Helmsman on a public server and deploy it from GitHub Actions
- [Runbook](docs/runbook.md): operations, production deploy, backups, load test
- [Security checklist](docs/security.md): each control, where it lives and its test

## Conventions

Deny by default (every route declares a permission in the route table), no free-form command execution anywhere, no secrets in git (see `.env.example`), conventional commits.
