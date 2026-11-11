#!/usr/bin/env bash
# Nightly Postgres backup for the IDP host. Run from cron as a user that can use docker:
#   15 2 * * *  cd /opt/helmsman && ./deploy/backup/backup.sh >> /var/log/helmsman-backup.log 2>&1
#
# Writes a custom-format pg_dump named helmsman-<UTC timestamp>.dump to BACKUP_DEST, then deletes
# dumps older than BACKUP_KEEP_DAYS (default 14).
#   BACKUP_DEST=s3://my-bucket/helmsman     uploads with the aws cli (instance role or AWS_* env)
#   BACKUP_DEST=/var/backups/helmsman       keeps dumps in a local directory (also used by make backup-test)
# S3 can enforce the retention itself with a lifecycle rule; the prune below is a second guard.
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/server/compose.yml}"
PG_SERVICE="${PG_SERVICE:-postgres}"
PG_USER="${PG_USER:-helmsman}"
PG_DB="${PG_DB:-helmsman}"
BACKUP_KEEP_DAYS="${BACKUP_KEEP_DAYS:-14}"
: "${BACKUP_DEST:?set BACKUP_DEST to an s3:// URL or a directory}"

compose() { docker compose -f "$COMPOSE_FILE" ${ENV_FILE:+--env-file "$ENV_FILE"} "$@"; }

name="helmsman-$(date -u +%Y%m%dT%H%M%SZ).dump"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

# -Fc is compressed and restorable table by table. ACLs are kept so the role grants survive a restore.
compose exec -T "$PG_SERVICE" pg_dump -U "$PG_USER" -Fc "$PG_DB" > "$tmp"
[ -s "$tmp" ] || { echo "backup: pg_dump produced an empty file" >&2; exit 1; }

case "$BACKUP_DEST" in
  s3://*)
    aws s3 cp --only-show-errors "$tmp" "${BACKUP_DEST%/}/$name"
    cutoff="$(date -u -d "-$BACKUP_KEEP_DAYS days" +%s 2>/dev/null || date -u -v-"${BACKUP_KEEP_DAYS}"d +%s)"
    aws s3 ls "${BACKUP_DEST%/}/" | awk '{print $1" "$2" "$4}' | while read -r d t key; do
      [ -n "$key" ] || continue
      ts="$(date -u -d "$d $t" +%s 2>/dev/null || date -u -j -f '%Y-%m-%d %H:%M:%S' "$d $t" +%s)"
      [ "$ts" -lt "$cutoff" ] && aws s3 rm --only-show-errors "${BACKUP_DEST%/}/$key"
    done
    ;;
  *)
    mkdir -p "$BACKUP_DEST"
    cp "$tmp" "$BACKUP_DEST/$name"
    find "$BACKUP_DEST" -name 'helmsman-*.dump' -mtime +"$BACKUP_KEEP_DAYS" -delete
    ;;
esac
echo "backup: wrote $name ($(wc -c < "$tmp") bytes) to $BACKUP_DEST"
