# Server install

Put Helmsman on a server with a public address, sign in with GitHub, and connect your first app. Plan on about an hour the first time. Every command is meant to be pasted as it is, with your own values where the text says so.

```
 browsers ─ https ─> nginx or Caddy ─> Helmsman (127.0.0.1:8080) ─> Postgres      (backup sidecar ─> S3 bucket)
 app servers: agent ─ wss ─> nginx or Caddy ─> Helmsman       (agents dial out, they open no inbound ports)
```

`portal.example.com` stands for your own DNS name throughout.

## Decide first

**Who handles HTTPS.** **Caddy** (bundled) gets its own Let's Encrypt certificate but needs ports 80 and 443 free. Use **your own nginx** if the server already runs a web server there: Caddy would fail with `address already in use`. Both are covered in step 6.

## 1. Prepare the server

You need an x86 or arm64 Linux server with a public IP, Docker Engine with Compose v2, and git. As root:

```bash
adduser --disabled-password --gecos "" deploy
usermod -aG docker deploy
mkdir -p /opt/helmsman /opt/helmsman-secrets
chown deploy:deploy /opt/helmsman
chmod 700 /opt/helmsman-secrets
```

Open ports **22** (SSH, keys only), **80** and **443**. Never open 8080 or 5432.

Create a DNS `A` record for `portal.example.com` and wait until `dig +short portal.example.com` returns the server IP.

## 2. Register Helmsman with GitHub

Helmsman needs **two separate** GitHub registrations. They are different kinds of app with different settings, and mixing them up is the most common first-day error.

| | Purpose | Kind | Goes in `.env` |
|---|---|---|---|
| **Sign-in** | People log in, and Helmsman reads their org and team membership | **OAuth App** | `GITHUB_OAUTH_CLIENT_ID`, `GITHUB_OAUTH_CLIENT_SECRET` |
| **Runs** | Starts migrations and seeds and reads workflow runs in your app repositories | **GitHub App** | `GITHUB_APP_ID` and a private key file |

**OAuth App** (Settings > Developer settings > OAuth Apps > New, under the org or your account):
- Homepage URL: `https://portal.example.com`
- Authorization callback URL: `https://portal.example.com/auth/github/callback`
- Generate a client secret. Keep the client ID and the secret.
- If the org restricts third-party apps, an org owner must approve it (Org settings > Third-party access). Otherwise sign-in fails as "not a member".

**GitHub App** (only needed for migrations, seeds and the Deployments tab):
- Permissions: Actions read and write, Contents read, Metadata read. Webhooks off. A callback URL is not needed.
- Generate a private key (a `.pem` download) and note the App ID.
- Install it on every repository Helmsman manages. An App owned by one org can only be installed on another org if you make it public; otherwise create the App in the org that owns the repositories.

Put the key on the server. **The server container runs as user 65532, so that user must own the file.** A key owned by your own user with mode 600 makes the portal say "the GitHub App private key could not be read".

```bash
scp github-app.pem deploy@portal.example.com:/tmp/github-app.pem
ssh -t deploy@portal.example.com 'sudo mv /tmp/github-app.pem /opt/helmsman-secrets/github-app.pem && sudo chown 65532:65532 /opt/helmsman-secrets/github-app.pem && sudo chmod 400 /opt/helmsman-secrets/github-app.pem'
```

Also note your numeric GitHub user ID, for the first admin: `gh api users/<your login> --jq .id`.

## 3. Get the files onto the server

```bash
sudo mkdir -p /opt/helmsman && sudo chown deploy:deploy /opt/helmsman
cd /opt/helmsman && git clone https://github.com/OWNER/helmsman-install.git .
cd deploy/server
```

Keep the folder layout: the compose file refers to `../backup`, `../loki`, `../alloy` and `../docker`.

## 4. Fill in `.env`

In `/opt/helmsman/deploy/server`:

```bash
cp env.example .env && chmod 600 .env
openssl rand -hex 32      # run it twice: one value for IDP_SESSION_KEY, one for IDP_ENCRYPTION_KEY
nano .env
```

| Variable | Value |
|---|---|
| `IDP_HOST`, `IDP_BASE_URL` | `portal.example.com` and `https://portal.example.com` |
| `COMPOSE_PROFILES` | `caddy,backup` with Caddy, or `backup` with your own nginx |
| `CADDY_TLS` | leave empty (Let's Encrypt) |
| `IMAGE_TAG` | uncomment the line (`latest`), or pin a version |
| `IDP_SESSION_KEY`, `IDP_ENCRYPTION_KEY` | the two random values |
| `IDP_ALLOWED_ORGS` | the GitHub org slug or slugs (comma separated) whose members may sign in |
| `IDP_BOOTSTRAP_ADMIN_GITHUB_IDS` | your numeric GitHub ID |
| `GITHUB_OAUTH_CLIENT_ID`, `GITHUB_OAUTH_CLIENT_SECRET` | from the OAuth App |
| `GITHUB_APP_ID`, `GITHUB_APP_KEY_FILE` | the GitHub App ID and `/opt/helmsman-secrets/github-app.pem` |
| `POSTGRES_PASSWORD`, `APP_DB_PASSWORD` | two different random passwords |

Check it: `bash preflight.sh` prints `preflight: ok` (a warning tells you if the key file has the wrong owner). Change `.env` and the new values reach the server only when you recreate it: `docker compose --env-file .env up -d --no-deps server`. A plain `docker restart` does not reload it.

## 5. Start Helmsman

The server image is pulled from `ghcr.io/poppyszn/helmsman-server` with the tag set by `IMAGE_TAG` (`latest` unless you pin a version). If the image is private, copy a token to your clipboard on your laptop (the `gh` login needs the `read:packages` scope once):
```bash
gh auth refresh -h github.com -s read:packages
gh auth token | pbcopy
```
Then on the server, paste it at the password prompt, pull, and log out straight away:
```bash
docker compose --env-file .env up -d postgres          # first start creates the limited helmsman_app role
docker login ghcr.io -u <your github login>
docker compose --env-file .env pull server
docker logout ghcr.io
docker compose --env-file .env up -d
docker compose --env-file .env ps                      # everything healthy
```
A classic personal access token with only `read:packages` is safer than your `gh` token if you prefer to make one.

## 6. HTTPS

### Caddy

With `caddy` in `COMPOSE_PROFILES`, Caddy was started above and gets its certificate in under a minute. Open `https://portal.example.com`.

### Your own nginx

Leave `caddy` out of `COMPOSE_PROFILES` (and run `docker compose --env-file .env rm -sf caddy` if it already tried to start). Helmsman listens on `127.0.0.1:8080`. In `/etc/nginx/conf.d/portal.conf`:

```nginx
# If nginx -t reports a duplicate "$connection_upgrade" map, delete this block and use the existing one.
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

limit_req_zone $binary_remote_addr zone=idp_auth:10m rate=30r/m;

server {
    listen 80;
    listen [::]:80;
    server_name portal.example.com;

    client_max_body_size 64k;

    proxy_set_header Host              $host;
    proxy_set_header X-Real-IP         $remote_addr;
    proxy_set_header X-Forwarded-For   $remote_addr;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;

    # Agents and live log streams are long-lived WebSockets. nginx drops the server-level
    # proxy_set_header lines inside a location that sets its own, so this one repeats them.
    # Without Host, the server sees 127.0.0.1:8080 and its same-origin check rejects the
    # browser with a 403 (live tail shows "connection refused").
    location ~ ^/(agent/ws|ws/) {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade           $http_upgrade;
        proxy_set_header Connection        $connection_upgrade;
        proxy_read_timeout  3600s;
        proxy_send_timeout  3600s;
        proxy_buffering off;
    }

    location /auth/ {
        limit_req zone=idp_auth burst=10 nodelay;
        proxy_pass http://127.0.0.1:8080;
    }

    location /agent/join {
        limit_req zone=idp_auth burst=10 nodelay;
        proxy_pass http://127.0.0.1:8080;
    }

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_read_timeout 70s;
    }
}
```

`deploy/nginx/idp.conf` here is the same configuration written for a certificate that already exists. Let certbot add HTTPS and the redirect to the file above:

```bash
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d portal.example.com        # choose the redirect option
curl -sI https://portal.example.com/readyz | head -3
```

Optionally add `add_header Strict-Transport-Security "max-age=63072000; includeSubDomains" always;` to the 443 block.

Open `https://portal.example.com` and sign in with GitHub.

## 7. Check the live server

Open **Setup check** in the sidebar (admins). It checks the public address, proxy headers, a WebSocket test from your browser, GitHub sign-in, the App key and installs, the catalog, every agent and the database and audit protection, each with a fix.

## 8. Connect an app

An app needs three things: an **agent** on the host that runs it, an **entry in the catalog**, and (for migrations and seeds) a **workflow** in its repository. The order matters, because the catalog rejects an environment whose agent does not exist yet.

### The agent

1. In the portal, **Agents > Create agent**. Name it after the host and keep the join token (single use, valid for an hour). The portal also shows the files to put on the host.
2. On the app's host run `agent/install.sh` from this repository (see the README). It writes `agent.yaml`, `compose.agent.yml` and `.env`, starts the agent, and removes the token once the agent has joined.
3. **Agents > Containers** lists what runs on the host and drafts the entries to expose services and to add them to the catalog.

More about the agent in [agent.md](agent.md).

### The catalog

Add the app to `deploy/server/apps.yaml` on the server. The server reloads it within 30 seconds, and a bad file is rejected while the last good one stays active (the error shows on **Admin > Catalog**).

The catalog tracks one container per environment, so an app with an API, an admin and a storefront is three entries, all with the same `repo`:

```yaml
version: 1
apps:
  - id: my-app-api
    name: My App API
    repo: my-org/my-app
    environments:
      - env: develop
        tier: nonprod
        agent: my-app-host           # the agent's name
        container: { compose_project: my-app-develop, service: api }
        url: https://api.my-app.example.com
        health: { url: https://api.my-app.example.com/health, via: server }
        workflows: { deploy: deploy.yml, migrate: helmsman-run.yml }
        github_environment: develop
```

`github_environment` is sent to the workflow as its `environment` input. Use `via: agent` for health URLs only the app host can reach, and list them under `health_urls` in `agent.yaml`.

### Migrations and seeds

Skip this if you only want status, logs and restarts: set `workflows.migrate` to any workflow file name, and do not grant `action.migrate` or `action.seed` in **Access**, so the buttons never appear.

Otherwise the repository needs a `workflow_dispatch` workflow that declares exactly the inputs Helmsman sends, `environment`, `command` (`migrate` or `seed`), `correlation_id` and `requested_by`, with the correlation id in its `run-name` (Helmsman finds its run by it). GitHub rejects a dispatch carrying an undeclared input, so an existing migrate workflow with only an `environment` input will fail. `examples/helmsman-run.yml` in this repository adds this for repositories that use the standard deploy pipeline, without changing their own files. Install the GitHub App on the repository, and set required reviewers on its `production` environment.

### Access

On **Access**, bind people and teams to roles. Team subjects are `org/team-slug`. A binding with `env: *` never matches a production environment, so production access is always an explicit binding. Production runs need approval from a second person with `action.approve`.

## Day to day

- **Update:** `git pull` in `/opt/helmsman` to refresh the files, pull the image (login, pull, logout if it is private), then `docker compose --env-file .env up -d`.
- **Server logs:** `docker compose --env-file .env logs -f server`.
- **Backups:** nightly. Check them and run a restore drill now and then. The bundled bucket is on the same disk as the database, so copy it to another machine from time to time.
- **Emergency access** if GitHub sign-in is down: the break-glass admin (set `IDP_BREAKGLASS_ENABLED` and its three values in `.env`).

## If something fails

Open **Setup check** in the sidebar first (admins). It runs the checks for the problems new installs hit most, each with its fix: the public address and proxy headers, a live WebSocket test from your browser, GitHub sign-in (including a GitHub App's client ID used by mistake), the App's key and installation on each repository, the catalog, every agent and every environment's container, the database role and the audit log protection. It changes nothing.


| Symptom | Cause and fix |
|---|---|
| `error while interpolating ... FARO_ALLOWED_ORIGINS / LOG_HOST_NAME / DOCKER_GID` | An older compose file. Update to the current one; these are only needed with the `logs` profile. |
| Caddy: `address already in use` on port 80 | Another web server owns 80 and 443. Remove `caddy` from `COMPOSE_PROFILES` and use nginx (step 6). |
| "This GitHub App must be configured with a callback URL" | `GITHUB_OAUTH_CLIENT_ID` holds a GitHub App's ID. Sign-in needs an OAuth App (step 2). |
| Sign-in says you are not a member | The user's org is not in `IDP_ALLOWED_ORGS`, or the org has not approved the OAuth App. |
| "Could not reach GitHub: the GitHub App private key could not be read" | The file must be owned by `65532:65532`, mode 400. If the path did not exist when the container first started, Docker made a directory there: remove it and put the file in place. Then recreate the server. |
| Live tail shows "connection refused"; nginx logs a 403 on `/ws/` | The nginx WebSocket location is missing `proxy_set_header Host $host`. Use the config in step 6. |
| `docker compose` says `no configuration file provided` | The file is not named `compose.yml`. Add `-f compose.agent.yml`. |
| Agent never connects, or logs a join error | The join token expired or was already used: create a new agent. Check `idp_url` and that `/agent/ws` reaches the server through nginx. |
| Catalog rejected: agent not registered | Create the agent first; its name in the catalog must match exactly. |
| Status `unknown` and actions disabled | The agent is offline. Check its logs. |
| Migrate or seed fails at dispatch ("unexpected inputs") | The repository's workflow does not declare the four inputs. Add `helmsman-run.yml` (from the Helmsman project). |
| A repo shows as not connected | The GitHub App is not installed on that repository. |
| Server healthy but the site does not load | DNS does not point at the server, or ports 80 and 443 are closed. |
