# Helmsman

A lightweight internal developer portal. One place to see every app in every environment, tail container logs, restart containers, run migrations and seeds through GitHub Actions, and follow deploys, all behind GitHub login with per-app, per-environment, per-action access control.

Status: **M0 foundation**. The UI shell, API skeleton, database schema and dev stack run. Login, catalog, agent and GitHub runs arrive in M1 to M3.

## Requirements

Docker Engine with Compose v2, and `make`. Nothing else is installed on the host.

## Quick start

```bash
make up
```

- UI: http://localhost:5173
- API: http://localhost:8080 (`/healthz`, `/readyz`)
- Demo apps: http://localhost:9001/health and http://localhost:9002/health

Other targets: `make test`, `make down`, `make logs`, `make build`. Run `make help` for the list.

## Layout

See the directory layout in `docs/catalog.md` and the code comments. Key parts: `cmd/server`, `cmd/agent`, `internal/*`, `pkg/protocol`, `web/`, `deploy/`, `catalog/apps.yaml`.

## Docs

- [Agent install](docs/agent-install.md)
- [Catalog reference](docs/catalog.md)
- [Runbook](docs/runbook.md)

## Conventions

Deny by default (every route declares a permission in the route table), no free-form command execution anywhere, no secrets in git (see `.env.example`), conventional commits.
