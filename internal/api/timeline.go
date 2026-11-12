package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/health"
	"github.com/poppyszn/helmsman/internal/rbac"
	"github.com/poppyszn/helmsman/pkg/protocol"
)

// timelineWindow is how far back the timeline reaches.
const timelineWindow = 14 * 24 * time.Hour

const timelineLimit = 200

// timelineEntry is one thing that happened to an environment.
type timelineEntry struct {
	At    time.Time `json:"at"`
	Kind  string    `json:"kind"`  // deploy | run | restart | container | health
	Level string    `json:"level"` // ok | info | warn | bad
	Title string    `json:"title"`
	// Detail is a short line under the title: a commit, an exit code, an error. Never log text.
	Detail string `json:"detail,omitempty"`
	Actor  string `json:"actor,omitempty"`
	URL    string `json:"url,omitempty"`
}

// handleTimeline merges what happened to one environment into a single newest-first list, so "what
// changed before it broke" is answered in one place: deployments, migrations and seeds, restarts from
// Helmsman, container starts, exits and health changes, and health checks starting or stopping to fail.
// Container and health history need app.view; deployments, runs and restarts name people and need
// deploy.view, like the tabs they come from.
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	app, envName := chi.URLParam(r, "app"), chi.URLParam(r, "env")
	e, err := s.Store.EnvByName(r.Context(), app, envName)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	spec := parseSpec(e.Spec)
	since := time.Now().Add(-timelineWindow)
	p := rc(r).Principal
	sc := rbac.Scope{AppID: app, Env: envName, Tier: e.Tier}
	var out []timelineEntry
	var notes []string

	// Container events the agent reported.
	if evs, err := s.Store.RecentEvents(r.Context(), e.Agent, spec.Container.ComposeProject, spec.Container.Service, since, 300); err == nil {
		for _, ev := range evs {
			out = append(out, containerEntry(ev.At, ev.Event, ev.Detail))
		}
	}
	// Health checks starting and stopping to fail.
	if rows, err := s.Store.HealthSince(r.Context(), e.ID, since, 5000); err == nil {
		fails := 0
		down := false
		for _, h := range rows {
			if h.Status == health.ProbeFail {
				fails++
				if fails == 2 && !down {
					down = true
					d := "no answer"
					if h.HTTPCode > 0 {
						d = "HTTP " + strconv.Itoa(h.HTTPCode)
					}
					out = append(out, timelineEntry{At: h.CheckedAt, Kind: "health", Level: "bad", Title: "Health checks started failing", Detail: d})
				}
				continue
			}
			fails = 0
			if down {
				down = false
				out = append(out, timelineEntry{At: h.CheckedAt, Kind: "health", Level: "ok", Title: "Health checks recovered"})
			}
		}
	}

	if p.Allowed(rbac.DeployView, sc) {
		out = append(out, s.timelineRuns(r.Context(), e.ID, since)...)
		out = append(out, s.timelineRestarts(r.Context(), app, envName, since)...)
		d, notes2 := s.timelineDeploys(r.Context(), app, envName, since)
		out = append(out, d...)
		notes = append(notes, notes2...)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > timelineLimit {
		out = out[:timelineLimit]
	}
	if out == nil {
		out = []timelineEntry{}
	}
	if notes == nil {
		notes = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "notes": notes, "since": since})
}

func containerEntry(at time.Time, event string, detail *string) timelineEntry {
	t := timelineEntry{At: at, Kind: "container"}
	if detail != nil {
		t.Detail = *detail
	}
	switch {
	case event == protocol.EventDie:
		t.Level, t.Title = "bad", "Container exited"
	case event == protocol.EventStart:
		t.Level, t.Title = "ok", "Container started"
	case event == protocol.EventRestart:
		t.Level, t.Title = "info", "Container restarted"
	case strings.HasPrefix(event, "health:"):
		st := strings.TrimPrefix(event, "health:")
		switch st {
		case "unhealthy":
			t.Level, t.Title = "warn", "Container became unhealthy"
		case "healthy":
			t.Level, t.Title = "ok", "Container became healthy"
		default:
			t.Level, t.Title = "info", "Container health: "+st
		}
	default:
		t.Level, t.Title = "info", "Container "+event
	}
	return t
}

func (s *Server) timelineRuns(ctx context.Context, envID int64, since time.Time) []timelineEntry {
	runs, err := s.Store.ListRuns(ctx, envID, 60)
	if err != nil {
		return nil
	}
	var out []timelineEntry
	for _, r := range runs {
		what := map[string]string{"migrate": "Migration", "seed": "Seed"}[r.Action]
		if what == "" {
			what = r.Action
		}
		url := ""
		if r.GHRunURL != nil {
			url = *r.GHRunURL
		}
		if r.CreatedAt.After(since) {
			out = append(out, timelineEntry{At: r.CreatedAt, Kind: "run", Level: "info", Title: what + " requested", Actor: r.RequestedByLogin})
		}
		if r.DecidedAt != nil && r.DecidedAt.After(since) {
			switch {
			case r.ApprovedByLogin != nil:
				out = append(out, timelineEntry{At: *r.DecidedAt, Kind: "run", Level: "ok", Title: what + " approved", Actor: *r.ApprovedByLogin})
			case r.RejectedByLogin != nil:
				out = append(out, timelineEntry{At: *r.DecidedAt, Kind: "run", Level: "warn", Title: what + " rejected", Actor: *r.RejectedByLogin})
			}
		}
		if r.FinishedAt != nil && r.FinishedAt.After(since) {
			t := timelineEntry{At: *r.FinishedAt, Kind: "run", URL: url}
			switch r.Status {
			case "succeeded":
				t.Level, t.Title = "ok", what+" succeeded"
			case "cancelled":
				t.Level, t.Title = "warn", what+" cancelled"
			case "rejected":
				continue
			default:
				t.Level, t.Title = "bad", what+" failed"
				if r.Error != nil {
					t.Detail = *r.Error
				}
			}
			out = append(out, t)
		}
	}
	return out
}

func (s *Server) timelineRestarts(ctx context.Context, app, env string, since time.Time) []timelineEntry {
	rows, err := s.Store.TargetAudit(ctx, app+"/"+env, []string{"action.restart"}, since, 100)
	if err != nil {
		return nil
	}
	var out []timelineEntry
	for _, a := range rows {
		t := timelineEntry{At: a.At, Kind: "restart", Level: "ok", Title: "Restarted from Helmsman", Actor: a.ActorID}
		if a.Result != "ok" {
			t.Level, t.Title = "bad", "Restart from Helmsman failed ("+a.Result+")"
		}
		out = append(out, t)
	}
	return out
}

func (s *Server) timelineDeploys(ctx context.Context, app, env string, since time.Time) ([]timelineEntry, []string) {
	v, err := s.Actions.Deployments(ctx, app, env)
	if err != nil || !v.Configured {
		return nil, nil
	}
	if v.Error != "" {
		return nil, []string{"Deployments are not shown: " + v.Error}
	}
	var out []timelineEntry
	for _, d := range v.Deployments {
		if d.At.Before(since) {
			continue
		}
		t := timelineEntry{At: d.At, Kind: "deploy", Actor: d.Actor, URL: d.URL, Detail: d.Title}
		c := d.Commit
		if len(c) > 7 {
			c = c[:7]
		}
		switch d.Status {
		case "succeeded":
			t.Level, t.Title = "ok", "Deployed "+c
		case "failed":
			t.Level, t.Title = "bad", "Deployment of "+c+" failed"
		case "cancelled":
			t.Level, t.Title = "warn", "Deployment of "+c+" cancelled"
		default:
			t.Level, t.Title = "info", "Deployment of "+c+" "+strings.ReplaceAll(d.Status, "_", " ")
		}
		out = append(out, t)
	}
	return out, nil
}
