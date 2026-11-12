#!/bin/sh
# Installs the Helmsman agent on this host and starts it.
#
#   ./install.sh                      asks for the portal address and the join token
#   HELMSMAN_URL=https://portal.example.com HELMSMAN_JOIN_TOKEN=... ./install.sh
#
# Options: --dir <folder> (default /opt/helmsman-agent), --no-start (write the files only).
# The join token is read without echo, is never printed, and is removed from .env once the agent has joined.
set -eu

DIR="${HELMSMAN_AGENT_DIR:-/opt/helmsman-agent}"
START=1
die() { echo "install: $*" >&2; exit 1; }
usage() { sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
  case "$1" in
    --dir) [ $# -ge 2 ] || die "--dir needs a folder"; DIR="$2"; shift ;;
    --no-start) START=0 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option $1 (see --help)" ;;
  esac
  shift
done

here=$(cd "$(dirname "$0")" && pwd)
[ -f "$here/compose.agent.yml" ] || die "compose.agent.yml must sit next to this script"
command -v docker >/dev/null 2>&1 || die "Docker is not installed"
docker compose version >/dev/null 2>&1 || die "Docker Compose v2 is required (docker compose version)"

# 1. The portal address.
portal="${HELMSMAN_URL:-}"
if [ -z "$portal" ]; then
  printf 'Portal address, for example https://portal.example.com: '
  read -r portal
fi
portal=${portal%/}
case "$portal" in
  https://*) ;;
  *) die "the portal address must start with https://" ;;
esac
case "$portal" in
  *[!A-Za-z0-9./:_-]*) die "the portal address has unexpected characters" ;;
esac
idp_url="wss://${portal#https://}/agent/ws"

# 2. The join token, from the Agents page of the portal. Single use, valid for an hour.
token="${HELMSMAN_JOIN_TOKEN:-}"
if [ -z "$token" ]; then
  printf 'Join token (input is hidden): '
  if [ -t 0 ]; then
    trap 'stty echo 2>/dev/null || true' EXIT INT TERM
    stty -echo
    read -r token
    stty echo
    trap - EXIT INT TERM
    echo
  else
    read -r token
  fi
fi
case "$token" in
  ""|*[!A-Za-z0-9_-]*) die "that does not look like a join token (letters, digits, - and _ only)" ;;
esac
[ "${#token}" -ge 30 ] || die "that join token is too short"

# 3. The group that owns the Docker socket, so the agent can read it without root.
sock=/var/run/docker.sock
[ -S "$sock" ] || die "$sock is not there: is Docker running on this host?"
gid=$(stat -c %g "$sock" 2>/dev/null || stat -f %g "$sock" 2>/dev/null) || die "could not read the group of $sock"

# 4. The files.
mkdir -p "$DIR"
cp "$here/compose.agent.yml" "$DIR/compose.agent.yml"
if [ -f "$DIR/agent.yaml" ]; then
  echo "install: keeping your existing $DIR/agent.yaml"
else
  cat > "$DIR/agent.yaml" <<YAML
# Read-only inside the container. Nothing is exposed until you list it under allow.
idp_url: ${idp_url}
# The agent tells the portal the names (not logs) of compose services on this host that are not exposed yet,
# so the portal can draft the entries to add them. Set to false to turn that off.
discovery: true
allow: []
YAML
fi
umask 077
cat > "$DIR/.env.new" <<ENV
IDP_JOIN_TOKEN=${token}
AGENT_VERSION=${AGENT_VERSION:-latest}
DOCKER_GID=${gid}
ENV
mv "$DIR/.env.new" "$DIR/.env"
chmod 600 "$DIR/.env"
echo "install: wrote $DIR (agent.yaml, compose.agent.yml, .env)"

[ "$START" -eq 1 ] || { echo "install: not started (--no-start). Run: cd $DIR && docker compose -f compose.agent.yml --env-file .env up -d"; exit 0; }

# 5. Start it, and wait for it to join.
cd "$DIR"
dc() { docker compose -f compose.agent.yml --env-file .env "$@"; }
if ! dc pull; then
  die "could not pull the agent image. If it is private, run: docker login ghcr.io (then docker logout ghcr.io when done)"
fi
dc up -d
echo "install: waiting for the agent to join the portal..."
i=0
while [ "$i" -lt 30 ]; do
  if dc logs --tail 100 2>/dev/null | grep -q '"msg":"joined"'; then
    # The agent keeps its own credential now. The token is spent, so take it out of .env.
    grep -v '^IDP_JOIN_TOKEN=' .env > .env.new || true
    mv .env.new .env
    chmod 600 .env
    echo "install: joined. The join token was removed from .env."
    echo "Next: in the portal, open Agents > Containers on this host to choose what to expose."
    exit 0
  fi
  i=$((i + 1))
  sleep 2
done
echo "install: the agent has not joined yet. See why with:  cd $DIR && docker compose -f compose.agent.yml --env-file .env logs --tail 30" >&2
echo "install: the join token is single use and lasts one hour; create a new one on the Agents page if it expired." >&2
exit 1
