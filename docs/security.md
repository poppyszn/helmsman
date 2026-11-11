# Security checklist

Every item from the pilot checklist, with where it is enforced and how it is tested. "Test" names are Go tests that run in `make test`.

| # | Requirement | Where it is enforced | Evidence |
|---|---|---|---|
| 1 | TLS everywhere. Agents refuse `ws://` outside dev | `deploy/server/caddy/Caddyfile` (Let's Encrypt) or `deploy/nginx/idp.conf` terminates TLS with HSTS. `internal/agent/config.go` refuses `ws://` unless `IDP_ALLOW_INSECURE=true`. Cookies are `Secure` unless the base URL is plain http (dev) | `internal/agent/agent_test.go` (ws refusal). Cookie flags: `TestLoginAndSession` |
| 2 | No inbound ports for the agent. Docker socket never exposed over TCP | The agent only dials out and opens no listener. `deploy/compose.agent.yml` publishes no ports and mounts the socket file only | Protocol has no server-initiated connection to the agent: `pkg/protocol` |
| 3 | Agent runs non-root with `group_add` and enforces its local allowlist | `deploy/compose.agent.yml`: `user: 65532`, `group_add: [DOCKER_GID]`, 64 MB and 0.25 CPU. `internal/agent` denies anything not in `allow` | `TestAgentEndToEnd` (allowlist refusal) |
| 4 | Session IDs, agent credentials and join tokens hashed. GitHub user tokens encrypted | Sessions and credentials are stored as SHA-256 hashes. Join tokens are SHA-256 hashed and single use. GitHub tokens use AES-256-GCM with `IDP_ENCRYPTION_KEY` (`internal/auth/crypto.go`) | `TestAgentJoinRevokeAndSharedCredential`, crypto unit tests |
| 5 | CSRF header on state-changing requests, strict CSP, WebSocket Origin check | `guard()` checks `X-CSRF-Token` on every non-GET. `securityHeaders` sets CSP (`default-src 'self'`, no inline script). The log socket accepts same-origin only | `TestCSRFRequiredOnStateChange`, `TestSecurityHeaders`, `TestLogsWebSocketRejectsOtherOrigins` |
| 6 | Generated test proving every route returns 403 without its permission | The route table in `internal/api/routes.go` is the only router. A route without a permission refuses to mount | `TestEveryRouteDeniesWithoutPermission` walks the table: 401 without a session, 403 with none of the permission |
| 7 | Production actions need second-person approval in Helmsman plus GitHub environment reviewers | `internal/actions`: production requests are `pending_approval`, the requester can never approve (`rbac.CanApprove`), a rerun needs approval again. The GitHub half is a setting: add required reviewers to the `production` environment of each app repository | `TestProductionNeedsASecondPerson`, `TestProductionRejectAndWithdraw`. Real GitHub run verified by hand |
| 8 | Audit table append-only at the database permission level | Triggers block UPDATE, DELETE and TRUNCATE for everyone. The server runs as `helmsman_app`, which holds only INSERT and SELECT on `audit_events` (migration 00004). Rows leave only through the owner-only `audit_purge()` function, which refuses to keep under 90 days and records itself | `TestAppRoleCannotChangeOrRemoveAuditEvents`, `TestOwnerCannotEditAuditButMayPurgeOldRows`. The whole API suite runs as `helmsman_app` |
| 9 | Rate limits on login, actions and log-open | Login and agent join: 20 per minute per IP. Actions (every non-GET): 30 per minute per user. Log streams opened: 60 per minute per user, plus 5 open streams per agent. nginx adds 30 per minute on `/auth/` and `/agent/join` | `TestRateLimiter`, `TestActionsAndLogOpensAreRateLimitedPerUser` |
| 10 | CI runs govulncheck, npm audit and Trivy. Server compose pins images by digest | `.github/workflows/ci.yml`: govulncheck, `npm audit --omit=dev --audit-level=high`, Trivy on both images and on `deploy/` configuration, and a check that every image in `deploy/server/compose.yml` carries a digest | CI (see the note in the runbook about GitHub Actions on this repository) |
| 11 | Retention: run logs 30 days, audit 1 year, health checks 24 hours | `Store.Prune` runs every 6 hours: health checks 24 hours, container events 14 days, stored run logs 30 days. Audit: `SELECT audit_purge('365 days')` from cron as the database owner | `TestRetentionPrune`, `TestOwnerCannotEditAuditButMayPurgeOldRows` |

## Other controls

- **Client address.** Rate limits and the audit log use the real client address. `X-Forwarded-For` is believed only when the connection comes from a trusted proxy (loopback and private ranges by default, `IDP_TRUSTED_PROXIES` to narrow it), and it is read from the right, so a caller cannot invent its own address. This replaced chi's `RealIP`, which trusts the header from anyone. Test: `TestClientAddrTrustsForwardedForOnlyFromProxies`.
- **Break-glass admin** is off by default. When on, it needs a bcrypt password and a TOTP code, locks for 15 minutes after 5 failures (globally, not per address), refuses a reused code, issues a one hour session, and audits every attempt without the password or code. Tests: `TestBreakglassIsOffByDefault`, `TestBreakglassNeedsBothFactorsAndIsAudited`, `TestBreakglassLocksAfterRepeatedFailures`, and the RFC 6238 vector in `internal/auth`.
- **No remote code execution.** The agent protocol has fixed message types and the portal can only start the catalog's workflows with four fixed inputs. See `pkg/protocol` and `internal/actions`.
- **Production tier protection.** `env: *` never matches a prod env. Seeds are refused when `allow_seed` is false, for admins too. Tests: `TestProdInvisibleToNonprodOnlyDev`, `TestSeedRefusedWhenEnvForbidsItEvenForAdmins`.
- **Server container.** Distroless, non-root, read-only root filesystem, all capabilities dropped, `no-new-privileges`, 256 MB limit, bound to `127.0.0.1` (`deploy/server/compose.yml`).
- **Secrets.** Nothing secret is in the repository. `.env`, `*.pem` and `deploy/secrets/` are ignored. `.env.example` and `deploy/server/env.example` document every variable.

## Open items

- GitHub environment reviewers are configured on GitHub, so Helmsman cannot enforce that they exist. The runbook lists it as a step per app repository.
- Audit retention depends on a cron entry the operator adds. If it is missing, the table only grows, which is the safe failure.
