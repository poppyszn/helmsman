# Server setup

Put Helmsman on a server with a public address, sign in with GitHub, and connect your first app. Plan on about an hour the first time. Every command is meant to be pasted as it is, with your own values where the text says so.

```
 browsers ─ https ─> nginx or Caddy ─> Helmsman (127.0.0.1:8080) ─> Postgres      (backup sidecar ─> S3 bucket)
 app servers: agent ─ wss ─> nginx or Caddy ─> Helmsman       (agents dial out, they open no inbound ports)
```

`portal.example.com` stands for your own DNS name throughout.

## Decide two things first

| Decision | Options |
|---|---|
| **How files and updates reach the server** | **Pipeline**: GitHub Actions copies the repository and restarts the server on every merge to `main`. Needs SSH access from GitHub to the server. **By hand**: you copy a small zip of files and update when you choose. Works with a private repository and no SSH from GitHub, and is the quickest way to start. You can switch to the pipeline later. |
| **Who handles HTTPS** | **Caddy** (bundled): gets its own Let's Encrypt certificate, but needs ports 80 and 443 free. **Your own nginx**: use this if the server already runs nginx or anything else on 80 and 443. Caddy will fail with `address already in use`. |

Both choices are marked in the steps below. Keep the repository private; nothing here needs it public.

## 1. Prepare the server

You need an x86 or arm64 Linux server with a public IP, Docker Engine with Compose v2, and (for the pipeline) git. As root:

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

## 3. Put the files on the server

### Option A: by hand

On your laptop, in a checkout of the repository:

```bash
make server-bundle      # writes tmp/helmsman-server-files.zip
```

Copy the zip to the server (scp, SFTP, anything), then:

```bash
cd /opt/helmsman && unzip helmsman-server-files.zip
cd /opt/helmsman/helmsman/deploy/server
```

Keep the folder layout: the compose file refers to `../backup`, `../loki`, `../alloy` and `../docker`.

### Option B: the deploy pipeline

The workflow deploys `main` to a GitHub Environment called `develop` and `production` to one called `production`.

1. Make a key used only for deploying, and pin the server's host key:
   ```bash
   ssh-keygen -t ed25519 -N "" -C "helmsman-deploy" -f helmsman-deploy
   ssh-copy-id -i helmsman-deploy.pub deploy@portal.example.com
   ssh -i helmsman-deploy deploy@portal.example.com docker ps          # must work without a password
   ssh-keyscan -p 22 portal.example.com > known_hosts.txt
   ```
2. Create the environment and its secrets. The values never go through chat or a file in the repository:
   ```bash
   R=<owner>/helmsman
   gh api -X PUT repos/$R/environments/develop >/dev/null
   gh secret set SSH_HOST        --env develop -R $R --body "portal.example.com"
   gh secret set SSH_USER        --env develop -R $R --body "deploy"
   gh secret set WORKING_DIR     --env develop -R $R --body "/opt/helmsman"
   gh secret set SSH_PRIVATE_KEY --env develop -R $R < helmsman-deploy
   gh secret set SSH_KNOWN_HOSTS --env develop -R $R < known_hosts.txt
   gh variable set HEALTH_URL    --env develop -R $R --body "https://portal.example.com/readyz"
   # only if SSH is not on port 22:  gh secret set SSH_PORT --env develop -R $R --body "2222"
   ```
   Then delete the private key from your laptop.
3. Run **Actions > Deploy > Run workflow** on `main`. It builds the image, puts the repository at `/opt/helmsman/helmsman` and **stops at the preflight step** because there is no `.env` yet. That is expected.
4. `cd /opt/helmsman/helmsman/deploy/server` on the server and continue below.

## 4. Fill in `.env`

In `/opt/helmsman/helmsman/deploy/server`:

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
| `IMAGE_TAG` | by hand only: uncomment the line (`latest`). With the pipeline leave it commented out |
| `IDP_SESSION_KEY`, `IDP_ENCRYPTION_KEY` | the two random values |
| `IDP_ALLOWED_ORGS` | the GitHub org slug or slugs (comma separated) whose members may sign in |
| `IDP_BOOTSTRAP_ADMIN_GITHUB_IDS` | your numeric GitHub ID |
| `GITHUB_OAUTH_CLIENT_ID`, `GITHUB_OAUTH_CLIENT_SECRET` | from the OAuth App |
| `GITHUB_APP_ID`, `GITHUB_APP_KEY_FILE` | the GitHub App ID and `/opt/helmsman-secrets/github-app.pem` |
| `POSTGRES_PASSWORD`, `APP_DB_PASSWORD` | two different random passwords |

Check it: `bash preflight.sh` prints `preflight: ok` (a warning tells you if the key file has the wrong owner). Change `.env` and the new values reach the server only when you recreate it: `docker compose --env-file .env up -d --no-deps server`. A plain `docker restart` does not reload it.

## 5. Start Helmsman

The images are GitHub packages (`ghcr.io/poppyszn/helmsman-server` and `helmsman-agent`). Make them **public** in their package settings (Package settings > Danger Zone > Change visibility): package visibility is separate from the repository's, so the repository stays private, and hosts then pull with no login. The tag is `latest` when you copy files by hand (it exists once the **Publish: Images** workflow has run on `main`), or the commit SHA the pipeline built.

**By hand.**
```bash
docker compose --env-file .env up -d postgres          # first start creates the limited helmsman_app role
docker compose --env-file .env pull server
docker compose --env-file .env up -d
docker compose --env-file .env ps                      # everything healthy
```

**If a package is private**, log in around the pull and log out straight away. Copy a token to your clipboard on your laptop (the `gh` login needs the `read:packages` scope once):
```bash
gh auth refresh -h github.com -s read:packages
gh auth token | pbcopy
```
Then on the host, paste it at the password prompt:
```bash
docker login ghcr.io -u <your github login>
docker compose --env-file .env pull server
docker logout ghcr.io
```
A classic personal access token with only `read:packages` is safer than your `gh` token if you prefer to make one.

**Pipeline.** The pipeline only restarts the `server` service, so the first time start the database yourself:
```bash
docker compose --env-file .env up -d postgres
```
Run **Deploy** again from GitHub (it now passes preflight, pulls the image and migrates the database), then start the rest:
```bash
export IMAGE_TAG=$(cat /opt/helmsman/helmsman.develop.deployed-sha)
docker compose --env-file .env up -d
docker compose --env-file .env ps
```
The run's last step checks `/readyz`, which fails on this one run if HTTPS is not set up yet.

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

`deploy/nginx/idp.conf` is the same configuration written for a certificate that already exists. Let certbot add HTTPS and the redirect to the file above:

```bash
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d portal.example.com        # choose the redirect option
curl -sI https://portal.example.com/readyz | head -3
```

Optionally add `add_header Strict-Transport-Security "max-age=63072000; includeSubDomains" always;` to the 443 block. `make e2e-server` checks for it.

Open `https://portal.example.com` and sign in with GitHub.

## 7. Check the live server

From your laptop, in the repository, `make e2e-server` checks without changing anything: HTTPS and the certificate, the redirect, the security headers, that the sign-in redirect uses state and PKCE with the right callback URL, that the API is closed without a session, and that an agent cannot connect without a credential. Set `E2E_SERVER_URL` if your name differs from the default. To also test the signed-in pages, turn on the break-glass admin (see the runbook) and pass `E2E_BREAKGLASS_PASSWORD` and `E2E_BREAKGLASS_TOTP_SECRET`.

## 8. Connect an app

An app needs three things: an **agent** on the host that runs it, an **entry in the catalog**, and (for migrations and seeds) a **workflow** in its repository. The order matters, because the catalog rejects an environment whose agent does not exist yet.

### The agent

1. In the portal, **Agents > Create agent**. Name it after the host. Copy the join token: it is single use and valid for an hour.
2. On the app's host, make a folder with `agent.yaml` (from `examples/agent.yaml`) and `compose.agent.yml` (from `deploy/`). Allow only what you want exposed, using the real compose labels:
   ```bash
   docker ps --format '{{.Names}}  {{.Label "com.docker.compose.project"}}  {{.Label "com.docker.compose.service"}}'
   ```
   ```yaml
   idp_url: wss://portal.example.com/agent/ws
   allow:
     - compose_project: my-app-develop        # the project label, exactly
       services: [api, admin]                 # the service labels
       actions: [logs, restart]
   ```
3. Create `.env` next to them (`nano`, not the shell history):
   ```bash
   cat > .env <<'EOF'
   IDP_JOIN_TOKEN=
   AGENT_VERSION=latest
   DOCKER_GID=
   EOF
   chmod 600 .env
   sed -i "s/^DOCKER_GID=.*/DOCKER_GID=$(stat -c %g /var/run/docker.sock)/" .env
   nano .env        # paste the join token
   ```
4. The compose file is not called `compose.yml`, so name it. With the agent package public there is no login:
   ```bash
   docker compose -f compose.agent.yml --env-file .env pull
   docker compose -f compose.agent.yml --env-file .env up -d
   docker compose -f compose.agent.yml --env-file .env logs --tail 20      # "joined", then "connected"
   ```
   If the package is private, run `docker login ghcr.io -u <your github login>` before the pull and `docker logout ghcr.io` after it.
   The agent keeps its own credential in its data volume, so remove `IDP_JOIN_TOKEN` from `.env` afterwards. Changing `agent.yaml` needs `up -d --force-recreate`, because the agent reads it at start.

### The catalog

Add the app to `deploy/server/apps.yaml` (fields in `catalog.md`). With the pipeline, send it in a pull request. By hand, edit the file on the server. The server reloads it within 30 seconds, and a bad file is rejected while the last good one stays active (the error shows on **Admin > Catalog**).

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

Otherwise the repository needs a `workflow_dispatch` workflow that declares exactly the inputs Helmsman sends, `environment`, `command` (`migrate` or `seed`), `correlation_id` and `requested_by`, with the correlation id in its `run-name` (Helmsman finds its run by it). GitHub rejects a dispatch carrying an undeclared input, so an existing migrate workflow with only an `environment` input will fail. `examples/workflows/helmsman-run.yml` adds this for repositories that use the standard deploy pipeline, without changing their own files. Install the GitHub App on the repository, and set required reviewers on its `production` environment.

### Access

On **Access**, bind people and teams to roles. Team subjects are `org/team-slug`. A binding with `env: *` never matches a production environment, so production access is always an explicit binding. By default production runs need approval from a second person with `action.approve`; set `require_approval: false` on an environment in the catalog to skip it (see `catalog.md`).

## Day to day

- **Update, by hand:** `make server-bundle`, copy and unzip again (your `.env` is never in the zip), pull the image (login, pull, logout), then `docker compose --env-file .env up -d`.
- **Update, pipeline:** merge to `main`. **Roll back:** Actions > Deploy > Run workflow with an older `commit_sha`.
- **Server logs:** `docker compose --env-file .env logs -f server`.
- **Backups:** nightly. Check them and run a restore drill monthly (see the runbook). The bundled bucket is on the same disk as the database, so copy it to another machine now and then.
- **Emergency access** if GitHub sign-in is down: the break-glass admin in the runbook.

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
| `manifest unknown` or `denied` when pulling | The tag does not exist yet (wait for **Publish: Images**, or set `IMAGE_TAG`), or the package is private and you are not logged in to `ghcr.io` (make it public, or log in). |
| Agent never connects, or logs a join error | The join token expired or was already used: create a new agent. Check `idp_url` and that `/agent/ws` reaches the server through nginx. |
| Catalog rejected: agent not registered | Create the agent first; its name in the catalog must match exactly. |
| Status `unknown` and actions disabled | The agent is offline. Check its logs. |
| Migrate or seed fails at dispatch ("unexpected inputs") | The repository's workflow does not declare the four inputs. Add `examples/workflows/helmsman-run.yml`. |
| A repo shows as not connected | The GitHub App is not installed on that repository. |
| Pipeline fails at "missing secrets" | The environment is named `develop` and needs `SSH_HOST`, `SSH_USER`, `SSH_PRIVATE_KEY`, `SSH_KNOWN_HOSTS` and `WORKING_DIR`. |
| `Host key verification failed` | Run `ssh-keyscan` again and reset `SSH_KNOWN_HOSTS`. |
| Pipeline stops at preflight | The message names what is missing from `.env`. |
| `.env must be listed in .gitignore` | Do not rename or move `deploy/server/.env`. |
| Server healthy but the site does not load | DNS does not point at the server, or ports 80 and 443 are closed. |
