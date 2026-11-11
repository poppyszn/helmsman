# Runbook

## Dev stack

- Start: `make up`. Stop: `make down`. Logs: `make logs`.
- Reset the dev database: `docker compose -f deploy/compose.dev.yml down -v` (deletes dev data).

## Sign-in and access

- Sign-in is GitHub OAuth (state and PKCE). The user must belong to an org in `IDP_ALLOWED_ORGS`. Membership is re-checked every 15 minutes; users who left lose all sessions. A GitHub outage never revokes existing sessions, it only blocks new sign-ins.
- If an org restricts third-party OAuth apps, the sign-in page names it. An owner of that org must approve the Helmsman OAuth app.
- First admin: put your numeric GitHub id in `IDP_BOOTSTRAP_ADMIN_GITHUB_IDS` before your first sign-in (it applies on first login only). After that, admins grant roles under **Access**.
- Roles are bound to users or `org/team` subjects per app and env. Bindings union. `env: *` never covers prod.
- Break-glass local admin (`IDP_BREAKGLASS_ENABLED`) is not built yet.

## Dev stack accounts

`make up` runs `make seed-dev`, which registers the dev agent and pre-binds users. Sign in through fake GitHub at http://localhost:5173:

| User | Org | Access |
| --- | --- | --- |
| alice | acme-org | admin (bootstrap) |
| bob | acme-org/backend | developer on every nonprod env |
| carol | acme-org | viewer on demo-app staging |
| erin | acme-org/leads | approver (nonprod) |
| dave | other-org | rejected: not in an allowed org |
| frank | acme-org (restricted) | rejected: org blocks OAuth apps |

## Agents, status and logs

- **Add a server:** Agents > Create agent, then follow [agent-install.md](agent-install.md). The join token is shown once and lasts 1 hour; **New join token** issues another for an agent that has not joined.
- **Status** is derived: `healthy`, `degraded` (a health check over 2 s, or Docker reports the container unhealthy), `down` (two failed checks in a row, or the container is not running), `unknown` (agent offline and no fresh health check). A single failed check never flips an environment to `down`.
- **Health checks** run every 30 s with a 5 s timeout, from the server (`via: server`) or through the agent (`via: agent`, for private URLs). `via: agent` needs the exact URL under `health_urls` in the agent's `agent.yaml`.
- **Agent offline:** after 60 s without a message the environment shows `unknown`, the last-seen time is displayed, and log tailing and restart are disabled with the reason. Check the agent container on that host (`docker compose logs idp-agent`) and the network path to the IDP.
- **Crash loop badge:** three or more container exits in 10 minutes. Read the logs before restarting again.
- **Shared credential alert:** a second agent process connected with a credential that is already in use (a cloned image or copied volume). The second one is rejected. Create a separate agent for each host, then dismiss the alert.
- **Revoke an agent:** Agents > Revoke. The socket closes at once. Environments that use it show `unknown`. To replace it, create a new agent and update the catalog.
- **Logs:** history is "since container start" because the agent reads `docker logs`. An open stream re-checks the viewer's access every 60 s and closes if access was removed. Opening a stream is written to the audit log (who, which env); log text never is.
- **Restart:** needs `action.restart` on the env. It is audited twice, when requested and with the result. Production asks you to type the environment name.
- **Retention:** container events and health checks are kept for 14 days.

## Database

Migrations run automatically at server start (goose, embedded). Never edit an applied migration; add a new one. `audit_events` is append-only: UPDATE, DELETE and TRUNCATE are rejected by trigger.

Backup, restore and production operations are written in M4.

## Migrations, seeds and deployments (GitHub Actions)

Helmsman starts the catalog's `workflows.migrate` workflow with `workflow_dispatch`. A seed runs the same workflow with `command: seed`. The inputs are fixed: `environment` (the catalog's `github_environment`), `command`, `correlation_id` and `requested_by`. Nothing else can be passed.

### How a run goes

1. A person with `action.migrate` or `action.seed` on the environment starts it from the **runs** tab. Production tiers create a request instead and wait for approval.
2. The ref is the tag that points at the running image revision. If no tag matches, or the revision is unknown, the default branch is used and the run shows a warning.
3. GitHub answers a dispatch with no run id, so Helmsman finds the run by the correlation id, which the workflow must put in its `run-name`. See `examples/workflows/migrate.yml`. It checks every 3 seconds for 60 seconds. After that the run becomes `unknown` and links to the workflow.
4. The run and its jobs are polled every 5 seconds. The job logs are stored when the run ends, up to 5 MB per run, keeping the end of each log.
5. `waiting` means GitHub is holding the job for an environment approval, which is done on GitHub.

Rules enforced by the server:

- one active run per environment (database unique index), and a second click within 10 seconds returns the first run
- a seed is refused when the environment has `allow_seed: false`, for admins too
- a production request needs a second person with `action.approve`. You cannot approve your own request. A requester can withdraw it. A rerun of a production run needs approval again
- every request, approval, rejection, dispatch, cancel and completion is written to the audit log

### GitHub App

Dispatching uses a GitHub App, not a user token. Create the App with repository permissions **Actions: read and write**, **Contents: read** and **Metadata: read**, install it on each org or repository in the catalog, and set:

```
GITHUB_APP_ID=<app id>
GITHUB_APP_PRIVATE_KEY_PATH=/run/secrets/github-app.pem
```

The server signs a 9 minute JWT, asks GitHub for the installation of each repository, and caches the installation token, refreshing it at 50 of its 60 minutes. A 401 drops the token and retries once.

The **GitHub** admin page checks every repository in the catalog at startup and every 10 minutes (every minute while one is failing): the App is installed, a token can be minted, and the migrate workflow exists and has a `workflow_dispatch` trigger. A repository that fails has its actions disabled, with the reason shown on the page and in the runs tab.

### Rate limits and polling cost

Requests use `ETag` and `If-None-Match`, and the deployment list is cached for 30 seconds. On a 403 or 429 with `Retry-After` all polling pauses until that time. Dispatches made during the pause fail with a retry hint.

### Trying it locally

`make up` starts `fake-github`, which also fakes the GitHub App and Actions APIs. It paces runs so a dispatch shows up after 4 seconds, waits 2 seconds in the queue and runs for 10 seconds. Change the pacing or break it on purpose:

```bash
curl -X PUT localhost:8090/_fake/scenario -d '{"dispatch":"never_appears"}'
curl -X PUT localhost:8090/_fake/scenario -d '{"dispatch":"422"}'
curl -X PUT localhost:8090/_fake/scenario -d '{"rate_limit_count":2,"rate_limit_retry_after":10}'
curl -X PUT localhost:8090/_fake/scenario -d '{"waiting":true}'
curl -X POST localhost:8090/_fake/expire-tokens
```

Sign in as `bob` and open `demo-app` / `prod` to request a production migration, then sign in as `erin` to approve it from **Approvals**.

### When a run is stuck or unknown

- `unknown`: the dispatch was accepted but no run carried the correlation id. Check that the workflow's `run-name` includes `${{ inputs.correlation_id }}`, then open the workflow link on the run.
- `failed` with "rejected the dispatch": the workflow does not accept the four inputs, the ref does not exist, or it has no `workflow_dispatch` trigger.
- Actions disabled for a repository: open the GitHub admin page and read the reason.

## Production deploy (IDP host)

You need a Linux host with Docker Compose v2 and nginx, a DNS name for it, and a TLS certificate (certbot is fine).

1. Install the server image by digest. The publish workflow pushes `ghcr.io/<owner>/helmsman-server`; copy the digest of the build you want into `IDP_IMAGE`.
2. Copy `deploy/env.server.example` to `.env` (mode 600) and fill it in. Generate the keys with `openssl rand -hex 32`. `POSTGRES_PASSWORD` is the table owner, used only for migrations. `APP_DB_PASSWORD` is the password of the limited `helmsman_app` role the server runs as.
3. Put the GitHub App key at `GITHUB_APP_KEY_FILE`, and check the catalog repository out at `CATALOG_DIR`.
4. Start it: `docker compose -f deploy/compose.server.yml --env-file .env up -d`. The first boot creates the `helmsman_app` login role (`deploy/db/00-app-role.sh`). The server then migrates as the owner and runs as `helmsman_app`.
5. Install `deploy/nginx/idp.conf` with your host name and certificate paths, then `nginx -t && systemctl reload nginx`. It sets the WebSocket upgrade headers and `proxy_read_timeout 3600s` for `/agent/ws` and `/ws/`.
6. For each app repository, set required reviewers on the `production` GitHub environment. Helmsman also requires a second approver, but it cannot see this GitHub setting.
7. Create the agents on the Agents page, give each host its join token, and install the agent as in `docs/agent-install.md`.

The server container is read-only, drops all capabilities, listens on `127.0.0.1:8080` and is limited to 256 MB. It checks itself with `/server healthcheck`.

### Upgrading

Pin the new digest in `IDP_IMAGE`, run `docker compose ... up -d`, and watch `docker compose logs idp`. Migrations are additive and run before the server starts serving. To roll back, put the previous digest back. Agents reconnect on their own.

## Backups and restore

`deploy/backup/backup.sh` writes a custom-format `pg_dump` and keeps 14 days. Run it nightly from cron on the IDP host:

```
15 2 * * *  cd /opt/helmsman && BACKUP_DEST=s3://my-bucket/helmsman ./deploy/backup/backup.sh >> /var/log/helmsman-backup.log 2>&1
```

With an S3 destination it uses the `aws` CLI (an instance role is best) and deletes dumps older than `BACKUP_KEEP_DAYS`. A bucket lifecycle rule of 14 days is a good second guard. With a directory as `BACKUP_DEST` it keeps local files.

Restore with `deploy/backup/restore.sh <dump or s3 url> [database]`. Passing a scratch database name is a drill. Restoring into the live database needs `RESTORE_CONFIRM=yes`, then `docker compose restart idp`. The dump carries the agent credential hashes, so agents reconnect by themselves, and the catalog is in git.

**Restore drill.** `make backup-test` (or `deploy/backup/verify-restore.sh` on the host, with `COMPOSE_FILE` unset) takes a dump, restores it into a scratch database, compares row counts for the main tables, and checks that the append-only trigger and the limited role's missing audit privileges survived. Run it monthly. It last passed here with:

```
ok   users: 4 rows         ok   action_runs: 9 rows
ok   apps: 1 rows          ok   audit_events: 94 rows
ok   app_envs: 2 rows      ok   audit_events is still append-only
ok   agents: 1 rows        ok   helmsman_app cannot update or delete audit events
ok   role_bindings: 5 rows restore drill passed
```

## Retention

The server prunes every 6 hours: health checks after 24 hours, container events after 14 days, stored run logs after 30 days (the run records stay). Audit events are kept one year and can only be removed by the owner-only function, so add a cron entry as the database owner:

```
30 3 * * 0  docker compose -f deploy/compose.server.yml exec -T postgres psql -U helmsman -d helmsman -c "SELECT audit_purge('365 days')"
```

The function refuses to keep under 90 days and writes an `audit.purge` event with the number of rows removed.

## Break-glass admin

Off by default. To enable it, run `go run ./cmd/breakglass-init` on an admin machine. It prints a random password (store it in your password manager), a TOTP secret (add it to an authenticator app) and the `IDP_BREAKGLASS_*` lines for `.env`. Set `IDP_BREAKGLASS_ENABLED=true` and restart. The login page then shows **Emergency access**. Sessions last one hour. Five failed attempts lock it for 15 minutes. Every attempt is in the audit log as `auth.breakglass`. Turn it off again once GitHub sign-in is back.

## Load test

`tools/loadtest` simulates agents and log viewers over the real protocol. It needs the dev stack with the load-test catalog and an empty database:

```bash
docker compose -f deploy/compose.dev.yml down -v
docker compose -f deploy/compose.dev.yml -f deploy/compose.loadtest.yml up -d --build --wait
make seed-dev
go run ./tools/loadtest -url http://localhost:8080 -agents 20 -viewers 10 -duration 60s
make up     # back to the normal catalog
```

Result on a MacBook Air (Docker Desktop), with the development build of the server:

| Measure | Result |
|---|---|
| Agents connected | 20 of 20 |
| Whole fleet dropped at once, time until all 20 were back | about 1 second, no agent stuck |
| Snapshots and health checks | 260 snapshots, 40 health checks answered |
| Log viewers | 10 concurrent, each on its own app, 0 open failures |
| Log delivery | every line the agents sent reached a viewer (5950 of 5950) |
| Time to first log line | p50 290 ms, p95 290 ms |
| Server memory | 16 to 18 MiB during the run |
| Postgres memory | 45 to 47 MiB |
| Server CPU | 1 to 4 percent |

The 10 viewers all report "closed early". That is the fleet-wide drop closing their streams, which is the intended behavior; the browser UI shows the stream as closed and the user reopens it. The tool does not reopen them. The 20 "agent errors" are the same drop, one per agent. The production image was not measured here, and the join endpoint's 20 per minute per-IP limit slowed the setup phase, which is why the tool retries joins.
