# Catalog reference

`catalog/apps.yaml` lives in git and is reviewed in pull requests. The server loads it at startup, every 30 seconds when the file changes, and on **Admin > Catalog > Reload now**.

## Invalid catalogs

A reload is all or nothing. On bad YAML, unknown fields, duplicate ids, or an agent that is not registered, the reload is rejected, the **last good catalog stays active**, and the error is shown on the admin Catalog page and written to the audit log. The server never refuses to start because of the catalog.

Apps or environments removed from the file are soft-deleted: hidden from everyone, history kept, actions refused. Adding them back restores them.

## Fields

```yaml
version: 1                      # must be 1
apps:
  - id: billing-api             # lowercase slug, unique, used in RBAC bindings
    name: Billing API
    repo: acme-org/billing-api  # owner/repo; the owner selects the GitHub App installation (M3)
    environments:
      - env: staging            # lowercase slug, unique per app. "*" is reserved for bindings
        tier: nonprod           # nonprod | prod
        agent: staging-app-01   # must already exist in the IDP (created by an admin in M2)
        container:
          compose_project: billing   # matched by compose labels, never by container id
          service: api
        url: https://staging-billing.example.com
        health:
          url: https://staging-billing.example.com/health
          via: server           # server | agent (use agent for private URLs)
          tls_skip_verify: false
        workflows:
          deploy: deploy.yml    # optional
          migrate: migrate.yml  # handles both migrate and seed
        github_environment: staging
        allow_seed: true        # default: true for nonprod, false for prod
        require_approval: true  # default: true for prod, false for nonprod
```

## Quick links

`links` adds shortcuts to the app page, next to an automatic link to the repository. Put them on the app to show them on every environment, and on an environment for that one only (the app's come first). At most 10 each, a name of 1 to 40 characters and an http(s) address.

```yaml
apps:
  - id: billing-api
    links:
      - { name: Runbook, url: https://wiki.example.com/billing }
    environments:
      - env: staging
        links:
          - { name: Grafana, url: https://grafana.example.com/d/billing-staging }
```

## Approval

By default a migration or seed on a `tier: prod` environment waits for a second person with `action.approve`, and runs on other environments start at once. Set `require_approval` on an environment to change that for it:

```yaml
      - env: prod
        tier: prod
        require_approval: false   # start production runs at once
```

With `false`, a run still needs the permission on that exact environment (a wildcard `env: *` binding never reaches production), seeds stay refused when `allow_seed` is false, and every run is audited, including that no approval was required. The real gate against someone running a workflow outside Helmsman is the GitHub environment's protection rules (required reviewers on the environment the workflow deploys to). Helmsman shows such a run as waiting for GitHub approval.

## Tiers and access

`tier: prod` environments are only reachable through bindings that name the env explicitly (for example `env: prod`). A binding with `env: *` covers non-production environments only. Site admins (the `is_admin` flag) can see everything.

`allow_seed: false` refuses seeds for that environment even for admins.

## Agents must exist first

Catalog validation rejects an environment whose `agent` is not registered. Create the agent first under **Agents** in the UI (see [agent-install.md](agent-install.md)); `make seed-dev` registers `dev-agent` for the local stack. Revoking an agent does not change environments that already use it, but a catalog reload that still references it is rejected.

## Health checks

`health.via: server` probes from the IDP host. `via: agent` probes from the app server through its agent, for URLs only that host can reach. The agent only probes URLs listed under `health_urls` in its own `agent.yaml`, and the URL must match exactly. A response below 400 within 2 s is healthy, slower is degraded, and two failures in a row mean down.

## Workflows

`workflows.migrate` is required. Migrations and seeds both run it, with the input `command` set to `migrate` or `seed`. `workflows.deploy` is optional. When it is set, the Deployments tab lists its recent runs and shows a badge if the latest successful deploy commit differs from the running image revision (the `org.opencontainers.image.revision` label). `github_environment` is passed as the workflow's `environment` input. Both workflows must exist in `.github/workflows` and the migrate workflow must have a `workflow_dispatch` trigger, or actions for that repository are disabled. See `examples/workflows/migrate.yml` for the inputs and the `run-name` that Helmsman uses to find its run.
