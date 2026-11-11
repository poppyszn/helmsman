#!/usr/bin/env bash
# Restore drill: dump the running database, restore it into a scratch database, compare row counts
# and check that the restored copy still enforces the append-only audit log and the limited role.
# Used by `make backup-test` against the dev stack, and meant to be run on the IDP host once a month.
set -euo pipefail
cd "$(dirname "$0")/../.."

export COMPOSE_FILE="${COMPOSE_FILE:-deploy/server/compose.yml}"
PG_SERVICE="${PG_SERVICE:-postgres}"; PG_USER="${PG_USER:-helmsman}"; PG_DB="${PG_DB:-helmsman}"
scratch="helmsman_restore_drill"
dir="$(mktemp -d)"
trap 'rm -rf "$dir"; docker compose -f "$COMPOSE_FILE" exec -T "$PG_SERVICE" psql -U "$PG_USER" -d postgres -qc "DROP DATABASE IF EXISTS $scratch" >/dev/null 2>&1 || true' EXIT

BACKUP_DEST="$dir" deploy/backup/backup.sh
dump="$(ls "$dir"/helmsman-*.dump)"
deploy/backup/restore.sh "$dump" "$scratch"

q() { docker compose -f "$COMPOSE_FILE" exec -T "$PG_SERVICE" psql -U "$PG_USER" -d "$1" -tAc "$2" | tr -d '[:space:]'; }
fail=0
for t in users apps app_envs agents role_bindings action_runs audit_events; do
  a="$(q "$PG_DB" "select count(*) from $t")"; b="$(q "$scratch" "select count(*) from $t")"
  if [ "$a" = "$b" ]; then echo "  ok   $t: $a rows"; else echo "  FAIL $t: live $a, restored $b"; fail=1; fi
done
# Triggers and grants must survive the restore.
out="$(docker compose -f "$COMPOSE_FILE" exec -T "$PG_SERVICE" psql -U "$PG_USER" -d "$scratch" -qc "delete from audit_events" 2>&1 || true)"
if [[ "$out" == *"append-only"* ]]; then
  echo "  ok   audit_events is still append-only"; else echo "  FAIL audit trigger missing after restore"; fail=1; fi
priv="$(q "$scratch" "select has_table_privilege('helmsman_app','audit_events','UPDATE') or has_table_privilege('helmsman_app','audit_events','DELETE')")"
if [ "$priv" = "f" ]; then echo "  ok   helmsman_app cannot update or delete audit events"; else echo "  FAIL helmsman_app privileges after restore: $priv"; fail=1; fi
[ "$fail" = 0 ] && echo "restore drill passed" || { echo "restore drill FAILED"; exit 1; }
