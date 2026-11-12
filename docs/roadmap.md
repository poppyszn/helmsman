# Roadmap

What Helmsman does today is in the [README](../README.md). This page is for what comes next: ideas we have agreed are worth doing, ideas we are still weighing, and things we have deliberately parked. Nothing here is a promise or a date. When an item ships, move it to **Shipped** with the pull request, and keep its rationale so the next person knows why.

How to use this page:

- Add an idea as a row with a status and one line of why it matters. Longer thinking goes in a note below the table, not in the row.
- **Planned** means we intend to build it. **Considering** means it needs a decision, a design, or more evidence. **Parked** means we decided not to do it now, and the reason is written down.
- Anything that adds power to the agent or to the server's reach (new outbound calls, new agent message types) needs a note on the security rules it touches (see [security.md](security.md) and the hard rules in [AGENTS.md](../AGENTS.md)).

## Integrations

Notifications already go to email and webhooks ([notifications.md](notifications.md)). Chat and incident tools are next, built as new channel kinds on the same machinery (stored deliveries, retries, signing where it applies, per-channel event and scope filters), so each one is small.

| Item | Status | Why and notes |
|---|---|---|
| Slack | Planned | The most asked for. An incoming webhook URL is enough to start (a channel kind that formats the event as a Slack message). Later: a Slack app with a bot token so a channel can be picked from a list and approvals can be answered from Slack. |
| Telegram | Planned | A bot token and a chat id per channel. Simple HTTPS call to the Bot API; the message is the same plain text as email. |
| Microsoft Teams | Considering | Incoming webhook via Workflows. Same adapter shape as Slack. |
| Discord | Considering | Webhook URL, near identical to Slack's. |
| PagerDuty and Opsgenie | Considering | Open an incident for env.down, resolve it on env.recovered. Needs the dedup key to be the environment, which our event keys already are. |
| Generic chat adapter | Considering | One "chat webhook" kind with a template for the body, so Mattermost, Rocket.Chat, Google Chat and similar need no code. Possibly replaces the per-vendor kinds. |
| Sentry, Grafana, Uptime Kuma links | Considering | Already possible as **Tools** links and per-app quick links. Deeper integration (show open issues or firing alerts on the app page) needs a read token per tool and a decision on where those tokens live. |
| Jira or Linear | Parked | Creating tickets from failed deploys. Low demand so far; webhooks cover it for teams that want it. |
| Mail viewer (Mailpit) | Parked | A Mail page for development environments was built and removed. Not needed for now; the design (native page, sandboxed HTML, read through the agent or a direct URL) is in the pull request history. |

## Adoption and setup

| Item | Status | Why and notes |
|---|---|---|
| Bulk adopt for hosts with many apps | Planned | Group discovered containers into apps and environments from naming patterns (`name-env`), pre-fill the repository from image labels, and produce one reviewed `apps.yaml` block. Make the migrate workflow, URL and health URL optional first, so an app can be adopted for status, logs and restarts alone. |
| Per-organization repository template | In progress | [`templates/control-repo`](../templates/control-repo/README.md) is the portal's compose file, catalog, deploy workflow and a catalog check (`server catalog-check`). Still to add: each host's agent files, log history and backups in the template. |
| Workflow detection | Considering | Use the GitHub App to list an app repository's workflows and suggest the deploy and migrate ones. |
| UI-managed catalog | Considering | Let admins edit the catalog in the interface, stored in the database, as an alternative to the reviewed YAML file. Trade-off: easier for many apps, harder to review. |

## Operations

| Item | Status | Why and notes |
|---|---|---|
| Update check | Planned | Compare the running server and each agent with the newest published image and show "update available" on Agents and Setup check. Needs one outbound call to the registry, switchable off. |
| Update button | Considering | Run the app's own GitHub Actions deploy workflow for the portal (reuses the trusted pipeline, audited). |
| Agent driven updates | Parked | An agent pulling a new image and replacing containers, including the server and itself. It adds code execution power to every agent, needs a helper container for self replacement and a rollback. Decide only after the update check and button have been used. |
| Docs tab | Planned | Render the app repository's README on the app page, read through the GitHub App (Contents read). Images fetched through an authenticated proxy, links rewritten to GitHub. |
| Deploy and roll back from the portal | Considering | Start the deploy workflow for an environment, or redeploy an older commit, through `workflow_dispatch`, like migrations. |
| Resource usage | Considering | CPU and memory per container, one more read-only message type in the agent protocol. |
| Prometheus metrics endpoint | Considering | For Grafana alerts and dashboards of Helmsman itself. |

## Access and governance

| Item | Status | Why and notes |
|---|---|---|
| Notify admins of access requests | Planned | An event type "someone is waiting for access", delivered like any other notification. |
| Time boxed production access | Considering | Ask for production for an hour, an approver says yes, it expires. Builds on the approval flow. |
| Other sign-in providers | Parked | GitLab, Google or generic OIDC. GitHub organizations and teams drive access today, which is why only GitHub is supported. |

## Shipped

Newest first. See the pull request list for details.

- Access requests: people who sign in without a role are listed for an admin to grant, with a "no access yet" page and account switching.
- Apps page: tag filters, organization switch, external tools.
- Deep linkable addresses for app, environment and tab; clickable breadcrumbs.
- Notification channels limited to chosen apps, environments and events, including started and other-workflow events.
- Cleaner log styling for JSON, key=value, access and plain lines.
- Optional second-person approval for runs, and no seeding of production by developers.
- Notifications by email and webhook; per-environment timeline; setup check page; agent install files and container discovery; quick links.
- Deployment steps and logs; log history (Loki, Alloy, Faro); integrations page; production stack, backups and break-glass admin; GitHub Actions runs; agent, status, logs and restarts; GitHub sign-in, catalog and access control.
