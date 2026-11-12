# Agents

One folder per app host. Each holds that host's `agent.yaml` (which compose projects and services Helmsman may see, and what it may do) and `compose.agent.yml`. Neither has a secret. The join token and host values live in a `.env` on the host.

To add a host:

1. In the portal, **Agents > Create agent** with an id (this is what `agent:` in `catalog/apps.yaml` names) and copy the join token.
2. `cp -R agents/example agents/<agent id>`, then edit `agent.yaml` (portal address, projects, services).
3. Put the folder on the host (the portal's Agents page also shows the same files ready to copy). On the host create `.env` from `env.example`, with the token.
4. `docker compose -f compose.agent.yml up -d` and check `docker compose -f compose.agent.yml logs idp-agent` for `joined` and `connected`.
5. Add the app's environments to `catalog/apps.yaml` with `agent: <agent id>`, in a pull request.

The agent image has to exist for the host's CPU (amd64 or arm64). See [docs/agent-install.md](https://github.com/poppyszn/helmsman/blob/main/docs/agent-install.md) for everything the agent does and refuses.

The portal host's deploy workflow ignores this folder: it does not put agent files on the portal host.
