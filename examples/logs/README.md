# Log history samples

Ready-to-copy configuration for the log pipeline described in [docs/logs.md](../../docs/logs.md): **Alloy** collects, **Loki** stores, **Grafana Faro** reports from browsers, **Grafana** (optional) raises alerts. Helmsman reads Loki for its history view. All of these were run against real Loki, Alloy and Grafana, and CI checks that they still parse.

## Which file do I want?

| You have | Use |
|---|---|
| One machine with your containers and web apps, and you want everything in one place | `alloy/all-in-one.alloy` + `loki/loki-local.yaml`, or just `compose/standalone.yml` |
| Several app hosts, Loki on one of them | `alloy/containers-only.alloy` on every app host, pushing to the Loki |
| Browsers must reach a public address, app hosts stay private | `alloy/faro-only.alloy` on the machine browsers reach |
| An app that prints JSON lines with a `level` field | `alloy/json-app-logs.alloy` (shows how to read the level from the JSON) |
| Logs that should survive losing the machine, or months of history | `loki/loki-s3.yaml` |
| A static web app you want to report browser errors | `faro/index.html` (plain page) or `faro/init.ts` (bundler) |
| nginx serving that app | `faro/nginx.conf` (JSON access lines, a health endpoint, errors to stderr) |
| Alerts | `compose/standalone.yml --profile alerts` and `grafana/provisioning/` |

The three `alloy/*.alloy` files (except `json-app-logs.alloy`) and `loki/loki-local.yaml` are **generated** from the files Helmsman deploys (`deploy/alloy`, `deploy/loki`) by `build-all-in-one.sh`, so the samples cannot drift from what is tested. Edit the originals and run the script.

## What the labels mean

Every line carries `source` (container or browser), `kind` (access, error, app, exception, event, measurement), `level` (error, warn, info, debug), and, for containers, `host`, `compose_project` and `compose_service`; for browsers, `app` and `env`. The table and the reasoning are in docs/logs.md. Two rules to remember:

- `HOST_NAME` in Alloy must equal the **agent id** of that machine in the Helmsman catalog.
- The Faro `app.name` must equal `logs.faro_app` in the catalog, and the Faro `app.environment` the environment's name.

## Try it in two minutes

```bash
cd examples/logs/compose
export DOCKER_GID=$(stat -c %g /var/run/docker.sock)   # macOS Docker Desktop: use 0
export GRAFANA_ADMIN_PASSWORD='choose-one'
docker compose -f standalone.yml --profile alerts up -d
```

Send a browser error the way a page would (the receiver is on port 12347):

```bash
curl -X POST localhost:12347/collect -H 'Origin: http://localhost:8080' -H 'Content-Type: application/json' -d '{
  "meta": {"sdk": {"name": "x", "version": "0"}, "app": {"name": "billing-web", "version": "1.4.2", "environment": "prod"}, "session": {"id": "s1"}},
  "exceptions": [{"type": "TypeError", "value": "payment widget crashed", "timestamp": "'"$(date -u +%Y-%m-%dT%H:%M:%S.000Z)"'"}]
}'
```

Look at it in Loki (a few seconds later), or in Grafana at http://localhost:3000 (user `admin`):

```bash
curl -G localhost:3100/loki/api/v1/query_range --data-urlencode 'query={source="browser", kind="exception", env="prod"}'
```

The provisioned rule *Browser exceptions in prod* turns to **firing** within about two minutes. Add a contact point in Grafana (Alerting > Contact points) so an alert reaches someone. The container rules fire on `level="error"` lines from the `api` service and `5xx` access lines from the `web` service: change those names in `grafana/provisioning/alerting/rules.yaml` to yours.

## Connecting Helmsman

In Helmsman, open **Integrations**, enter Loki's address as the Helmsman server reaches it (`http://loki:3100` inside the same Docker network, otherwise its hostname), **Test connection**, then **Save and turn on**.

## Notes

- Alloy reads the Docker socket, so give it only the compose projects you mean to ship (`LOG_PROJECTS`).
- The Faro receiver is open to the internet by design. Keep `FARO_ALLOWED_ORIGINS` to your own origins; Alloy rate limits it and caps the payload.
- Faro captures page URLs and console output. Use `beforeSend` (shown in the samples) to strip query strings and anything personal.
- Retention is set in the Loki file (`retention_period`): 14 days in `loki-local.yaml`, 90 in `loki-s3.yaml`.
