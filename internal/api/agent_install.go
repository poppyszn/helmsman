package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// agentCompose is deploy/compose.agent.yml. A test keeps the two identical, so the install bundle shown
// in the portal never drifts from the file the docs and the dev stack use.
const agentCompose = `services:
  idp-agent:
    # AGENT_IMAGE lets a pilot use a locally built image (helmsman-agent:pilot) instead of GHCR.
    image: ${AGENT_IMAGE:-ghcr.io/poppyszn/helmsman-agent:${AGENT_VERSION}}
    restart: unless-stopped
    environment:
      IDP_JOIN_TOKEN: ${IDP_JOIN_TOKEN:-}
    user: "65532:65532"
    group_add: ["${DOCKER_GID}"]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./agent.yaml:/etc/idp-agent/agent.yaml:ro
      - agent-data:/data
    mem_limit: 64m
    cpus: 0.25
    logging: { driver: json-file, options: { max-size: "10m", max-file: "3" } }
volumes: { agent-data: {} }
`

// agentWSURL is the address agents dial: the server's public address with wss.
func (s *Server) agentWSURL() string {
	u := strings.TrimRight(s.Cfg.BaseURL, "/")
	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://") + "/agent/ws"
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://") + "/agent/ws"
	}
	return "wss://" + u + "/agent/ws"
}

// installBundle is everything an admin needs to start an agent on a new host. The join token appears here
// once, in the answer to creating the agent or issuing a new token, and is never stored in clear.
func (s *Server) installBundle(agentID, token string) map[string]string {
	agentYAML := "# /opt/helmsman-agent/agent.yaml, read-only inside the container.\n" +
		"idp_url: " + s.agentWSURL() + "\n" +
		"# Discovery: the agent tells the portal the names of the compose services on this host that it does not\n" +
		"# expose yet (names, image and state only, never logs), so the portal can offer to set them up.\n" +
		"# Set it to false to turn that off.\n" +
		"discovery: true\n" +
		"# Nothing is exposed until you list it here. The portal drafts these entries from what it discovers.\n" +
		"allow: []\n"
	env := "# /opt/helmsman-agent/.env (chmod 600)\n" +
		"IDP_JOIN_TOKEN=" + token + "\n" +
		"AGENT_VERSION=latest\n" +
		"# The group id of the Docker socket on this host: stat -c %g /var/run/docker.sock\n" +
		"DOCKER_GID=\n"
	commands := "mkdir -p /opt/helmsman-agent && cd /opt/helmsman-agent\n" +
		"# 1. create agent.yaml, compose.agent.yml and .env from the three files above\n" +
		"# 2. set DOCKER_GID in .env:\n" +
		"sed -i \"s/^DOCKER_GID=.*/DOCKER_GID=$(stat -c %g /var/run/docker.sock)/\" .env\n" +
		"# 3. the agent image is private: log in, pull, log out, start\n" +
		"docker login ghcr.io -u <your github login>\n" +
		"docker compose -f compose.agent.yml --env-file .env pull\n" +
		"docker logout ghcr.io\n" +
		"docker compose -f compose.agent.yml --env-file .env up -d\n" +
		"docker compose -f compose.agent.yml --env-file .env logs --tail 20   # joined, then connected\n"
	return map[string]string{"agent_yaml": agentYAML, "compose": agentCompose, "env": env, "commands": commands}
}

// handleAgentContainers lists what an agent exposes and what it discovered on its host.
func (s *Server) handleAgentContainers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cs, err := s.Store.AgentContainers(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": cs})
}
