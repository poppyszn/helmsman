# Home pilot

Run Helmsman on a server at home, with no domain, no public IP and no AWS. It uses the same production compose file as a real deployment plus a small overlay (`deploy/compose.pilot.yml`) that adds:

- **Caddy** for TLS. It acts as its own certificate authority, so no domain or Let's Encrypt is needed.
- **Adobe S3Mock** as an S3 compatible bucket for backups.
- A **backup sidecar** that dumps Postgres to that bucket every night and keeps 14 days.

Everything runs in Docker. The host needs Docker with Compose v2, git, and a LAN address your laptop can reach.

## What the pilot does not give you

- **S3Mock is a test double on the same machine.** It protects you from a bad upgrade or a corrupted database. It does not protect you from losing the disk. Copy the backup volume to another machine now and then (command below).
- **The certificate is private.** Each browser and each agent host must trust Caddy's root certificate once. Without that you get certificate warnings, and agents refuse to connect, which is correct.
- **It is reachable on your LAN only.** Do not forward ports 80 or 443 from your router.

## 1. Pick an address

Browsers and agents reach the server by one name or IP, called `IDP_HOST` below. Use the server's fixed LAN IP, or a name your router or `/etc/hosts` resolves, for example `homeserver.local`. Reserve that IP in your router so it does not change. GitHub sends you back to `https://IDP_HOST/auth/github/callback` after sign-in, and your browser, not GitHub, opens it, so a private address works.

## 2. GitHub setup (once)

1. **OAuth App** for sign-in: https://github.com/organizations/pops-personal-org/settings/applications/new. Homepage `https://IDP_HOST`, callback `https://IDP_HOST/auth/github/callback`. Note the client ID and generate a client secret. In the organization's third-party access policy, approve the app if it asks.
2. **GitHub App** for migrations and seeds: already created for the real-GitHub test. Reuse its ID and private key.
3. Find your numeric GitHub user ID for the first admin: `gh api users/<your login> --jq .id`.

## 3. Get the code and build the images

On the home server:

```bash
git clone https://github.com/poppyszn/helmsman.git /opt/helmsman && cd /opt/helmsman
docker build -f deploy/docker/server.Dockerfile -t helmsman-server:pilot .
docker build -f deploy/docker/agent.Dockerfile -t helmsman-agent:pilot .
```

The repository is private, so use a GitHub login on the server (`gh auth login`) or a deploy key. Building locally avoids needing a published image. For a real deployment you would pin an image digest instead (see the runbook).

## 4. Configure

```bash
cp deploy/env.server.example .env && chmod 600 .env
cat deploy/env.pilot.example >> .env
mkdir -p /opt/helmsman/secrets /opt/helmsman/catalog
cp /path/to/github-app.pem /opt/helmsman/secrets/github-app.pem && chmod 600 /opt/helmsman/secrets/github-app.pem
```

Edit `.env`:

| Variable | Value |
|---|---|
| `IDP_IMAGE` | `helmsman-server:pilot` |
| `IDP_HOST`, `IDP_BASE_URL` | your address, and `https://` plus the same address |
| `IDP_SESSION_KEY`, `IDP_ENCRYPTION_KEY` | `openssl rand -hex 32` each |
| `IDP_ALLOWED_ORGS` | `pops-personal-org` |
| `IDP_BOOTSTRAP_ADMIN_GITHUB_IDS` | your numeric ID from step 2 |
| `IDP_GITHUB_MODE` | `real` |
| `GITHUB_OAUTH_CLIENT_ID`, `GITHUB_OAUTH_CLIENT_SECRET` | from the OAuth App |
| `GITHUB_APP_ID`, `GITHUB_APP_KEY_FILE` | the App ID and `/opt/helmsman/secrets/github-app.pem` |
| `CATALOG_DIR` | `/opt/helmsman/catalog` |
| `POSTGRES_PASSWORD`, `APP_DB_PASSWORD` | two different random passwords |

Start with an empty catalog so the server boots before any agent exists:

```bash
printf 'version: 1\napps: []\n' > /opt/helmsman/catalog/apps.yaml
```

## 5. Start

```bash
docker compose -f deploy/compose.server.yml -f deploy/compose.pilot.yml --env-file .env up -d --build
docker compose -f deploy/compose.server.yml -f deploy/compose.pilot.yml --env-file .env ps
```

Everything should be `healthy` within a minute. The backup sidecar takes the first backup right away, then one a night at `BACKUP_TIME`.

## 6. Trust the certificate

Copy Caddy's root certificate out of its volume:

```bash
docker compose -f deploy/compose.server.yml -f deploy/compose.pilot.yml --env-file .env cp caddy:/data/caddy/pki/authorities/local/root.crt ./idp-root.crt
```

Install it on each machine that opens the UI: on macOS, open the file and add it to the System keychain, then set it to Always Trust. On Windows, import it into Trusted Root Certification Authorities. On iOS or Android, install it as a profile or CA certificate. Then open `https://IDP_HOST` and sign in with GitHub.

## 7. Add your containers

1. List what is running on the server, to see the compose project and service of each app:
   ```bash
   docker ps --format '{{.Label "com.docker.compose.project"}} {{.Label "com.docker.compose.service"}}' | sort -u
   ```
2. In the UI, open **Agents**, create an agent (for example `home`), and copy its join token. It expires in one hour and works once.
3. On the same server (or any other host), write `agent.yaml` next to `deploy/compose.agent.yml`, naming only the services you want exposed. See `examples/agent.yaml`.
   ```yaml
   idp_url: wss://IDP_HOST/agent/ws
   allow:
     - compose_project: myproject
       services: [web, api]
       actions: [logs, restart]
   ```
4. Put the root certificate next to it as `deploy/idp-root.crt`, then start the agent with the CA overlay:
   ```bash
   export AGENT_IMAGE=helmsman-agent:pilot DOCKER_GID=$(stat -c %g /var/run/docker.sock) IDP_JOIN_TOKEN=<token>
   docker compose -f deploy/compose.agent.yml -f deploy/compose.agent.ca.yml up -d
   ```
   The agent keeps its credential in the `agent-data` volume, so the join token is only needed once.
5. Add the apps to `/opt/helmsman/catalog/apps.yaml` (see `docs/catalog.md`). The reload is picked up within 30 seconds, or use the Catalog page. For migrations and seeds, each app needs a repository the GitHub App is installed on, with a migrate workflow like `examples/workflows/migrate.yml`.

## Backups

The sidecar runs these commands inside the compose project:

```bash
BK="docker compose -f deploy/compose.server.yml -f deploy/compose.pilot.yml --env-file .env run --rm backup"
$BK list                         # show the stored backups
$BK once                         # take one now
$BK drill                        # backup, restore into a scratch database, compare, verify the guarantees
RESTORE_CONFIRM=yes $BK restore latest   # replace the live database (then restart the idp service)
```

Run `drill` after the first day and then monthly. To keep a copy off this machine:

```bash
docker run --rm -v helmsman_s3-data:/data -v "$PWD":/out alpine tar czf /out/helmsman-backups.tgz -C /data .
```

Adjust the volume name to `docker volume ls | grep s3-data`. Move the archive to a NAS or another computer.

## Updating

```bash
cd /opt/helmsman && git pull
docker build -f deploy/docker/server.Dockerfile -t helmsman-server:pilot .
docker compose -f deploy/compose.server.yml -f deploy/compose.pilot.yml --env-file .env up -d
```

Migrations run before the server starts. Take a backup first with `$BK once`.

## Troubleshooting

- **Browser warns about the certificate**: the root certificate is not trusted on that machine yet (step 6).
- **Agent says `x509: certificate signed by unknown authority`**: the agent does not have `idp-root.crt` mounted, or `IDP_HOST` in `idp_url` differs from the name on the certificate. Use exactly the same name or IP.
- **Agent connects but shows `docker_unavailable`**: `DOCKER_GID` does not match the group of `/var/run/docker.sock`.
- **Sign-in loops back with an error**: the OAuth App callback URL must match `https://IDP_HOST/auth/github/callback` exactly. Your GitHub account must be in `pops-personal-org`.
- **Migrations are disabled for a repository**: open the GitHub page in the admin menu and read the reason.
