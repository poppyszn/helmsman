#!/bin/sh
# Runs inside the Playwright container on the dev compose network.
set -eu
node forward.mjs &
npm install --no-audit --no-fund --silent
npx tsc --noEmit
exec npx playwright test --project=dev "$@"
