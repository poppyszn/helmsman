#!/usr/bin/env sh
# Builds the generated samples in examples/logs from the files Helmsman actually deploys, so they can never
# drift from them: three Alloy files and the local Loki config. CI runs this and fails if a committed file
# is out of date.
set -eu
cd "$(dirname "$0")/../.."

build() { # name, description, parts...
  out="examples/logs/alloy/$1.alloy"; desc="$2"; shift 2
  {
    printf '// %s\n//\n' "$desc"
    printf '// Generated from deploy/alloy (%s). Do not edit here: edit those files and run\n' "$*"
    printf '// examples/logs/build-all-in-one.sh. Settings come from environment variables, described in each part.\n'
    for f in "$@"; do
      printf '\n// ---------------------------------------------------------------- %s.alloy\n' "$f"
      cat "deploy/alloy/$f.alloy"
    done
  } > "$out"
}

build all-in-one "ALL IN ONE: container logs and the Faro receiver in one file, for a single machine that runs both your
// containers and the Faro endpoint. Run with:  alloy run all-in-one.alloy" write collector faro
build containers-only "CONTAINERS ONLY: ships this host's Docker container logs to a Loki elsewhere (set LOKI_URL, LOKI_USER,
// LOKI_PASSWORD). Use it on every app host. Run with:  alloy run containers-only.alloy" write collector
build faro-only "FARO ONLY: the browser telemetry receiver and nothing else, for the machine browsers reach (set
// FARO_ALLOWED_ORIGINS and LOKI_URL). Run with:  alloy run faro-only.alloy" write faro

{
  printf '# Local Loki: single process, files on disk, 14 days of retention. Generated from deploy/loki/loki.yaml.\n'
  printf '# Change limits_config.retention_period to keep logs longer (and give the disk room for it).\n'
  cat deploy/loki/loki.yaml
} > examples/logs/loki/loki-local.yaml
