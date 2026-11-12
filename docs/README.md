# Documentation

An index of every page, by what you are trying to do. If you are new, read the [README](../README.md) and then [architecture.md](architecture.md).

## Run Helmsman

| Page | What it covers |
|---|---|
| [server-setup.md](server-setup.md) | Install on a server: DNS, GitHub registrations, `.env`, HTTPS with Caddy or nginx, first sign-in, connecting an app |
| [../templates/control-repo](../templates/control-repo/README.md) | A folder to copy into a private repository in any org, so GitHub manages the portal: catalog and version in pull requests, deploy on merge |
| [agent-install.md](agent-install.md) | Add a host: the agent, its allowlist, discovery, troubleshooting |
| [runbook.md](runbook.md) | Day to day: sign-in and access, agents and status, runs, deploys, backups and restores, retention, break-glass, load test, tests |
| [security.md](security.md) | The security checklist: each control, where it lives, how it is tested |

## Configure Helmsman

| Page | What it covers |
|---|---|
| [catalog.md](catalog.md) | Every field of `apps.yaml`: apps, environments, tags, links, approval, health, workflows |
| [notifications.md](notifications.md) | Channels, events, scopes, SMTP, webhook signing |
| [logs.md](logs.md) | Log history with Loki and Alloy, nginx and browser (Faro) logs, filters, sample configs in `examples/logs` |

## Build Helmsman

| Page | What it covers |
|---|---|
| [architecture.md](architecture.md) | The parts, packages, protocol, data, background work, where each rule lives |
| [design-system.md](design-system.md) | UI rules: surfaces, type, icons, status indicators, links |
| [../CONTRIBUTING.md](../CONTRIBUTING.md) | Workflow, definition of done, recipes, testing, docs style |
| [../AGENTS.md](../AGENTS.md) | The hard rules and the project map, written for AI coding agents |
| [roadmap.md](roadmap.md) | Planned, considered and parked work, and what has shipped |

## Examples

`examples/` holds files to copy: an `agent.yaml`, a migrate workflow and the `helmsman-run.yml` adapter in `examples/workflows`, and the log history samples (Alloy, Loki, Faro, Grafana alerts) in `examples/logs`.

## Keeping docs right

A change that alters behaviour updates its doc in the same pull request. Style rules and where things belong are in [CONTRIBUTING.md](../CONTRIBUTING.md#docs). `make docs-check` checks titles, relative links and the no em dash rule.
