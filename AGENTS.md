# AGENTS.md: Helmsman Architecture & Agent Guide

> **Single source of truth for AI agents (Antigravity, Gemini, Claude, Cursor, Copilot)** working in the `helmsman` repository.
> Consult this document before planning, modifying, or creating code, migrations, or configurations.

---

## 1. Project Overview

**Helmsman** is a lightweight internal developer portal (IDP). It provides a unified control plane for engineering teams to:
- Inspect all applications, environments, and container statuses across hosts.
- Tail live container logs via WebSockets with automatic permission re-checking.
- Safely trigger container restarts with audit tracking and concurrency protection.
- Trigger migrations, seeds, and deployments via GitHub Actions (`workflow_dispatch`).
- Enforce strict Role-Based Access Control (RBAC) linked to GitHub Organizations and Teams.
- Maintain an append-only, tamper-proof audit log for every administrative and operational action.

### Current state

The milestones M0 to M4 are built: the dev stack and CI, GitHub login, catalog and RBAC, the agent with status, logs and restarts, GitHub Actions runs, and the production stack with backups. Since then: log history (Loki, Alloy, Faro), the Integrations page, the deployments view with steps and logs, notifications (email and webhooks), a setup check page, agent install files and container discovery, access requests, tags, organization switching and external tools on the Apps page, a per-environment timeline, and optional second-person approval. What is next is in [docs/roadmap.md](docs/roadmap.md); the map of the code is [docs/architecture.md](docs/architecture.md); how to work on it is [CONTRIBUTING.md](CONTRIBUTING.md). Keep those three in step with this file.

---

## 2. Hard Security & Architectural Rules

AI agents must strictly uphold these inviolable constraints:

1. **Deny by Default in HTTP Routes**:
   - Every route in `internal/api/routes.go` MUST declare an explicit `Permission` and `ScopeKind`.
   - The route table enforces this at registration; any route missing a permission will panic and refuse to mount.
   - Public routes are restricted solely to health probes (`/healthz`, `/readyz`), the OAuth handshake, and agent join/connect.

2. **No Arbitrary Remote Code Execution**:
   - The agent container dials out to the IDP; it opens **zero inbound ports**.
   - The agent protocol (`pkg/protocol`) only supports fixed, predefined message types (`restart`, `logs`, `health.check`). No free-form shell commands, arbitrary exec calls, or dynamic scripts are permitted.

3. **Append-Only Audit Trail**:
   - The `audit_events` table is protected by PostgreSQL database triggers that raise exceptions on any `UPDATE`, `DELETE`, or `TRUNCATE`.
   - Every state-altering action (login, logout, catalog reload, agent registration, revocation, token generation, container restart, log stream opening) must write to `audit.Writer`.
   - Opening a log stream is audited; actual log payloads are never written to the audit log.

4. **Production Tier Protection (`tier: prod`)**:
   - Role bindings with wildcard environments (`env: *`) **NEVER** match `tier: prod` environments. Access to production requires an explicit binding where `env == "prod"` (or superadmin status `is_admin = true`).
   - Seeding production is never granted by a role or binding: `action.seed` on a `tier: prod` environment is held only by site admins (`rbac.Allowed`), so a developer can migrate production but not seed it. Even for a site admin the environment's `allow_seed` still decides.
   - Environments configured with `allow_seed: false` (standard for `prod`) must reject seed requests regardless of user role, even for site admins.
   - Restarts in production require the user to explicitly type the environment name in the UI.

5. **Agent Security & Single-Use Join Tokens**:
   - Agents register using single-use join tokens that expire after 1 hour (`JoinTokenTTL`). Tokens are stored as SHA-256 hashes.
   - On join, the agent exchanges the join token for a persistent credential stored in `/data/credential` on the agent volume.
   - **Anti-cloning protection**: Each running agent sends a random `InstanceID` on connection. If a second live agent connects using an existing credential, the hub rejects the second connection, logs an alert (`alert_at`, `alert`), and marks the agent in the admin UI.

6. **Agent Local Allowlist**:
   - Agents enforce their own local configuration (`agent.yaml`). Even if requested by the server, an agent will reject operations on compose projects, services, or health check URLs not explicitly listed in its `allow` config.

---

## 3. Technology Stack

### Backend
- **Language**: Go 1.26+ (standard library emphasized).
- **HTTP Framework**: Chi v5 (`github.com/go-chi/chi/v5`).
- **Database Access**: `github.com/jackc/pgx/v5` with connection pooling (`pgxpool`).
- **Migrations**: `github.com/pressly/goose/v3` embedded in `internal/store/migrations/`.
- **WebSockets**: `github.com/coder/websocket` for low-overhead, standards-compliant WebSockets.
- **Logging**: Standard library `log/slog` outputting structured JSON.

### Frontend
- **Framework**: React 18+ with TypeScript, bundled with Vite.
- **Routing**: React Router DOM (`react-router-dom`).
- **State & Data Fetching**: TanStack Query (`@tanstack/react-query`).
- **Styling**: Tailwind CSS, PostCSS, Lucide React icons.
- **UI Components**: Radix UI primitives (`@radix-ui/*`) styled in the `web/src/components/ui/` directory.
- **Embedded Delivery**: In production, `web/embed.go` embeds `web/dist` directly into the Go server binary.

### Agent & Host Integration
- **Docker Client**: Custom minimal client (`internal/agent/docker.go`) communicating with Unix domain socket `/var/run/docker.sock`.
- **Protocol**: Defined in `pkg/protocol/protocol.go`. Envelope-based JSON protocol over WebSocket with semantic versioning.

### Testing & Tooling
- **Local Dev Stack**: Docker Compose (`deploy/compose.dev.yml`).
- **Mock Services**:
  - `tools/fake-github`: Local GitHub OAuth and API server on `:8090`.
  - `tools/demo-app`: Dummy targets on `:9001` and `:9002` simulating containerized user apps.

---

## 4. Directory & Package Layout

```
.
├── .github/                    # CI and publish workflows, the PR template, issue templates
├── catalog/apps.yaml           # The dev catalog (the production catalog is deploy/server/apps.yaml)
├── cmd/
│   ├── agent/                  # The host agent
│   ├── breakglass-init/        # Generates the emergency admin's password hash and TOTP secret
│   └── server/                 # The API and web portal
├── deploy/                     # Compose files (dev, server, agent, logs), proxy config, backups, Dockerfiles
├── docs/                       # See docs/README.md for the index
├── e2e/                        # Playwright tests (tests/dev and tests/server)
├── examples/                   # agent.yaml, workflows, log history samples
├── internal/
│   ├── actions/                # Migrations and seeds through GitHub Actions, deployments, repository checks
│   ├── agent/                  # The agent: Docker socket, allowlist, logs, reconnect
│   ├── agenthub/               # Server side of the agent connection
│   ├── api/                    # Handlers, middleware, the route table, integration tests
│   ├── audit/                  # Append-only audit writer
│   ├── auth/                   # GitHub OAuth, sessions, CSRF, break-glass login
│   ├── catalog/                # Parse, validate and sync apps.yaml
│   ├── config/                 # Environment configuration
│   ├── fakegithub/             # Fake GitHub for the dev stack and tests
│   ├── gh/                     # GitHub App authentication and REST client
│   ├── health/                 # Status derivation and probes
│   ├── integrations/           # Optional services set up from the UI (Loki)
│   ├── loki/                   # Read client for Loki
│   ├── notify/                 # Notification channels, deliveries, email, webhooks, SMTP
│   ├── rbac/                   # Principals, permissions, scope evaluation
│   └── store/                  # All SQL; migrations/ holds the goose migrations
├── pkg/protocol/               # Agent and server messages
├── tools/                      # demo-app, fake-github, loadtest
└── web/src/                    # components/, lib/ (API client and types), pages/
```

---

## 5. Development & Testing Commands

All development dependencies run in Docker. The host only requires `make` and `docker`.

| Task | Command | Description |
|---|---|---|
| **Start Dev Stack** | `make up` | Starts Postgres, server, web UI, agent, fake-github, demo-apps, and seeds test data. |
| **Stop Dev Stack** | `make down` | Stops compose containers (preserves database volumes). |
| **Reset Database** | `docker compose -f deploy/compose.dev.yml down -v` | Completely resets Postgres data. |
| **View Dev Logs** | `make logs` | Streams logs across all compose services. |
| **Seed Dev DB** | `make seed-dev` | Idempotently inserts dev agent, test users, and role bindings. |
| **Run Test Suite** | `make test` | Starts throwaway `postgres-test`, runs `go vet` and `go test`, followed by web lint and typecheck. |
| **Build Images** | `make build` | Builds `helmsman-server:dev` and `helmsman-agent:dev` Docker images. |
| **Build Frontend** | `make web-build` | Runs `npm ci && npm run lint && npm run build` into `web/dist`. |

### Dev Stack Accounts (Pre-seeded via `make seed-dev`)
Login at http://localhost:5173 using the fake GitHub OAuth picker:
- `alice` (id: 1001, org: `acme-org`): Site Administrator (bootstrap admin).
- `bob` (id: 1002, org: `acme-org/backend`): Developer across all non-production environments (`env: *`).
- `carol` (id: 1003, org: `acme-org`): Viewer on `demo-app` in `staging` only.
- `erin` (id: 1005, org: `acme-org/leads`): Approver across all non-production environments.
- `grace` (id: 1007, org: `acme-org`): No role. Signs in and sees "You don't have access yet"; she appears under Awaiting access on the Access page.
- `dave` (rejected): User from `other-org` (not in `IDP_ALLOWED_ORGS`).
- `frank` (rejected): Belongs to an org that blocks third-party OAuth apps.

---

## 6. Key Subsystems & Design Patterns

### A. RBAC & Principal Resolution (`internal/rbac`)
- Permissions: `app.view`, `deploy.view`, `logs.view`, `action.restart`, `action.migrate`, `action.seed`, `action.approve`, `admin.access`, `admin.agents`, `audit.view`.
- Subjects can be `user` (`subject: user_id`) or `team` (`subject: org/team_slug`).
- A user's principal is resolved by taking the union of direct bindings and bindings belonging to their GitHub teams.
- Admins (`users.is_admin = true`) bypass RBAC checks and hold all permissions across all environments.

### B. Catalog Management (`internal/catalog`)
- Loaded from `catalog/apps.yaml` on startup, reloaded on change (30s poller) or via admin button `/api/admin/catalog/reload`.
- Validation is **all-or-nothing**: syntax errors, unknown fields, duplicate app/env IDs, or referencing unregistered agents will reject the reload.
- **Fail-safe**: The last known good catalog stays active in memory and in the database. An invalid file never crashes the server.
- Removed apps/envs are soft-deleted (`deleted_at IS NOT NULL`).

### C. Agent Communication & WebSocket Protocol (`internal/agenthub`, `pkg/protocol`)
- The agent initiates connection to `/agent/ws` passing its bearer credential in the `Authorization` header.
- Protocol envelope structure:
  ```json
  {
    "v": 1,
    "type": "hello | snapshot | event | logs.open | logs.chunk | logs.close | health.check | health.result | action.request | action.result | error",
    "id": "<random-hex>",
    "ts": "2026-11-11T12:00:00Z",
    "payload": { ... }
  }
  ```
- Compatibility: Server accepts protocol `Version` and `Version - 1`. Future protocol changes must be purely additive.
- Silence detection: Agents that send no messages for 60 seconds are marked offline (`OfflineAfter`). The UI shows status `unknown` and disables actions with a clear explanation.

### D. Health & Status Derivation (`internal/health`)
- Health checks probe endpoints every 30 seconds (5s timeout).
- Probes can run `via: server` (IDP host dials public URL) or `via: agent` (agent dials host-local/private URL).
- Status computation (`Derive`):
  - `healthy`: Container running and recent probe returned HTTP < 400 within 2s.
  - `degraded`: Health check latency > 2s (`ProbeSlow`) or Docker reports container unhealthy.
  - `down`: Two consecutive probe failures or container is exited/stopped.
  - `unknown`: Agent offline and no fresh probe within 90s.
- Crash loop detection: Tagged if the container exited 3 or more times within the last 10 minutes.

### E. Container Restart & Concurrency
- REST call: `POST /api/apps/{app}/envs/{env}/restart`.
- Concurrency protection: `inflight` lock ensures multiple clicks or tabs cannot initiate concurrent restarts on the same environment.
- Audited twice: Once when the restart is requested, and once with the final execution result from the agent.

### F. Frontend Design System & UI Architecture (`docs/design-system.md`)
- Helmsman strictly follows the **Vercel Geist Minimalist** design aesthetic: pure black canvas (`#000000`), flat tonal surface elevation (`#0a0a0d`, `#121215`, `#141418`) instead of noisy boxy borders, and high-contrast typography.
- **Inviolable UI Rules**:
  - **Zero Emojis**: Always use `lucide-react` icons with `strokeWidth={1.5}` or `2`; emojis are strictly prohibited in the UI.
  - **No Border Clutter**: Avoid wrapping sections or cards in `1px` border lines. Rely on background tonal contrast.
  - **Typography Floor**: No sub-12px microtext (`text-[10px]`, `text-[11px]` are forbidden). Base body is 14-15px; telemetry metrics are 24px bold monospace.
  - **Single Calm Status Indicators**: Use `StatusBadge` (8px glowing dot) rather than repetitive, flashing "LIVE" markers.
  - **Explicit Action Icons**: Toggle buttons must show the action they will perform (e.g. Pause icon when streaming, Play icon when paused to resume).
- Refer to `docs/design-system.md` for full component specifications, surface elevation tokens, and quality gates (`cd web && npm run typecheck && npm run lint`).

---

### G. Notifications (`internal/notify`)
- Channels are email recipients or signed webhooks. Each picks events and may be limited to apps, environment names and production or non-production.
- Events are stored first (`notification_deliveries`) and sent by a worker with retries; a watcher in `internal/api/notifywatch.go` announces state changes only after two looks, records first sightings silently, and holds back flapping.
- Webhook addresses are validated on save and again when the connection is made (no metadata or link-local addresses, no redirects). SMTP and signing secrets are encrypted at rest and never returned.
- Payloads never contain log text or secrets.

### H. Access requests and first sign-in
- Someone in an allowed organization who has no role binding signs in and sees "You don't have access yet" (`/api/me` reports `has_access: false`). They are listed under Access > Awaiting access for an admin to grant a role; granting uses the ordinary bindings API.

### I. Apps page features
- Catalog `tags` (app and environment) feed the tag filter; an app's organization is the owner of its repo; external tools are rows in `custom_apps`, managed by admins and visible to anyone who can see apps.

## 7. Guidelines for GitHub workflow runs

When implementing or extending GitHub Action runs:
1. **GitHub Integration**:
   - Utilize GitHub App authentication or OAuth token to invoke repository workflows (`workflow_dispatch`).
   - Workflows must accept inputs: `environment`, `command` (`migrate` | `seed`), `correlation_id`, `requested_by`.
2. **Action Run Tracking**:
   - Record records in `action_runs` table (`id`, `correlation_id`, `requested_by`, `app_env_id`, `action`, `status`, `gh_run_id`, `gh_run_url`).
   - Supported statuses: `pending_approval`, `rejected`, `dispatching`, `queued`, `in_progress`, `waiting`, `succeeded`, `failed`, `cancelled`, `unknown`.
3. **Approval Flow**:
   - Approval is opt in per environment: with `require_approval: true` in the catalog a run waits for `action.approve` from a second person before dispatching. Off by default; the permission on the environment, `allow_seed` and the audit entry apply either way.
4. **Safety Enforcements**:
   - Verify `allow_seed` flag before dispatching seeds.
   - Enforce concurrency groups so multiple migrations do not run simultaneously on the same environment.
5. **Auditing**:
   - Every dispatch, approval, rejection, and completion must generate an immutable audit log entry.
