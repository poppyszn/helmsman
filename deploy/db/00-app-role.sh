#!/bin/sh
# Runs once, when the Postgres data directory is first created. It makes the login role that the
# server uses at runtime. The table grants (INSERT and SELECT only on audit_events) come from
# migration 00004, which runs when the server first starts.
# For a database that already exists, run the same statement by hand with a new password:
#   ALTER ROLE helmsman_app LOGIN PASSWORD '...';
set -eu
: "${APP_DB_PASSWORD:?APP_DB_PASSWORD is required}"
psql -v ON_ERROR_STOP=1 -v pw="$APP_DB_PASSWORD" --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<'SQL'
CREATE ROLE helmsman_app LOGIN PASSWORD :'pw';
SQL
