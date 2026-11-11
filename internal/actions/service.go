// Package actions runs migrations and seeds through GitHub Actions workflow_dispatch:
// request, approval, dispatch, correlation, status tracking and log capture. The only
// thing the portal can start is a catalog workflow with fixed inputs, never a free-form command.
package actions

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/catalog"
	"github.com/poppyszn/helmsman/internal/gh"
	"github.com/poppyszn/helmsman/internal/rbac"
	"github.com/poppyszn/helmsman/internal/store"
)

// Commands the portal can start. Both run the app's migrate workflow with a different input.
const (
	Migrate = "migrate"
	Seed    = "seed"
)

// Error carries an HTTP status for the API layer.
type Error struct {
	Status int
	Msg    string
	Run    *store.ActionRun
}

func (e *Error) Error() string { return e.Msg }

func fail(status int, format string, a ...any) *Error {
	return &Error{Status: status, Msg: fmt.Sprintf(format, a...)}
}

// Actor is who is acting.
type Actor struct {
	User      store.User
	Principal rbac.Principal
	IP        string
}

// Service owns the run lifecycle.
type Service struct {
	Store *store.Store
	GH    gh.API
	Audit *audit.Writer
	Log   *slog.Logger
	Now   func() time.Time

	Dedupe         time.Duration // same user, same env, same command within this window returns the first run
	CorrelateEvery time.Duration
	CorrelateFor   time.Duration
	PollEvery      time.Duration
	Tick           time.Duration
	LogCap         int // bytes of job logs kept per run

	mu           sync.Mutex
	blockedUntil time.Time
	checks       map[string]RepoCheck
}

// New builds a service with the production timings.
func New(st *store.Store, api gh.API, aw *audit.Writer, log *slog.Logger) *Service {
	return &Service{Store: st, GH: api, Audit: aw, Log: log, Dedupe: 10 * time.Second, CorrelateEvery: 3 * time.Second,
		CorrelateFor: 60 * time.Second, PollEvery: 5 * time.Second, Tick: time.Second, LogCap: 5 << 20, checks: map[string]RepoCheck{}}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Target is an env resolved with its app.
type Target struct {
	App  store.AppRow
	Env  store.EnvRow
	Spec catalog.Env
}

// Resolve loads an env and its catalog spec.
func (s *Service) Resolve(ctx context.Context, app, env string) (Target, error) {
	a, err := s.Store.GetApp(ctx, app)
	if err != nil {
		return Target{}, fail(http.StatusNotFound, "not found")
	}
	for _, e := range a.Envs {
		if e.Env == env {
			var spec catalog.Env
			_ = json.Unmarshal(e.Spec, &spec)
			return Target{App: a, Env: e, Spec: spec}, nil
		}
	}
	return Target{}, fail(http.StatusNotFound, "not found")
}

// PermissionFor is the permission a command needs.
func PermissionFor(command string) string {
	if command == Seed {
		return rbac.ActionSeed
	}
	return rbac.ActionMigrate
}

func scopeOf(t Target) rbac.Scope {
	return rbac.Scope{AppID: t.App.ID, Env: t.Env.Env, Tier: t.Env.Tier}
}

func (s *Service) audit(ctx context.Context, a Actor, action string, t Target, result string, d map[string]any) error {
	return s.Audit.Write(ctx, audit.Event{ActorType: audit.ActorUser, ActorID: a.User.Login, Action: action,
		Target: t.App.ID + "/" + t.Env.Env, Result: result, IP: a.IP, Details: d})
}

func (s *Service) auditSystem(ctx context.Context, action string, run store.ActionRun, result string, d map[string]any) {
	if d == nil {
		d = map[string]any{}
	}
	d["run_id"] = run.ID
	if err := s.Audit.Write(context.WithoutCancel(ctx), audit.Event{ActorType: audit.ActorSystem, ActorID: "helmsman", Action: action,
		Target: run.App + "/" + run.Env, Result: result, Details: d}); err != nil {
		s.Log.Error("audit write failed", "action", action, "err", err)
	}
}

// Result of Request.
type Result struct {
	Run     store.ActionRun
	Deduped bool
}

// Request creates a run for a migration or seed. Production runs wait for a second
// person with action.approve; everything else is dispatched at once.
func (s *Service) Request(ctx context.Context, a Actor, app, env, command, rerunOf string) (Result, error) {
	if command != Migrate && command != Seed {
		return Result{}, fail(http.StatusBadRequest, "unknown command")
	}
	t, err := s.Resolve(ctx, app, env)
	if err != nil {
		return Result{}, err
	}
	if t.Spec.Workflows.Migrate == "" {
		return Result{}, fail(http.StatusConflict, "this environment has no migrate workflow in the catalog")
	}
	if command == Seed && !rbac.SeedAllowed(t.Spec.SeedAllowed()) {
		_ = s.audit(ctx, a, "action.run.requested", t, "denied", map[string]any{"command": command, "reason": "allow_seed is false"})
		return Result{}, fail(http.StatusForbidden, "seeding is not allowed in %s", env)
	}
	if reason, bad := s.RepoProblem(t.App.Repo); bad {
		return Result{}, fail(http.StatusConflict, "actions are disabled for %s: %s", t.App.Repo, reason)
	}
	if rerunOf == "" {
		if prev, err := s.Store.RecentRunBy(ctx, t.Env.ID, a.User.ID, command, s.now().Add(-s.Dedupe)); err == nil {
			return Result{Run: prev, Deduped: true}, nil
		}
	}
	status := "dispatching"
	if t.Env.Tier == rbac.TierProd {
		status = "pending_approval"
	}
	id := newUUID()
	inputs := map[string]string{"command": command, "environment": envInput(t), "requested_by": a.User.Login}
	nr := store.NewRun{ID: id, CorrelationID: newUUID(), AppEnvID: t.Env.ID, Action: command, Inputs: inputs, Status: status,
		RequestedBy: a.User.ID, Repo: t.App.Repo, Workflow: t.Spec.Workflows.Migrate, RerunOf: rerunOf}
	if err := s.Store.CreateRun(ctx, nr); err != nil {
		if errors.Is(err, store.ErrActiveRun) {
			e := fail(http.StatusConflict, "another run is already active in this environment; wait for it to finish")
			if r, gerr := s.Store.ActiveRunForEnv(ctx, t.Env.ID); gerr == nil {
				e.Run = &r
			}
			return Result{}, e
		}
		return Result{}, err
	}
	det := map[string]any{"run_id": id, "command": command, "tier": t.Env.Tier, "status": status, "rerun_of": rerunOf}
	if err := s.audit(ctx, a, "action.run.requested", t, "ok", det); err != nil {
		_ = s.Store.Fail(ctx, id, "audit log unavailable; nothing was started")
		return Result{}, fail(http.StatusInternalServerError, "could not write the audit log; nothing was started")
	}
	if status == "dispatching" {
		s.dispatch(ctx, id)
	}
	run, err := s.Store.GetRun(ctx, id)
	return Result{Run: run}, err
}

func envInput(t Target) string {
	if t.Spec.GitHubEnvironment != "" {
		return t.Spec.GitHubEnvironment
	}
	return t.Env.Env
}

// dispatch performs the GitHub call for a run in state dispatching. Whoever claims the run
// does the work, so a click, an approval and the poller can never dispatch twice.
func (s *Service) dispatch(ctx context.Context, id string) {
	ok, err := s.Store.ClaimDispatch(ctx, id)
	if err != nil || !ok {
		return
	}
	run, err := s.Store.GetRun(ctx, id)
	if err != nil {
		return
	}
	t, err := s.Resolve(ctx, run.App, run.Env)
	if err != nil {
		_ = s.Store.Fail(ctx, id, "the environment no longer exists in the catalog")
		return
	}
	ref, warning, err := s.resolveRef(ctx, t, run.Repo)
	if err == nil {
		err = s.GH.Dispatch(ctx, run.Repo, run.Workflow, ref, map[string]string{
			"environment": envInput(t), "command": run.Action, "correlation_id": run.CorrelationID, "requested_by": run.RequestedByLogin})
	}
	if err != nil {
		msg := s.describe(run.Repo, err)
		_ = s.Store.Fail(ctx, id, msg)
		s.auditSystem(ctx, "action.run.dispatched", run, "error", map[string]any{"error": msg, "ref": ref})
		return
	}
	_ = s.Store.MarkDispatched(ctx, id, ref, warning, s.now(), s.now().Add(s.CorrelateEvery))
	s.auditSystem(ctx, "action.run.dispatched", run, "ok", map[string]any{"ref": ref, "warning": warning, "command": run.Action})
}

// describe turns a GitHub error into something an engineer can act on, and flags the repo
// when the credentials are the problem.
func (s *Service) describe(repo string, err error) string {
	var rl *gh.RateLimitError
	var api *gh.APIError
	switch {
	case errors.As(err, &rl):
		s.block(rl.RetryAfter)
		return fmt.Sprintf("GitHub rate limit reached. Try again in %s.", rl.RetryAfter.Round(time.Second))
	case errors.Is(err, gh.ErrNoInstallation):
		s.setProblem(repo, "the GitHub App is not installed on this repository")
		return "The GitHub App is not installed on " + repo + ". Ask an org owner to install it."
	case gh.IsUnprocessable(err):
		return "GitHub rejected the dispatch: " + err.Error() + ". Check that the workflow accepts the inputs environment, command, correlation_id and requested_by, and that it has a workflow_dispatch trigger."
	case gh.IsNotFound(err):
		return "GitHub could not find the repository or workflow, or the App lacks access to it."
	case errors.As(err, &api) && api.Status == http.StatusUnauthorized:
		s.setProblem(repo, "GitHub rejected the App's credentials")
		return "GitHub rejected the App's credentials for " + repo + "."
	}
	return "Could not reach GitHub: " + err.Error()
}

// resolveRef picks the ref to run: the tag of the version that is deployed, otherwise the
// default branch with a warning, because the scripts of an old tag are what produced the deploy.
func (s *Service) resolveRef(ctx context.Context, t Target, repo string) (ref, warning string, err error) {
	rev := ""
	if c, cerr := s.Store.ContainerFor(ctx, t.Env.Agent, t.Spec.Container.ComposeProject, t.Spec.Container.Service); cerr == nil {
		rev = c.Revision
	}
	if rev != "" {
		tag, ok, terr := s.GH.TagForCommit(ctx, repo, rev)
		var rl *gh.RateLimitError
		if errors.As(terr, &rl) {
			return "", "", terr
		}
		if terr == nil && ok {
			return tag, "", nil
		}
		warning = "No tag matches the deployed revision, so the default branch was used."
	} else {
		warning = "The deployed version is unknown, so the default branch was used."
	}
	def, err := s.GH.DefaultBranch(ctx, repo)
	if err != nil {
		return "", "", err
	}
	return def, warning, nil
}

// Approve lets a second person release a production run.
func (s *Service) Approve(ctx context.Context, a Actor, runID string) (store.ActionRun, error) {
	run, t, err := s.load(ctx, runID)
	if err != nil {
		return run, err
	}
	if run.Status != "pending_approval" {
		return run, fail(http.StatusConflict, "this request is no longer waiting for approval")
	}
	if a.User.ID == run.RequestedBy {
		_ = s.audit(ctx, a, "action.run.approved", t, "denied", map[string]any{"run_id": run.ID, "reason": "self approval"})
		return run, fail(http.StatusForbidden, "you cannot approve your own request")
	}
	if !a.Principal.CanApprove(scopeOf(t), run.RequestedBy, a.User.ID) {
		return run, fail(http.StatusForbidden, "forbidden")
	}
	if reason, bad := s.RepoProblem(run.Repo); bad {
		return run, fail(http.StatusConflict, "actions are disabled for %s: %s", run.Repo, reason)
	}
	won, err := s.Store.Approve(ctx, runID, a.User.ID)
	if err != nil {
		return run, err
	}
	if !won {
		return run, fail(http.StatusConflict, "someone else already decided this request")
	}
	if err := s.audit(ctx, a, "action.run.approved", t, "ok", map[string]any{"run_id": run.ID, "command": run.Action, "requested_by": run.RequestedByLogin}); err != nil {
		s.Log.Error("audit write failed after approval", "err", err)
	}
	s.dispatch(ctx, runID)
	return s.Store.GetRun(ctx, runID)
}

// Reject declines a production run. The requester can also withdraw their own request.
func (s *Service) Reject(ctx context.Context, a Actor, runID, reason string) (store.ActionRun, error) {
	run, t, err := s.load(ctx, runID)
	if err != nil {
		return run, err
	}
	if run.Status != "pending_approval" {
		return run, fail(http.StatusConflict, "this request is no longer waiting for approval")
	}
	withdraw := a.User.ID == run.RequestedBy
	if !withdraw && !a.Principal.CanApprove(scopeOf(t), run.RequestedBy, a.User.ID) {
		return run, fail(http.StatusForbidden, "forbidden")
	}
	if withdraw {
		reason = "withdrawn by the requester"
	}
	won, err := s.Store.Reject(ctx, runID, a.User.ID, reason)
	if err != nil {
		return run, err
	}
	if !won {
		return run, fail(http.StatusConflict, "someone else already decided this request")
	}
	_ = s.audit(ctx, a, "action.run.rejected", t, "ok", map[string]any{"run_id": run.ID, "reason": reason, "withdrawn": withdraw})
	return s.Store.GetRun(ctx, runID)
}

// Cancel asks GitHub to cancel a running workflow.
func (s *Service) Cancel(ctx context.Context, a Actor, runID string) (store.ActionRun, error) {
	run, t, err := s.load(ctx, runID)
	if err != nil {
		return run, err
	}
	if !a.Principal.Allowed(PermissionFor(run.Action), scopeOf(t)) {
		return run, fail(http.StatusForbidden, "forbidden")
	}
	switch run.Status {
	case "queued", "in_progress", "waiting":
	default:
		return run, fail(http.StatusConflict, "this run cannot be cancelled in state %s", run.Status)
	}
	if run.GHRunID == nil {
		return run, fail(http.StatusConflict, "the run is not visible on GitHub yet; try again in a few seconds")
	}
	if err := s.GH.Cancel(ctx, run.Repo, *run.GHRunID); err != nil {
		_ = s.audit(ctx, a, "action.run.cancelled", t, "error", map[string]any{"run_id": run.ID, "error": err.Error()})
		return run, fail(http.StatusBadGateway, "%s", s.describe(run.Repo, err))
	}
	_ = s.audit(ctx, a, "action.run.cancelled", t, "ok", map[string]any{"run_id": run.ID})
	_ = s.Store.SetNextPoll(ctx, runID, s.now())
	return s.Store.GetRun(ctx, runID)
}

// Rerun starts a new run with the same command. Production reruns need approval again.
func (s *Service) Rerun(ctx context.Context, a Actor, runID string) (Result, error) {
	run, t, err := s.load(ctx, runID)
	if err != nil {
		return Result{}, err
	}
	if !a.Principal.Allowed(PermissionFor(run.Action), scopeOf(t)) {
		return Result{}, fail(http.StatusForbidden, "forbidden")
	}
	switch run.Status {
	case "failed", "cancelled", "unknown", "succeeded", "rejected":
	default:
		return Result{}, fail(http.StatusConflict, "this run is still active")
	}
	return s.Request(ctx, a, run.App, run.Env, run.Action, run.ID)
}

func (s *Service) load(ctx context.Context, runID string) (store.ActionRun, Target, error) {
	run, err := s.Store.GetRun(ctx, runID)
	if err != nil {
		return run, Target{}, fail(http.StatusNotFound, "not found")
	}
	t, err := s.Resolve(ctx, run.App, run.Env)
	if err != nil {
		return run, t, err
	}
	return run, t, nil
}

// Load returns a run and its target for the API layer's own access checks.
func (s *Service) Load(ctx context.Context, runID string) (store.ActionRun, Target, error) {
	return s.load(ctx, runID)
}

func (s *Service) block(d time.Duration) {
	s.mu.Lock()
	s.blockedUntil = s.now().Add(d)
	s.mu.Unlock()
}

func (s *Service) blocked() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blockedUntil.Sub(s.now())
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
