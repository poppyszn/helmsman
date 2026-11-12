#!/usr/bin/env bash
# Runs docker compose for this stack with the image named in helmsman.version, on the host.
#   ./up.sh up -d --wait      start or update everything
#   ./up.sh ps                status
#   ./up.sh logs -f server    follow the server's log
# The deploy workflow uses it too, so a deploy and a manual run are the same thing.
set -euo pipefail
cd "$(dirname "$0")"
image=$(grep -vE '^\s*(#|$)' helmsman.version | head -1 | tr -d '[:space:]')
[ -n "$image" ] || { echo "up.sh: helmsman.version has no image line" >&2; exit 1; }
export HELMSMAN_IMAGE="$image"
exec docker compose --env-file .env "$@"
