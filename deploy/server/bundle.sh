#!/usr/bin/env bash
# Zips the files a server needs when you copy them by hand instead of using the deploy pipeline.
# Run it from the repository root (make server-bundle). It never includes a .env.
set -euo pipefail
cd "$(dirname "$0")/../.."
out=tmp/helmsman-server-files.zip
rm -rf tmp/bundle "$out"
dest=tmp/bundle/helmsman/deploy
mkdir -p "$dest/server" "$dest/docker" "$dest/backup" "$dest/nginx"
cp -R deploy/server/. "$dest/server/"
rm -f "$dest/server/.env" "$dest/server/bundle.sh"
cp deploy/docker/backup.Dockerfile "$dest/docker/"
cp deploy/backup/sidecar.sh "$dest/backup/"
cp deploy/nginx/idp.conf "$dest/nginx/"
cp -R deploy/loki deploy/alloy "$dest/"
(cd tmp/bundle && zip -qr "../helmsman-server-files.zip" helmsman)
echo "wrote $out"
