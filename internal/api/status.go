package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/catalog"
	"github.com/poppyszn/helmsman/internal/health"
	"github.com/poppyszn/helmsman/internal/rbac"
	"github.com/poppyszn/helmsman/internal/store"
	"github.com/poppyszn/helmsman/pkg/protocol"
)

const (
	historyWindow  = 14 * 24 * time.Hour
	crashLoopDies  = 3
	crashLoopSpan  = 10 * time.Minute
	eventListLimit = 100
)

type agentView struct {
	Online          bool       `json:"online"`
	LastSeen        *time.Time `json:"last_seen"`
	Version         string     `json:"version"`
	Outdated        bool       `json:"outdated"`
	DockerOK        *bool      `json:"docker_ok"`
	ProtocolVersion int        `json:"protocol_version"`
}

type containerView struct {
	State        string     `json:"state"`
	Health       string     `json:"health"`
	Image        string     `json:"image"`
	Revision     string     `json:"revision"`
	StartedAt    *time.Time `json:"started_at"`
	RestartCount int        `json:"restart_count"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type healthView struct {
	Configured bool             `json:"configured"`
	Via        string           `json:"via"`
	Latest     *store.HealthRow `json:"latest"`
}

type blocked struct {
	Logs    string `json:"logs"`
	Restart string `json:"restart"`
}

type statusView struct {
	Env         string           `json:"env"`
	Status      string           `json:"status"`
	Reason      string           `json:"reason"` // agent_offline, docker_unavailable, container_missing or empty
	Agent       agentView        `json:"agent"`
	Container   *containerView   `json:"container"`
	LastRestart *time.Time       `json:"last_restart"`
	CrashLoop   bool             `json:"crash_loop"`
	Health      healthView       `json:"health"`
	Events      []store.EventRow `json:"events"`
	CanLogs     bool             `json:"can_logs"`
	CanRestart  bool             `json:"can_restart"`
	Blocked     blocked          `json:"blocked"`
}

func parseSpec(raw json.RawMessage) catalog.Env {
	var spec catalog.Env
	_ = json.Unmarshal(raw, &spec)
	return spec
}

// computeStatus assembles the status of one env. full adds history and capabilities.
func (s *Server) computeStatus(ctx context.Context, appID string, e store.EnvRow, p rbac.Principal, full bool) (statusView, error) {
	spec := parseSpec(e.Spec)
	v := statusView{Env: e.Env, Events: []store.EventRow{}}
	agent, err := s.Store.GetAgent(ctx, e.Agent)
	if err != nil {
		return v, err
	}
	online, _ := s.Hub.Online(e.Agent)
	v.Agent = agentView{Online: online, LastSeen: agent.LastSeenAt, DockerOK: agent.DockerOK}
	if agent.Version != nil {
		v.Agent.Version = *agent.Version
	}
	if agent.ProtocolVersion != nil {
		v.Agent.ProtocolVersion = *agent.ProtocolVersion
		v.Agent.Outdated = *agent.ProtocolVersion < protocol.Version
	}

	cs, cerr := s.Store.ContainerFor(ctx, e.Agent, spec.Container.ComposeProject, spec.Container.Service)
	hasContainer := cerr == nil
	if hasContainer {
		v.Container = &containerView{State: cs.State, Health: cs.Health, Image: cs.Image, Revision: cs.Revision,
			StartedAt: cs.StartedAt, RestartCount: cs.RestartCount, UpdatedAt: cs.UpdatedAt}
		v.LastRestart = cs.StartedAt
	}
	probes, err := s.Store.RecentHealth(ctx, e.ID, 2)
	if err != nil {
		return v, err
	}
	v.Health = healthView{Configured: spec.Health.URL != "", Via: spec.Health.Via}
	if len(probes) > 0 {
		v.Health.Latest = &probes[0]
	}
	dockerOK := agent.DockerOK != nil && *agent.DockerOK
	v.Status = health.Derive(health.Input{Now: time.Now(), AgentOnline: online, DockerOK: dockerOK, HasContainer: hasContainer,
		State: cs.State, DockerHealth: cs.Health, Probes: probes})
	switch {
	case !online:
		v.Reason = "agent_offline"
	case !dockerOK:
		v.Reason = "docker_unavailable"
	case !hasContainer:
		v.Reason = "container_missing"
	}
	if !full {
		return v, nil
	}

	since := time.Now().Add(-historyWindow)
	if v.Events, err = s.Store.RecentEvents(ctx, e.Agent, spec.Container.ComposeProject, spec.Container.Service, since, eventListLimit); err != nil {
		return v, err
	}
	// A crash loop is repeated exits in a short span. Deploy awareness arrives with the
	// deployments view; a deploy recreates the container once and never produces three exits.
	n, err := s.Store.CountEvents(ctx, e.Agent, spec.Container.ComposeProject, spec.Container.Service, protocol.EventDie, time.Now().Add(-crashLoopSpan))
	if err != nil {
		return v, err
	}
	v.CrashLoop = n >= crashLoopDies

	sc := rbac.Scope{AppID: appID, Env: e.Env, Tier: e.Tier}
	v.CanLogs = p.Allowed(rbac.LogsView, sc)
	v.CanRestart = p.Allowed(rbac.ActionRestart, sc)
	reason := ""
	switch {
	case !online:
		reason = "The agent is offline, so this is unavailable."
	case !dockerOK:
		reason = "Docker is not reachable on the app server."
	}
	if v.CanLogs && reason != "" {
		v.Blocked.Logs = reason
	}
	if v.CanRestart && reason != "" {
		v.Blocked.Restart = reason
	}
	return v, nil
}

func (s *Server) handleEnvStatus(w http.ResponseWriter, r *http.Request) {
	e, err := s.Store.EnvByName(r.Context(), chi.URLParam(r, "app"), chi.URLParam(r, "env"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	v, err := s.computeStatus(r.Context(), chi.URLParam(r, "app"), e, rc(r).Principal, true)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// statusLabel is the short status shown in the app list.
func (s *Server) statusLabel(ctx context.Context, appID string, e store.EnvRow) string {
	v, err := s.computeStatus(ctx, appID, e, rbac.Principal{}, false)
	if err != nil {
		return health.Unknown
	}
	return v.Status
}

var _ = fmt.Sprint
