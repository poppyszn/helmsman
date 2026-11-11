# Runbook

## Dev stack

- Start: `make up`. Stop: `make down`. Logs: `make logs`.
- Reset the dev database: `docker compose -f deploy/compose.dev.yml down -v` (deletes dev data).

## Database

Migrations run automatically at server start (goose, embedded). Never edit an applied migration; add a new one. `audit_events` is append-only: UPDATE, DELETE and TRUNCATE are rejected by trigger.

Backup, restore and production operations are written in M4.
