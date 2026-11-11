# Log history

Helmsman shows two kinds of logs for each environment:

- **Live tail**: straight from Docker through the agent, as before. No delay, nothing stored.
- **History**: stored in Loki for 14 days, filled by Alloy. This is where you look when something broke an hour ago.

Both are in the **logs** tab, switched with **live tail | history**. History needs the `logs` profile (Loki and Alloy). Without it the history view says it is not set up and live tail keeps working.

```
 container logs ─ Alloy (each host) ─┐
                                      ├─> Loki (14 days) <── Helmsman (history view)
 browser ─ Grafana Faro ─> Caddy /faro ─> Alloy ─┘                 └─ Grafana (alerts, optional)
```

## Two sources, and how to tell them apart

A static web app is two things at once. Its nginx container writes an **access line for every request**. Its pages, running in browsers, report their own **console messages, errors and events** through Grafana Faro. Those are different logs about the same app, so every line carries labels that say which it is:

| Label | Values | Meaning |
|---|---|---|
| `source` | `container`, `browser` | Where the line came from: the container's output, or a browser running the app |
| `kind` | `access` | an nginx request line (default `combined` format, or JSON with a `status` field) |
| | `error` | the nginx error log (`[error]`, `[warn]`, `[notice]` ...) |
| | `app` | anything else the container prints, and `console` messages from the browser |
| | `exception` | an uncaught error in a browser (Faro) |
| | `event` | something a browser reported, such as a click or a navigation (Faro) |
| | `measurement` | browser performance, such as web vitals (Faro) |
| `level` | `error`, `warn`, `info`, `debug` | Taken from the status code for access lines (5xx error, 4xx warn), from nginx's own level, from the usual words (`error`, `warn`, `debug`) in other output, and from the Faro level. An exception is always `error`. |
| `compose_project`, `compose_service`, `host` | | Which container. `host` is the agent id of the machine, so staging and prod running the same compose project on two hosts never mix. |
| `app`, `env` | | Which web app and environment, for browser lines (from the Faro setup). |

Only these labels exist. Status codes, URLs and session ids stay in the line, so Loki stays small.

### The history view

- **source**: container, browser. The browser chip is disabled for an app with no `logs.faro_app` in the catalog.
- **type**: access, server errors, messages, exceptions, events, measurements. **Access starts hidden**, because on a static site it is most of the volume. One click shows it.
- **level**: error, warn, info, debug.
- **range**: 15 minutes up to 7 days. **search** finds text in the line (case insensitive, plain text, not a pattern).
- Browser lines are shown readably: an exception as `TypeError: x is undefined`, an event as `button_click id=pay`, with the page, app version and session underneath. Container lines keep their colours.
- **load older** pages further back. **auto refresh** polls every 10 seconds.

The **live tail** has a **hide access lines** switch for the same reason.

## Setting it up

### Dev

```bash
make up-logs
```

This starts Loki, Alloy (container logs and the Faro receiver), and a small nginx app, `static-demo`, whose page posts browser telemetry. Open `static-demo`, then **logs > history**. The page is at http://localhost:9003; the **Send a browser error** button creates an exception. `make e2e` includes the history tests when this stack is running.

### The Helmsman server

1. Add `logs` to `COMPOSE_PROFILES` in `deploy/server/.env` and set (see `env.example`):
   ```
   IDP_LOKI_URL=http://loki:3100
   DOCKER_GID=999                      # stat -c %g /var/run/docker.sock
   LOG_HOST_NAME=<this host's agent id>
   LOG_PROJECTS=.+                     # compose projects whose container logs are collected
   FARO_ALLOWED_ORIGINS=https://my-web-app.example.com
   ```
2. If browsers or other hosts will send logs, enable the public routes: copy `deploy/server/caddy/logs/logs.caddy.example` to `logs.caddy` (it is not committed, so a deploy leaves it alone). It adds `/faro/*` for browsers and an authenticated `/loki/api/v1/push` for Alloy on other hosts. Create the push password hash with `docker run --rm caddy caddy hash-password --plaintext '<password>'` and set `LOKI_PUSH_USER` and `LOKI_PUSH_PASSWORD_HASH` (single quoted) in `.env`.
3. Start it from `deploy/server`: `docker compose --env-file .env up -d`. Loki's query API is never exposed through Caddy; only the push path, behind a password.

`LOG_HOST_NAME` must equal the `agent:` of the environments on this host in `deploy/server/apps.yaml`. If they differ, the history view is empty.

### Other hosts

On each app host, next to the agent (see `deploy/compose.alloy.yml`):

```bash
export ALLOY_HOST_NAME=billing-host-1                               # this host's agent id
export LOKI_URL=https://helmsman.example.com/loki/api/v1/push
export LOKI_PASSWORD='...'                                          # the password behind LOKI_PUSH_PASSWORD_HASH
export LOG_PROJECTS='billing|ledger'                                # only these compose projects leave the host
export DOCKER_GID=$(stat -c %g /var/run/docker.sock)
docker compose -f deploy/compose.alloy.yml up -d
```

List only the projects the agent allowlist names. Alloy reads the Docker socket the way the agent does, so treat the host the same way.

### The catalog

Container history needs nothing in the catalog: it uses `container.compose_project`, `container.service` and the agent id. For a web app that also sends browser telemetry, name its Faro app:

```yaml
- env: staging
  agent: billing-host-1
  container: { compose_project: billing, service: web }
  logs: { faro_app: billing-web }
```

## Adding Faro to a static app

Add the Grafana Faro Web SDK to the page and point it at the server's `/faro/collect`. For a plain static page, in `index.html`:

```html
<script src="https://unpkg.com/@grafana/faro-web-sdk@2.14.0/dist/bundle/faro-web-sdk.iife.js"></script>
<script>
  window.GrafanaFaroWebSdk.initializeFaro({
    url: "https://helmsman.example.com/faro/collect",
    app: { name: "billing-web", version: "1.4.2", environment: "staging" },
  });
</script>
```

With a bundler: `npm i @grafana/faro-web-sdk`, then `initializeFaro({ url, app })` once at startup.

Three things must match, or the lines will not show up in Helmsman:

- `app.name` equals `logs.faro_app` in the catalog.
- `app.environment` equals the **env name** in the catalog (`staging`, `prod`).
- The page's origin is listed in `FARO_ALLOWED_ORIGINS`.

The Faro endpoint is public by nature, so Alloy rate limits it (50 requests a second) and caps the payload at 512 KiB, and only the allowed origins may post. Faro records page URLs and console output, so do not put personal data in either.

### Nicer access lines (optional)

nginx's default access format works. If you would rather have JSON, which keeps fields tidy, set it in the app's nginx config; Alloy recognises a line with `status` and `request`:

```nginx
log_format json escape=json '{"time":"$time_iso8601","remote":"$remote_addr","request":"$request","status":$status,"bytes":$body_bytes_sent,"ms":"$request_time"}';
access_log /dev/stdout json;
```

## Grafana and alerts

Helmsman only reads Loki for history. For alerts, run Grafana and add Loki as a data source. Loki is not published outside the compose network, so Grafana joins it (or you tunnel to port 3100). Because the labels are fixed, alert rules are short. In LogQL:

```
# server errors in an app's container in the last 5 minutes
sum(count_over_time({source="container", compose_service="api", level="error"}[5m])) > 10

# a static site returning 5xx
sum(count_over_time({kind="access", level="error", compose_service="web"}[5m])) > 5

# uncaught browser errors in prod
sum(count_over_time({source="browser", kind="exception", app="billing-web", env="prod"}[5m])) > 0
```

## Who can read history

Reading history needs `logs.view` on that environment, the same as the live tail. Helmsman, not the browser, queries Loki, and it builds the selector itself from the catalog (project, service, host, Faro app, environment). The only free text a user can supply is the search box, which is escaped as a literal. Opening history is audited (`logs.history`: who, which environment, which filters) and never records the search text or any log content. History requests share the per-user limit of 60 log opens a minute. Retention is 14 days (`deploy/loki/loki.yaml`); lines can contain personal data, so grant `logs.view` on prod narrowly.

## When history is empty

| Symptom | Check |
|---|---|
| "Log history is not set up" | `IDP_LOKI_URL` is empty on the server, or the `logs` profile is off |
| No container lines | `docker compose logs alloy`. `LOG_HOST_NAME` must equal the agent id, `LOG_PROJECTS` must match the compose project, and the container needs Docker compose labels |
| Container lines but no browser lines | `app.name` and `app.environment` in the Faro setup, `logs.faro_app` in the catalog, and `FARO_ALLOWED_ORIGINS`. In the browser's network tab the post to `/faro/collect` should answer 202 |
| Lines appear a few seconds late | Normal: Alloy batches, and Loki indexes before it serves |
| Access lines missing | They are hidden by default. Turn on the **access** type |
