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
