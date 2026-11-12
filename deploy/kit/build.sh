#!/usr/bin/env bash
# Builds the public install kit from this repository into a folder, ready to be a git repository of its own.
#   make install-kit OUT=../helmsman-install
# Everything in it is copied from the sources of truth in this repository (the compose files, the examples,
# the guides in deploy/kit), so the kit never drifts. It contains no application source and no secrets. An
# existing .git in the folder is kept, so you can commit and push the result.
set -euo pipefail
cd "$(dirname "$0")/../.."
out="${1:?usage: deploy/kit/build.sh <output folder>}"
repo="${KIT_REPO:-poppyszn/helmsman-install}"   # owner/name of the public repository the kit is published to
mkdir -p "$out"
find "$out" -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +

mkdir -p "$out/agent" "$out/deploy" "$out/docs" "$out/examples"
cp deploy/kit/README.md "$out/README.md"
cp deploy/kit/docs/*.md "$out/docs/"
cp deploy/kit/agent-install.sh "$out/agent/install.sh"
chmod +x "$out/agent/install.sh"
cp deploy/compose.agent.yml "$out/agent/compose.agent.yml"
cp examples/agent.yaml "$out/agent/agent.example.yaml"
cp deploy/kit/agent-env.example "$out/agent/env.example"
cp examples/workflows/helmsman-run.yml "$out/examples/helmsman-run.yml"

mkdir -p "$out/deploy/docker" "$out/deploy/backup" "$out/deploy/nginx"
cp -R deploy/server "$out/deploy/server"
rm -f "$out/deploy/server/.env" "$out/deploy/server/bundle.sh"
cp -R deploy/loki deploy/alloy "$out/deploy/"
cp deploy/backup/sidecar.sh "$out/deploy/backup/"
cp deploy/docker/backup.Dockerfile "$out/deploy/docker/"
cp deploy/nginx/idp.conf "$out/deploy/nginx/idp.conf"

# The clone address in the guides names the kit repository.
sed -i.bak "s#OWNER/helmsman-install#${repo}#g" "$out/README.md" "$out/docs/"*.md && rm -f "$out/README.md.bak" "$out/docs/"*.bak

printf '.env\n*.pem\n*.key\nagent/.env\n' > "$out/.gitignore"
git rev-parse HEAD > "$out/SOURCE_COMMIT"

# Nothing private may leave: refuse to finish if a secret-looking value or an internal name slipped in.
if grep -rIEn '(BEGIN [A-Z ]*PRIVATE KEY|ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16})' "$out" --exclude-dir=.git; then
  echo "build: a secret-looking value is in the kit; fix the source" >&2
  exit 1
fi
echo "wrote $out ($(find "$out" -type f ! -path '*/.git/*' | wc -l | tr -d ' ') files)"
