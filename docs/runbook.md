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

`make seed-dev` registers the dev agent and pre-binds users. Sign in through fake GitHub at http://localhost:5173:

| User | Org | Access |
| --- | --- | --- |
| alice | acme-org | admin (bootstrap) |
| bob | acme-org/backend | developer on every nonprod env |
| carol | acme-org | viewer on demo-app staging |
| erin | acme-org/leads | approver (nonprod) |
| dave | other-org | rejected: not in an allowed org |
| frank | acme-org (restricted) | rejected: org blocks OAuth apps |

## Database

Migrations run automatically at server start (goose, embedded). Never edit an applied migration; add a new one. `audit_events` is append-only: UPDATE, DELETE and TRUNCATE are rejected by trigger.

Backup, restore and production operations are written in M4.
