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

// deployLogCap is how much of one job's log a viewer receives (the tail, where failures are).
const deployLogCap = 1 << 20

// deployRun checks that runID is a recent run of this environment's own deploy workflow and returns
// the target. It stops a caller from reading any other run or job in the repository.
func (s *Service) deployRun(ctx context.Context, app, env string, runID int64) (Target, error) {
	t, err := s.Resolve(ctx, app, env)
	if err != nil {
		return t, err
	}
	if t.Spec.Workflows.Deploy == "" {
		return t, &Error{Status: http.StatusNotFound, Msg: "this app has no deploy workflow in the catalog"}
	}
	if reason, bad := s.RepoProblem(t.App.Repo); bad {
		return t, &Error{Status: http.StatusConflict, Msg: reason}
	}
	runs, err := s.GH.ListRuns(ctx, t.App.Repo, t.Spec.Workflows.Deploy, 20, 30*time.Second)
	if err != nil {
		return t, &Error{Status: http.StatusBadGateway, Msg: s.describe(t.App.Repo, err)}
	}
	for _, r := range runs {
		if r.ID == runID {
			return t, nil
		}
	}
	return t, &Error{Status: http.StatusNotFound, Msg: "not found"}
}

// DeploymentJobs lists the jobs of one deploy run with their steps.
func (s *Service) DeploymentJobs(ctx context.Context, app, env string, runID int64) ([]gh.Job, error) {
	t, err := s.deployRun(ctx, app, env, runID)
	if err != nil {
		return nil, err
	}
	jobs, err := s.GH.ListJobs(ctx, t.App.Repo, runID)
	if err != nil {
		return nil, &Error{Status: http.StatusBadGateway, Msg: s.describe(t.App.Repo, err)}
	}
	return jobs, nil
}

// DeploymentJobLog returns the tail of one job's log, read live from GitHub and never stored. Opening it
// is audited (who looked at which deploy), the text is not.
func (s *Service) DeploymentJobLog(ctx context.Context, a Actor, app, env string, runID, jobID int64) (string, bool, error) {
	t, err := s.deployRun(ctx, app, env, runID)
	if err != nil {
		return "", false, err
	}
	jobs, err := s.GH.ListJobs(ctx, t.App.Repo, runID)
	if err != nil {
		return "", false, &Error{Status: http.StatusBadGateway, Msg: s.describe(t.App.Repo, err)}
	}
	found := false
	for _, j := range jobs {
		found = found || j.ID == jobID
	}
	if !found {
		return "", false, &Error{Status: http.StatusNotFound, Msg: "not found"}
	}
	if err := s.audit(ctx, a, "deploy.logs.view", t, "ok", map[string]any{"gh_run_id": runID, "job_id": jobID}); err != nil {
		return "", false, &Error{Status: http.StatusInternalServerError, Msg: "could not write the audit log"}
	}
	text, cut, err := s.GH.JobLogs(ctx, t.App.Repo, jobID, deployLogCap)
	if err != nil {
		return "", false, &Error{Status: http.StatusBadGateway, Msg: s.describe(t.App.Repo, err)}
	}
	return strings.TrimPrefix(text, "\ufeff"), cut, nil
}

var _ gh.API
