#!/bin/sh
# Runs inside the test container on the dev compose network.
set -eu
socat TCP-LISTEN:5173,bind=127.0.0.1,fork,reuseaddr TCP:web:5173 &
socat TCP-LISTEN:8090,bind=127.0.0.1,fork,reuseaddr TCP:fake-github:8090 &
npm install --no-audit --no-fund --silent
npx tsc --noEmit
exec npx playwright test --project=dev "$@"
