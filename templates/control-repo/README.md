# Helmsman control repository

This repository runs one Helmsman portal from GitHub. It holds the server stack, the catalog (which apps and environments Helmsman shows) and the workflow that puts both on the server. Nothing in it is specific to one organization or secret, so the same files work in any org: copy this folder into a new private repository and fill in the secrets below.

```
catalog/apps.yaml     which apps, environments and agents Helmsman shows (a pull request changes it)
helmsman.version      the Helmsman server image and tag this repository runs (a pull request upgrades it)
compose.yml           Helmsman, its Postgres and optionally Caddy for TLS
up.sh                 docker compose for this stack with that image, on the host
preflight.sh          stops a deploy before anything changes if the host's .env is incomplete
env.example           the host's .env, filled in once on the server, never committed
caddy/ db/            files compose.yml mounts
agents/               one folder per app host: agent.yaml and compose.agent.yml (see agents/README.md)
.github/workflows/    check.yml (pull requests) and deploy.yml (merge to main)
```

## One-time setup

The full walkthrough, with every command, is [docs/server-setup.md](https://github.com/poppyszn/helmsman/blob/main/docs/server-setup.md) in the Helmsman repository. The steps differ only where this repository replaces the Helmsman checkout:

1. **Server.** Docker with Compose v2, a `deploy` user in the `docker` group, `/opt/helmsman` owned by `deploy` and `/opt/helmsman-secrets` (mode 700, also owned by `deploy`, so the preflight can see the key file inside it). Open 22, 80 and 443. Point a DNS name at it. (Server setup, step 1.)
2. **GitHub registrations.** An OAuth App for sign-in and a GitHub App for runs, and the App's private key at `/opt/helmsman-secrets/github-app.pem` owned by 65532. (Step 2.) Create the GitHub App in the organization that owns the app repositories.
3. **The image must be pullable.** `helmsman.version` names a GitHub package. Make that package public (package settings), or log the server in to `ghcr.io` once with a `read:packages` token.
4. **Secrets for the deploy.** Create a deploy key and pin the host key (server setup, step 3, option B), then store them in a GitHub Environment named `production` in this repository:

   ```bash
   R=<org>/<this-repo>
   gh api -X PUT repos/$R/environments/production >/dev/null
   gh secret set SSH_HOST        --env production -R $R --body "portal.example.com"
   gh secret set SSH_USER        --env production -R $R --body "deploy"
   gh secret set WORKING_DIR     --env production -R $R --body "/opt/helmsman"
   gh secret set SSH_PRIVATE_KEY --env production -R $R < helmsman-deploy
   gh secret set SSH_KNOWN_HOSTS --env production -R $R < known_hosts.txt
   gh variable set HEALTH_URL    --env production -R $R --body "https://portal.example.com/readyz"
   ```

   Add required reviewers to the `production` environment if you want a person to approve each deploy.
5. **First deploy.** Turn the workflow on and run it. It copies the files and stops at the preflight because the host has no `.env` yet. That is expected.

   ```bash
   gh variable set DEPLOY_ENABLED --body true -R $R
   gh workflow run deploy.yml -R $R
   ```
6. **The host's `.env`.** On the server, in `/opt/helmsman`: `cp env.example .env && chmod 600 .env`, fill it in (server setup, step 4 lists every value), then `bash preflight.sh` until it prints `preflight: ok`. The `.env` is never in git and a deploy never touches it.
7. **Start it.** Run the deploy again from the Actions tab. It pulls the image, starts Postgres and Helmsman, waits for them to be healthy and checks `/readyz`.

Make the repository private, and protect `main` (require a pull request and the `CI: Checks` check). A merge to `main` is a production deploy.

## Day to day

- **Add or change an app or environment.** Edit `catalog/apps.yaml` in a pull request ([the fields](https://github.com/poppyszn/helmsman/blob/main/docs/catalog.md)). The check runs the server's own validation. After the merge the deploy copies it and Helmsman reloads it within 30 seconds, with no restart.
- **Upgrade or roll back Helmsman.** Change the tag in `helmsman.version` in a pull request. Use a version tag or a commit tag, not `latest`, so a deploy is repeatable. A rollback is the same change to the older tag.
- **Change the host's secrets.** Edit `/opt/helmsman/.env`, then recreate the server: `./up.sh up -d --no-deps server`. A plain `docker restart` does not reload it.
- **Look at it.** `./up.sh ps` and `./up.sh logs -f server` on the host.

## Using this in another organization

Create a private repository there, copy this folder's contents into it, and repeat the one-time setup with that organization's server and secrets. Apps and agents are separate per portal: each portal has its own catalog and its own agents.

## Not included yet

- Putting agent files on app hosts from the workflow (copy them by hand for now, see `agents/README.md`).
- Log history (Loki and Alloy) and the nightly Postgres backup that the Helmsman repository's own server stack has. Back up the `pgdata` volume some other way for now. They are on the [roadmap](https://github.com/poppyszn/helmsman/blob/main/docs/roadmap.md).
- Removing a file from the repository does not remove it from the server; delete it on the host.
