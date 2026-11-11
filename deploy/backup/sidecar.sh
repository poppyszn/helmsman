#!/bin/sh
# Backup sidecar. Commands: loop | once | list | restore <key|latest> [database] | drill
# Settings (environment): PGHOST PGUSER PGPASSWORD PGDATABASE, BACKUP_BUCKET, AWS_ENDPOINT_URL,
# AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, BACKUP_TIME (HH:MM, default 02:15), BACKUP_KEEP_DAYS (default 14).
set -eu

PGHOST="${PGHOST:-postgres}"; PGUSER="${PGUSER:-helmsman}"; PGDATABASE="${PGDATABASE:-helmsman}"
: "${BACKUP_BUCKET:?set BACKUP_BUCKET}"
KEEP="${BACKUP_KEEP_DAYS:-14}"
export PGHOST PGUSER PGDATABASE AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-us-east-1}"
S3="s3://$BACKUP_BUCKET"

log() { echo "$(date -u +%FT%TZ) backup: $*"; }

do_once() {
  key="helmsman-$(date -u +%Y%m%dT%H%M%SZ).dump"
  # Streamed: no temp file. -Fc keeps ACLs, so the limited role's grants survive a restore.
  pg_dump -Fc "$PGDATABASE" | aws s3 cp --only-show-errors - "$S3/$key"
  size="$(aws s3api head-object --bucket "$BACKUP_BUCKET" --key "$key" --query ContentLength --output text)"
  [ "$size" -gt 1024 ] || { log "FAILED: $key is only $size bytes"; return 1; }
  log "wrote $key ($size bytes)"
  cutoff="$(date -u -d "@$(( $(date +%s) - KEEP * 86400 ))" +%Y-%m-%dT%H:%M:%SZ)"
  aws s3api list-objects-v2 --bucket "$BACKUP_BUCKET" --prefix helmsman- \
    --query "Contents[?LastModified<\`$cutoff\`].Key" --output text | tr '\t' '\n' | while read -r old; do
      [ -n "$old" ] && [ "$old" != "None" ] || continue
      aws s3 rm --only-show-errors "$S3/$old" && log "pruned $old"
    done
}

latest() { aws s3api list-objects-v2 --bucket "$BACKUP_BUCKET" --prefix helmsman- --query 'sort_by(Contents,&LastModified)[-1].Key' --output text; }

do_restore() {
  key="${1:?usage: restore <key|latest> [database]}"; target="${2:-$PGDATABASE}"
  [ "$key" != latest ] || key="$(latest)"
  [ -n "$key" ] && [ "$key" != None ] || { log "no backups found"; return 1; }
  if [ "$target" = "$PGDATABASE" ] && [ "${RESTORE_CONFIRM:-}" != yes ]; then
    log "this replaces the live database '$PGDATABASE'. Re-run with RESTORE_CONFIRM=yes to continue."; return 2
  fi
  aws s3 cp --only-show-errors "$S3/$key" /tmp/restore.dump
  if [ "$target" != "$PGDATABASE" ]; then
    psql -d postgres -v ON_ERROR_STOP=1 -qc "DROP DATABASE IF EXISTS \"$target\"" -c "CREATE DATABASE \"$target\""
  fi
  pg_restore -d "$target" --clean --if-exists --exit-on-error /tmp/restore.dump
  rm -f /tmp/restore.dump
  log "restored $key into '$target'"
}

# Backup, restore into a scratch database, compare, check that the guarantees survived.
do_drill() {
  scratch="helmsman_restore_drill"; fail=0
  do_once
  do_restore latest "$scratch"
  for t in users apps app_envs agents role_bindings action_runs audit_events; do
    a="$(psql -d "$PGDATABASE" -tAc "select count(*) from $t")"; b="$(psql -d "$scratch" -tAc "select count(*) from $t")"
    if [ "$a" = "$b" ]; then log "ok   $t: $a rows"; else log "FAIL $t: live $a restored $b"; fail=1; fi
  done
  out="$(psql -d "$scratch" -qc "delete from audit_events" 2>&1 || true)"
  case "$out" in *append-only*) log "ok   audit_events is still append-only" ;; *) log "FAIL audit trigger missing after restore"; fail=1 ;; esac
  p="$(psql -d "$scratch" -tAc "select has_table_privilege('helmsman_app','audit_events','UPDATE') or has_table_privilege('helmsman_app','audit_events','DELETE')")"
  [ "$p" = f ] && log "ok   helmsman_app cannot update or delete audit events" || { log "FAIL limited role privileges: $p"; fail=1; }
  psql -d postgres -qc "DROP DATABASE IF EXISTS $scratch" >/dev/null 2>&1 || true
  [ "$fail" = 0 ] && log "restore drill passed" || { log "restore drill FAILED"; return 1; }
}

case "${1:-loop}" in
  once) do_once ;;
  list) aws s3 ls "$S3/" ;;
  restore) shift; do_restore "$@" ;;
  drill) do_drill ;;
  loop)
    at="${BACKUP_TIME:-02:15}"
    log "waiting for the first run at $at (container time ${TZ:-UTC}); keeping $KEEP days"
    [ "${BACKUP_ON_START:-}" != true ] || do_once || true
    while :; do
      now="$(date +%s)"; today="$(date +%Y-%m-%d)"
      next="$(date -d "$today $at" +%s)"; [ "$next" -gt "$now" ] || next="$(( next + 86400 ))"
      sleep "$(( next - now ))"
      do_once || log "run failed; will try again tomorrow"
    done ;;
  *) echo "usage: backup loop|once|list|restore <key|latest> [database]|drill" >&2; exit 64 ;;
esac
