package actions

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/poppyszn/helmsman/internal/gh"
)

// Deployment is one run of the app's deploy workflow.
type Deployment struct {
	RunID   int64     `json:"run_id"`
	Title   string    `json:"title"`
	Commit  string    `json:"commit"`
	Actor   string    `json:"actor"`
	At      time.Time `json:"at"`
	Status  string    `json:"status"` // succeeded | failed | in_progress | queued | cancelled
	URL     string    `json:"url"`
	Matches *bool     `json:"matches_running,omitempty"`
}

// DeploymentView is the Deployments tab of an environment.
type DeploymentView struct {
	Configured    bool         `json:"configured"`
	Deployments   []Deployment `json:"deployments"`
	RunningRev    string       `json:"running_revision"`
	LatestSuccess string       `json:"latest_success_commit"`
	Mismatch      bool         `json:"mismatch"`
	Error         string       `json:"error,omitempty"`
}

// Deployments lists recent runs of the catalog deploy workflow (cached 30 s, conditional
// requests) and flags when the latest successful deploy is not what is running.
func (s *Service) Deployments(ctx context.Context, app, env string) (DeploymentView, error) {
	t, err := s.Resolve(ctx, app, env)
	if err != nil {
		return DeploymentView{}, err
	}
	v := DeploymentView{Deployments: []Deployment{}}
	if c, cerr := s.Store.ContainerFor(ctx, t.Env.Agent, t.Spec.Container.ComposeProject, t.Spec.Container.Service); cerr == nil {
		v.RunningRev = c.Revision
	}
	if t.Spec.Workflows.Deploy == "" {
		return v, nil
	}
	v.Configured = true
	if reason, bad := s.RepoProblem(t.App.Repo); bad {
		v.Error = reason
		return v, nil
	}
	runs, err := s.GH.ListRuns(ctx, t.App.Repo, t.Spec.Workflows.Deploy, 20, 30*time.Second)
	if err != nil {
		v.Error = s.describe(t.App.Repo, err)
		return v, nil
	}
	for _, r := range runs {
		d := Deployment{RunID: r.ID, Title: r.Title, Commit: r.HeadSHA, Actor: r.Actor.Login, At: r.CreatedAt, URL: r.HTMLURL, Status: mapStatus(r)}
		if d.Status == "succeeded" && v.LatestSuccess == "" {
			v.LatestSuccess = r.HeadSHA
		}
		v.Deployments = append(v.Deployments, d)
	}
	if v.RunningRev != "" && v.LatestSuccess != "" {
		v.Mismatch = !sameCommit(v.RunningRev, v.LatestSuccess)
		for i := range v.Deployments {
			m := sameCommit(v.RunningRev, v.Deployments[i].Commit)
			v.Deployments[i].Matches = &m
		}
	}
	return v, nil
}

func sameCommit(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a != "" && b != "" && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a))
}

var _ = http.StatusOK
var _ gh.API
