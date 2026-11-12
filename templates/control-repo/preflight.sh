#!/usr/bin/env bash
# Runs on the host before every deploy.
# It stops the deploy, before anything changes, if the host's .env is missing something the stack needs.
set -euo pipefail
cd "$(dirname "$0")"

[ -f .env ] || { echo "preflight: $(pwd)/.env is missing. Copy env.example to .env and fill it in." >&2; exit 1; }

# Read values the way Compose does (KEY=value per line, quotes optional, spaces allowed). The file is
# never sourced, so a value like CADDY_TLS=tls internal cannot be run as a command.
val() { grep -E "^$1=" .env | tail -1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
for v in IDP_BASE_URL IDP_SESSION_KEY IDP_ENCRYPTION_KEY IDP_ALLOWED_ORGS POSTGRES_PASSWORD APP_DB_PASSWORD \
         GITHUB_OAUTH_CLIENT_ID GITHUB_OAUTH_CLIENT_SECRET GITHUB_APP_ID GITHUB_APP_KEY_FILE IDP_GITHUB_MODE; do
  printf -v "$v" '%s' "$(val "$v")"
done

missing=""
for v in IDP_BASE_URL IDP_SESSION_KEY IDP_ENCRYPTION_KEY IDP_ALLOWED_ORGS POSTGRES_PASSWORD APP_DB_PASSWORD \
         GITHUB_OAUTH_CLIENT_ID GITHUB_OAUTH_CLIENT_SECRET GITHUB_APP_ID GITHUB_APP_KEY_FILE; do
  [ -n "${!v:-}" ] || missing="$missing $v"
done
[ -z "$missing" ] || { echo "preflight: .env is missing:$missing" >&2; exit 1; }

# Only existence is checked: the key is owned by uid 65532 with mode 400 (the server container's user), so this user
# cannot read it, and Docker, which runs as root, mounts it. The folder must be reachable by this user, or the file
# looks missing: sudo chown deploy:deploy /opt/helmsman-secrets
[ -e "$GITHUB_APP_KEY_FILE" ] || { echo "preflight: GITHUB_APP_KEY_FILE ($GITHUB_APP_KEY_FILE) does not exist, or its folder cannot be reached by $(id -un). Upload the key, and run: sudo chown $(id -un):$(id -gn) $(dirname "$GITHUB_APP_KEY_FILE")" >&2; exit 1; }
# The server container runs as uid 65532. A key it cannot read shows up later as "private key could not be read".
if [ "$(stat -c %u "$GITHUB_APP_KEY_FILE" 2>/dev/null || echo x)" != 65532 ] && [ "$(( 0$(stat -c %a "$GITHUB_APP_KEY_FILE" 2>/dev/null || echo 0) & 4 ))" -eq 0 ]; then
  echo "preflight: warning: $GITHUB_APP_KEY_FILE is not readable by the server (uid 65532). Run: sudo chown 65532:65532 $GITHUB_APP_KEY_FILE && sudo chmod 400 $GITHUB_APP_KEY_FILE" >&2
fi
[ "${#IDP_SESSION_KEY}" -ge 64 ] || { echo "preflight: IDP_SESSION_KEY must be 64 hex characters (openssl rand -hex 32)" >&2; exit 1; }
[ "${#IDP_ENCRYPTION_KEY}" -ge 64 ] || { echo "preflight: IDP_ENCRYPTION_KEY must be 64 hex characters (openssl rand -hex 32)" >&2; exit 1; }
case "$IDP_BASE_URL" in https://*) ;; *) echo "preflight: IDP_BASE_URL must start with https://" >&2; exit 1 ;; esac
[ "${IDP_GITHUB_MODE:-real}" = real ] || { echo "preflight: IDP_GITHUB_MODE must be real on a deployed host" >&2; exit 1; }
[ -f catalog/apps.yaml ] || { echo "preflight: catalog/apps.yaml is missing" >&2; exit 1; }
[ -f helmsman.version ] || { echo "preflight: helmsman.version is missing" >&2; exit 1; }
echo "preflight: ok"
