#!/usr/bin/env bash
# Restore a backup made by backup.sh into a Postgres database. Usage:
#   deploy/backup/restore.sh <dump file | s3://bucket/key> [target database]
# The default target is the live database, which is DESTRUCTIVE: it drops and recreates every
# object. For a drill, pass a scratch database name, which is created if missing.
# After a restore into the live database, restart the server (docker compose restart idp). Agents
# keep their credentials, which are in the dump, and reconnect on their own.
set -euo pipefail

src="${1:?usage: restore.sh <dump file | s3:// url> [target database]}"
COMPOSE_FILE="${COMPOSE_FILE:-deploy/server/compose.yml}"
PG_SERVICE="${PG_SERVICE:-postgres}"
PG_USER="${PG_USER:-helmsman}"
PG_DB="${PG_DB:-helmsman}"
target="${2:-$PG_DB}"

compose() { docker compose -f "$COMPOSE_FILE" ${ENV_FILE:+--env-file "$ENV_FILE"} "$@"; }

tmp=""
trap '[ -z "$tmp" ] || rm -f "$tmp"' EXIT
if [[ "$src" == s3://* ]]; then
  tmp="$(mktemp)"; aws s3 cp --only-show-errors "$src" "$tmp"; src="$tmp"
fi
[ -s "$src" ] || { echo "restore: $src is missing or empty" >&2; exit 1; }

if [ "$target" = "$PG_DB" ] && [ -z "${RESTORE_CONFIRM:-}" ]; then
  echo "restore: this will replace the live database '$PG_DB'. Re-run with RESTORE_CONFIRM=yes to continue." >&2
  exit 2
fi

if [ "$target" != "$PG_DB" ]; then
  compose exec -T "$PG_SERVICE" psql -U "$PG_USER" -d postgres -v ON_ERROR_STOP=1 \
    -c "DROP DATABASE IF EXISTS \"$target\"" -c "CREATE DATABASE \"$target\""
fi
# --clean --if-exists makes the live restore replace objects. Owners are kept (the owner role is the
# same on a rebuilt host) and so are grants, so helmsman_app keeps its limited access.
compose exec -T "$PG_SERVICE" pg_restore -U "$PG_USER" -d "$target" --clean --if-exists --exit-on-error < "$src"
echo "restore: restored into database '$target'"
