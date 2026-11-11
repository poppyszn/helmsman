# Server setup

Put Helmsman on a server with a public address and deploy it from GitHub Actions. After this, every merge to `main` updates the server by itself.

```
 push to main ─> GitHub Actions ─ build image ─> ghcr.io/poppyszn/helmsman-server:<sha>
                      │
                      └─ SSH ─> your server:  sync repo, preflight, docker compose up server
 browsers ─ https ─> Caddy ─> Helmsman ─> Postgres        (backup sidecar ─> S3 bucket)
 app servers: agent ─ wss ─> Caddy ─> Helmsman             (agents dial out, no inbound ports)
```

You need: an x86 Linux server with a public IP, the DNS name `helmsman.vasconsolutionsapp.com` pointing at it, and admin rights on `poppyszn/helmsman` and the `pops-personal-org` organization. About 30 minutes.

## 1. Prepare the server

Install Docker Engine with Compose v2 and git. Then, as root:

```bash
adduser --disabled-password --gecos "" deploy
usermod -aG docker deploy
mkdir -p /opt/helmsman /opt/helmsman-secrets
chown deploy:deploy /opt/helmsman
chown deploy:deploy /opt/helmsman-secrets && chmod 700 /opt/helmsman-secrets
```

Open ports **22** (SSH, key only), **80** and **443** in the firewall or cloud security group. Do not open 8080 or 5432.

Point a DNS `A` record for `helmsman.vasconsolutionsapp.com` at the server and wait until `dig +short helmsman.vasconsolutionsapp.com` returns its IP. Caddy needs this to get a Let's Encrypt certificate.

## 2. Give GitHub Actions a way in

On your laptop, make a key used only for deploying Helmsman:

```bash
ssh-keygen -t ed25519 -N "" -C "helmsman-deploy" -f helmsman-deploy
ssh-copy-id -i helmsman-deploy.pub deploy@helmsman.vasconsolutionsapp.com    # or paste the .pub into /home/deploy/.ssh/authorized_keys
ssh -i helmsman-deploy deploy@helmsman.vasconsolutionsapp.com docker ps      # must work without a password
ssh-keyscan -p 22 helmsman.vasconsolutionsapp.com > known_hosts.txt          # pins the server's host key
```

## 3. GitHub: sign-in and runs

1. **OAuth App** (sign-in): https://github.com/organizations/pops-personal-org/settings/applications/new. Homepage `https://helmsman.vasconsolutionsapp.com`. Callback `https://helmsman.vasconsolutionsapp.com/auth/github/callback`. Keep the client ID, and generate a client secret.
2. **GitHub App** (migrations and seeds): reuse the one you made for the test. It needs Actions read and write, Contents read, Metadata read, and must be installed on every app repository. Copy its private key to the server. The server container runs as user 65532, so that user must own the file; a key owned by `deploy` with mode 600 gives "the GitHub App private key could not be read":
   ```bash
   scp github-app.pem deploy@helmsman.vasconsolutionsapp.com:/opt/helmsman-secrets/github-app.pem
   ssh deploy@helmsman.vasconsolutionsapp.com 'sudo chown 65532:65532 /opt/helmsman-secrets/github-app.pem && sudo chmod 400 /opt/helmsman-secrets/github-app.pem'
   ```
3. Your numeric GitHub user ID, for the first admin: `gh api users/<your login> --jq .id`.

## 4. GitHub: the `develop` environment

The workflow deploys `main` to a GitHub Environment named `develop`. Create it and add the secrets. Run these from the folder holding the key files (the values never go through a chat or a file in the repository):

```bash
R=poppyszn/helmsman
gh api -X PUT repos/$R/environments/develop >/dev/null
gh secret set SSH_HOST        --env develop -R $R --body "helmsman.vasconsolutionsapp.com"
gh secret set SSH_USER        --env develop -R $R --body "deploy"
gh secret set WORKING_DIR     --env develop -R $R --body "/opt/helmsman"
gh secret set SSH_PRIVATE_KEY --env develop -R $R < helmsman-deploy
gh secret set SSH_KNOWN_HOSTS --env develop -R $R < known_hosts.txt
gh variable set HEALTH_URL    --env develop -R $R --body "https://helmsman.vasconsolutionsapp.com/readyz"
# only if SSH is not on port 22:  gh secret set SSH_PORT --env develop -R $R --body "2222"
```

Then delete the private key from your laptop (`shred -u helmsman-deploy` or move it into a password manager).

The workflow builds on an x86 runner (`runner: ubuntu-latest`), which matches your server.

## 5. First deploy and bootstrap

The pipeline only restarts the `server` service. Postgres and the others are started once by hand, so the first deploy happens in two passes.

1. **First run.** In GitHub, open **Actions > Deploy > Run workflow** on `main`. It builds the image and puts the repository on the server at `/opt/helmsman/helmsman`. It then **stops at the preflight step** because there is no `.env` yet. That is expected.
2. **Create the host's `.env`**, on the server:
   ```bash
   cd /opt/helmsman/helmsman/deploy/server
   cp env.example .env && chmod 600 .env
   openssl rand -hex 32   # run twice: one for IDP_SESSION_KEY, one for IDP_ENCRYPTION_KEY
   nano .env
   ```
   Fill in:

   | Variable | Value |
   |---|---|
   | `IDP_HOST`, `IDP_BASE_URL` | `helmsman.vasconsolutionsapp.com` and `https://helmsman.vasconsolutionsapp.com` |
   | `CADDY_TLS` | leave empty (Let's Encrypt) |
   | `IDP_SESSION_KEY`, `IDP_ENCRYPTION_KEY` | the two random values |
   | `IDP_ALLOWED_ORGS` | `pops-personal-org` |
   | `IDP_BOOTSTRAP_ADMIN_GITHUB_IDS` | your numeric ID |
   | `GITHUB_OAUTH_CLIENT_ID`, `GITHUB_OAUTH_CLIENT_SECRET` | from the OAuth App |
   | `GITHUB_APP_ID` | the GitHub App ID |
   | `GITHUB_APP_KEY_FILE` | `/opt/helmsman-secrets/github-app.pem` |
   | `POSTGRES_PASSWORD`, `APP_DB_PASSWORD` | two different random passwords |

   Keep `COMPOSE_PROFILES=caddy,backup`. Check it: `bash preflight.sh` prints `preflight: ok`.
3. **Start the database**: `docker compose --env-file .env up -d postgres`. The first start creates the limited `helmsman_app` role.
4. **Second run.** Run **Deploy** again. It now passes preflight, pulls the image and starts the server, which migrates the database. The run's last step checks `https://helmsman.vasconsolutionsapp.com/readyz` (Caddy is not up yet, so that check may fail on this one run only; go on to the next step and re-run once).
5. **Start the rest**: Caddy, the bucket and the backup sidecar. They run under the same image tag the pipeline used:
   ```bash
   export IMAGE_TAG=$(cat /opt/helmsman/helmsman.develop.deployed-sha)
   docker compose --env-file .env up -d
   docker compose --env-file .env ps        # everything healthy
   ```
   Caddy gets its certificate in under a minute. Open `https://helmsman.vasconsolutionsapp.com` and sign in with GitHub.

From now on a merge to `main` is the whole deploy. The environment's `.env` is never touched by it.

## 6. Check the live server with the end-to-end tests

The Playwright suite has a project for a real deployment. From your laptop, in the repository:

```bash
make e2e-server
```

This checks, without changing anything: HTTPS and the certificate, the HTTP to HTTPS redirect, the security headers, that the GitHub sign-in redirect uses state and PKCE with the right callback URL, that the API is closed without a session, that an agent cannot connect without a credential, and that another website cannot open a log stream.

To also test the signed-in pages, turn on the break-glass admin (see the runbook), then:

```bash
E2E_BREAKGLASS_PASSWORD='...' E2E_BREAKGLASS_TOTP_SECRET='...' make e2e-server
```

That signs in, visits every admin page, checks that the catalog loaded and no agent has an alert, confirms the sign-in was audited, and signs out. It never tries a wrong password, because five failures lock break-glass for 15 minutes. Turn break-glass off again afterwards if you do not want it enabled.

## 7. Add your servers and apps

1. In the UI, **Agents > create agent**, for each host that runs apps. Copy the join token (single use, valid one hour).
2. On that host, make `agent.yaml` listing only the services to expose (`examples/agent.yaml`), with `idp_url: wss://helmsman.vasconsolutionsapp.com/agent/ws`. Find the project and service names with:
   ```bash
   docker ps --format '{{.Label "com.docker.compose.project"}} {{.Label "com.docker.compose.service"}}' | sort -u
   ```
3. Start the agent from `deploy/compose.agent.yml` (see `docs/agent-install.md`). Its image is `ghcr.io/poppyszn/helmsman-agent`, which is private by default: either run `docker login ghcr.io` on that host with a token that has `read:packages`, or make the package public under the repository's package settings.
4. Add the apps to `deploy/server/apps.yaml` in a pull request (fields in `docs/catalog.md`). After the merge and deploy, the server reloads the catalog within 30 seconds. Each app repository needs the GitHub App installed and a migrate workflow like `examples/workflows/migrate.yml`. Set required reviewers on the `production` GitHub environment of those repositories.
5. Bind people to roles on **Access**.

## Day to day

- **Deploy**: merge to `main`. **Roll back**: Actions > Deploy > Run workflow, with an older `commit_sha`.
- **Logs**: `docker compose --env-file .env logs -f server` in `/opt/helmsman/helmsman/deploy/server`.
- **Backups**: run every night. Check them and run a restore drill monthly (see the runbook). Copy the backup volume to another machine now and then: the bundled bucket is on the same disk as the database.
- **Emergency access** if GitHub sign-in is down: the break-glass admin in the runbook.
- **Production**: when you want a second environment, add a `production` GitHub Environment with the same secrets for that host, required reviewers, and deploy from the `production` branch.

## If something fails

| Symptom | Cause |
|---|---|
| Deploy fails at "missing secrets" | The environment is named `develop`, and `SSH_HOST`, `SSH_USER`, `SSH_PRIVATE_KEY`, `SSH_KNOWN_HOSTS`, `WORKING_DIR` must all be set on it. |
| `Host key verification failed` | `known_hosts.txt` does not match the server. Run `ssh-keyscan` again and reset the secret. |
| Deploy stops at preflight | The message names what is missing from `.env` on the server. |
| `.env must be listed in .gitignore` | Do not rename or move `deploy/server/.env`. |
| Server healthy but the site does not load | DNS is not pointing at the server yet, or ports 80 and 443 are closed. `docker compose logs caddy` shows the certificate attempts. |
| Sign-in returns an error | The OAuth callback URL must match `https://<your name>/auth/github/callback` exactly, and your GitHub account must be in `pops-personal-org`. |
| Pull of the image is denied | The pipeline logs in to GHCR for you. For manual commands on the host, `docker login ghcr.io` first. |
