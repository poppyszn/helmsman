# Architecture

How Helmsman is built: the parts, how they talk, what is stored, and where each rule is enforced. Read this before a change that crosses packages. The security rules themselves are in [AGENTS.md](../AGENTS.md) and [security.md](security.md); how to run it is in the [runbook](runbook.md).

## The parts

```
 browser ── https ──> reverse proxy (Caddy or nginx) ──> server ──> Postgres
                                                           │
 app host ── agent ── wss (agent dials out) ───────────────┘
 app host ── agent ── Docker socket (read, logs, restart)

 server ──> GitHub (sign-in, App: dispatch and read workflow runs, README and run logs)
 server ──> Loki (optional, log history)      server ──> SMTP and webhooks (notifications)
```

| Part | What it is |
|---|---|
| **Server** (`cmd/server`) | One Go binary. Serves the API and the embedded React UI, talks to Postgres and GitHub, and accepts agent connections. Runs migrations at start. |
| **UI** (`web/`) | React, TypeScript, Vite, TanStack Query, Tailwind. Built into `web/dist` and embedded in the server binary (`web/embed.go`). |
| **Agent** (`cmd/agent`) | A small Go container on each app host. Dials out to the server over a WebSocket (it opens no inbound port), reads the Docker socket, and can only report status, stream logs, restart listed services and probe listed URLs. |
| **Postgres** | The only store. Migrations are embedded (goose) and additive. |
| **GitHub** | Identity (OAuth App: organizations and teams) and automation (GitHub App: start workflows, read runs and jobs). Never both in one registration. |
| **Optional** | Loki and Alloy for log history; SMTP and webhooks for notifications; a backup sidecar and S3 compatible bucket. |

## Who decides what

Three kinds of identity, three kinds of trust:

- **People** sign in with GitHub OAuth (state and PKCE). The server checks they belong to an allowed organization and stores their teams. A session cookie then identifies them. What they may do comes only from **role bindings** in the database (`internal/rbac`): a binding gives a role's permissions on an app and environment, to a user or an `org/team`. Bindings add up. `env: *` never matches a production environment, and site admins hold everything.
- **Agents** authenticate with a long-lived credential obtained by exchanging a single-use join token. The server trusts an agent to report on its own host, nothing more. The agent trusts the server only as far as its own `agent.yaml` allows: a service that is not listed is invisible and cannot be acted on, whatever the server asks.
- **The server** acts on GitHub as a GitHub App installation, with the narrowest permissions (Actions read and write, Contents read, Metadata read).

## How a request flows

1. The proxy passes the request to the server (WebSocket upgrades included, with the `Host` header intact).
2. The router (`internal/api/server.go`, `routes.go`) looks the route up in a single table. Each row names a permission and a scope. A middleware loads the session, checks CSRF for anything that is not a read, resolves the person's principal from their bindings, and calls `rbac` for that permission on that scope. Only then does the handler run.
3. The handler reads or writes through `internal/store`, calls the agent through `internal/agenthub`, or calls GitHub through `internal/gh`. Anything that changes state writes an audit event first or alongside (`internal/audit`).
4. The answer is JSON. Errors say what to do next in words.

Public routes are only the health probes, the OAuth handshake, the agent join and connect endpoints, and the static UI.

## Packages

| Path | Responsibility |
|---|---|
| `cmd/server`, `cmd/agent`, `cmd/breakglass-init` | Entry points. `breakglass-init` generates the emergency admin's password hash and TOTP secret. |
| `pkg/protocol` | The agent and server message types and versioning, shared by both. Additive changes only. |
| `internal/api` | HTTP handlers, middleware, the route table, rate limits, integration tests. |
| `internal/rbac` | Permissions, principals, scope evaluation. Pure logic. |
| `internal/auth` | GitHub OAuth, sessions, CSRF, the membership refresh worker, the break-glass login, TOTP. |
| `internal/catalog` | Parse and validate `apps.yaml`, keep the last good catalog, sync it to the database, hot reload. |
| `internal/store` | All SQL, and `migrations/`. Opens the pool as the limited runtime role. |
| `internal/agenthub` | Server side of the agent connection: join, credentials, the clone alarm, requests (restart, logs, health), the stream broker. |
| `internal/agent` | The agent itself: Docker socket client, allowlist, snapshots, events, logs, reconnect with backoff. |
| `internal/health` | Derive an environment's status from probes, container state and agent presence, and run the probes. |
| `internal/actions` | Migrations and seeds through GitHub Actions: requests, approval, dispatch, finding the run by correlation id, polling, deployments, repository checks. |
| `internal/gh` | GitHub App authentication and the REST client (ETags, rate limit handling). |
| `internal/notify` | Notification channels, stored deliveries with retries, email and signed webhooks, SMTP settings. |
| `internal/integrations` | Optional services configured from the UI, today Loki. Secrets encrypted at rest. |
| `internal/loki` | A small read client for Loki. Queries are built from the catalog and fixed values, never from typed text. |
| `internal/audit` | The append-only audit writer. |
| `internal/config` | Environment variables to a validated config. |
| `internal/fakegithub` | A fake GitHub (OAuth, App, Actions) for the dev stack and tests. |
| `tools/` | `fake-github`, `demo-app` (a service that logs and fails on request), `loadtest`. |
| `web/src` | `pages/`, `components/`, `lib/` (the API client and types), `components/ui/` (primitives). |
| `deploy/` | Compose files (dev, production server, agent, logs), nginx and Caddy config, backup scripts, Dockerfiles. |
| `e2e/` | Playwright tests: `tests/dev` (the dev stack) and `tests/server` (a live deployment). |

## The agent protocol

JSON envelopes over one WebSocket: `{v, type, id, ts, payload}`. The agent sends `hello`, then a `snapshot` every 30 seconds (which doubles as the heartbeat; 60 seconds of silence marks it offline), plus container `event`s. The server sends requests and the agent answers: `action.request` (restart) with `action.result`, `logs.open` with `logs.chunk` and `logs.close`, `health.check` with `health.result`. There is nothing else: no exec, no file access, no arbitrary URL. The server accepts the current version and the one before. See `pkg/protocol/protocol.go` and [agent-install.md](agent-install.md).

## Data

Tables, grouped by what they are for. Names are in `internal/store/migrations`.

- **People and access:** `users`, `user_orgs` (organizations and teams), `sessions`, `roles`, `role_bindings`.
- **Catalog and state:** `apps`, `app_envs` (the stored spec per environment), `agents`, `agent_discovered`, `container_state`, `container_events`, `health_checks`.
- **Runs:** `action_runs`, `action_run_logs` (one active run per environment is a database constraint).
- **Audit:** `audit_events`, protected by triggers that reject update, delete and truncate.
- **Settings and features:** `integrations` (Loki, SMTP; passwords encrypted), `notification_channels`, `notification_deliveries`, `notification_state`, `custom_apps`.

The server runs as `helmsman_app`, a role with no rights to change the audit log. Migrations run as the table owner, then the runtime pool is opened as the limited role.

## Background work

All of it lives in the server process and stops on shutdown.

| Worker | Every | Does |
|---|---|---|
| Catalog poller | 30 s | Reloads `apps.yaml` when it changes; keeps the last good one on error. |
| Membership refresh | 15 min | Re-reads each person's organizations and teams; removes sessions of people who left. |
| Health checker | 30 s | Probes each environment's health URL (from the server or through the agent). |
| Run tracker | 5 s | Finds dispatched runs, follows them, stores their job logs. |
| Repository watcher | 10 min | Checks the GitHub App and workflows per repository. |
| Notification worker | 5 s | Sends due deliveries with retries. |
| Notification watcher | 30 s | Announces environment, agent, deployment and workflow changes, only after two looks. |
| Pruning | 6 h | Deletes old health checks, container events and run logs. |

## Where the rules live

| Rule | Enforced in | Proved by |
|---|---|---|
| Every route has a permission and scope | `routes.go` registration (panics otherwise) | a generated test that calls every route without a session and with too little access |
| Production is explicit | `rbac.Allowed` | the RBAC matrix test |
| Nothing runs arbitrary commands | `pkg/protocol` has no such message; the agent allowlist | agent tests |
| Audit is append-only | database triggers | the store tests and the restore drill |
| Secrets stay secret | encrypted columns, never returned by the API | API tests that read back and check |

The full checklist, with each control and its test, is [security.md](security.md).
