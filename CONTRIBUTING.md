# Contributing

Welcome. This page is how we work on Helmsman, so changes look the same whoever writes them. Read the [README](README.md) for what the product does, [docs/architecture.md](docs/architecture.md) for how it fits together, and the hard rules in [AGENTS.md](AGENTS.md) before you change anything that touches access, the agent, or the audit log.

## Set up

Everything runs in Docker. The host needs only `make` and Docker with Compose v2: do not install Go or Node on the host.

```bash
make up          # dev stack: Postgres, server, UI, agent, fake GitHub, demo apps, seeded data
make test        # Go vet and tests with a throwaway Postgres, then web lint, typecheck and unit tests
make lint        # golangci-lint, the same version CI runs
make e2e         # Playwright end-to-end tests against the dev stack
make help        # every target
```

The UI is at http://localhost:5173. Sign in through the fake GitHub picker; the dev accounts are in the [runbook](docs/runbook.md#dev-stack-accounts). `make up-logs` adds Loki and Alloy for log history, and `make up-github` runs against a real GitHub App.

## The shape of a change

1. **Branch** from `main`: `feat/<what>`, `fix/<what>`, `docs/<what>`, `chore/<what>`.
2. **Commit** with [conventional commits](https://www.conventionalcommits.org): `feat: ...`, `fix: ...`, `docs: ...`. The subject says what changed for a user; the body says why when it is not obvious.
3. **Open a pull request** against `main` using the template. One change per pull request. Keep them small enough to review in one sitting, and stack or split anything bigger.
4. **CI must be green** before merge: Go (lint, vet, race tests, vulnerability scan), web (lint, typecheck, unit tests, build, audit), images and config scans, the compose checks, and the end-to-end suite.
5. **Merge** with a merge commit. Merging `main` deploys it (see [docs/server-setup.md](docs/server-setup.md)), so do not merge something you would not want live.

### Definition of done

A change is done when all of these are true:

- It does what it says, and the failure paths are handled and shown to the user in words, not a status code.
- There are tests at the right level: a unit test for logic, an API integration test for anything behind a route (including who may call it and who may not), a Playwright test for anything a person clicks.
- The docs that describe it are updated in the same pull request (see [Docs](#docs)).
- Security rules still hold (see [Rules that do not bend](#rules-that-do-not-bend)).
- `make test`, `make lint` and `make e2e` pass locally, or you say in the pull request what you could not run.

## Rules that do not bend

The full list is in [AGENTS.md](AGENTS.md) section 2. The short version:

- **Deny by default.** Every HTTP route is declared in `internal/api/routes.go` with a permission and a scope. A route without one makes the server refuse to start, and a generated test calls every route unauthenticated.
- **No free form commands anywhere.** The agent protocol has fixed message types. Adding one is a security decision: write what it can do, what the agent's allowlist says about it, and what an attacker holding the server could do with it.
- **Everything that changes state is audited**, with who, what, and the result. Never put secrets or log text in an audit entry.
- **Secrets never go in git** or in logs or in API answers. Stored secrets are encrypted with `IDP_ENCRYPTION_KEY`.
- **Migrations are additive.** Never edit an applied migration; add the next number. The protocol only changes additively too (new optional fields), so an older agent keeps working.
- **Production is special.** An `env: *` binding never matches a production environment, and a role never grants seeding production.

## Recipes

### Add an API route

1. Write the handler in `internal/api/`.
2. Add a row to the table in `routes.go`: method, path, permission, scope, handler. Pick the narrowest scope that is true (`ScopeEnv` for a specific environment, `ScopeGlobal` for admin pages).
3. Audit anything that changes state.
4. Add an integration test in `internal/api/*_integration_test.go`. Test the allowed caller, a caller without the permission, and bad input.

### Add a database migration

1. Add `internal/store/migrations/NNNNN_name.sql` with the next number, goose format (`-- +goose Up` and `-- +goose Down`). Check the highest number on `main` first; two branches with the same number collide.
2. Only add: new tables, new nullable or defaulted columns. A destructive change needs an explicit plan in the pull request.
3. If the runtime role needs access to a new table, remember the limited `helmsman_app` role gets table privileges through default privileges; audit tables stay insert-only.
4. Add or extend a test in `internal/store` or the API tests that exercises it.

### Add a catalog field

1. Add the field to the struct in `internal/catalog/catalog.go` (yaml and json tags), validate it in `validate.go`, and carry it into the stored spec in `manager.go` if the UI needs it.
2. The parser rejects unknown fields on purpose, so a typo in `apps.yaml` is an error: keep that.
3. Document it in [docs/catalog.md](docs/catalog.md) with an example, and add a test in `internal/catalog`.

### Add a notification event

1. Add the constant and a row in `Types` in `internal/notify/event.go` (group, label, default).
2. Emit it from where it happens: the run tracker hook, or the watcher in `internal/api/notifywatch.go` for state changes (use `observe` so a change must show on two looks, and a first sighting is recorded silently).
3. Add it to the table in [docs/notifications.md](docs/notifications.md) and add a test.

### Add a page to the UI

1. Create the page in `web/src/pages/`, add the route in `web/src/App.tsx` and the sidebar item in `web/src/components/Layout.tsx` (gated on the right `me.can.*` flag).
2. Follow the [design system](docs/design-system.md): tonal surfaces, no emojis, no text under 12px, links in sky blue, one calm status indicator.
3. Read data with TanStack Query through `api()` in `web/src/lib/api.ts`; put the response types there.
4. Add a Playwright test under `e2e/tests/dev/`. Use roles and labels, not CSS selectors, and give form controls proper labels.

## Testing

| Level | Where | Run |
|---|---|---|
| Go unit and integration | `internal/**/*_test.go` (the API tests use a real Postgres) | `make test` |
| Web unit | `web/src/**/*.test.ts` (vitest) | `make test` |
| End to end | `e2e/tests/dev` against the dev stack | `make e2e` (`E2E_ARGS="tests/dev/05-runs.spec.ts"` for one file) |
| A live deployment | `e2e/tests/server` | `make e2e-server` |
| Backup and restore | `deploy/backup` | `make backup-test` |

Write tests that would have caught the bug. A test that only restates the implementation is not worth having.

## Docs

Docs are part of the change. If behaviour, a setting, a route, a permission, a catalog field, or an operation changes, the same pull request updates the doc that describes it, and the pull request template asks you to say which.

Where things go:

| You are writing | It goes in |
|---|---|
| What the product does, how to start | [README.md](README.md) |
| How it is built | [docs/architecture.md](docs/architecture.md) |
| A setting, field or command reference | the reference doc for that area (`catalog.md`, `notifications.md`, `logs.md`, `agent-install.md`) |
| How to run and fix it in production | [docs/runbook.md](docs/runbook.md) and [docs/server-setup.md](docs/server-setup.md) |
| A security control and its test | [docs/security.md](docs/security.md) |
| What comes next | [docs/roadmap.md](docs/roadmap.md) |
| Rules for agents and the hard constraints | [AGENTS.md](AGENTS.md) |

Style, so every page reads the same:

- Start with one `#` title and a sentence saying what the page is for. Use sentence case headings.
- Write plainly and in the present tense. Say what to do, then why. No marketing words, no emojis, no em dashes.
- Commands go in fenced blocks tagged `bash`, one command per block when someone will copy it, with no `$` prompt and no output mixed in.
- Name real files, settings and routes in backticks, and link to the doc that owns a topic instead of repeating it.
- Use relative links. Do not hard code the domain of a real deployment: use `portal.example.com`.
- Do not write numbers that go stale (test counts, durations) unless the page is about measuring them.

`make docs-check` checks the mechanical parts (titles, relative links, no em dashes) and CI runs it.

## Reviewing

Review for the things CI cannot see: is the permission and scope right, is anything audited that should be, can a lower role do something it should not, does the failure path tell the user what to do, and is the doc updated. Say what you checked in the review so the author knows what was covered.

## Security issues

Do not open a public issue for a vulnerability. Tell the repository owner directly.
