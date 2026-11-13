package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/actions"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/rbac"
	"github.com/poppyszn/helmsman/internal/store"
)

// runView is a run plus what the caller may do with it.
type runView struct {
	store.ActionRun
	WorkflowURL string `json:"workflow_url"`
	CanApprove  bool   `json:"can_approve"`
	CanReject   bool   `json:"can_reject"`
	CanCancel   bool   `json:"can_cancel"`
	CanRerun    bool   `json:"can_rerun"`
}

func (s *Server) actor(r *http.Request) actions.Actor {
	c := rc(r)
	return actions.Actor{User: c.User, Principal: c.Principal, IP: auth.ClientIP(r)}
}

func (s *Server) viewRun(r store.ActionRun, p rbac.Principal, uid int64) runView {
	sc := rbac.Scope{AppID: r.App, Env: r.Env, Tier: r.Tier}
	v := runView{ActionRun: r, WorkflowURL: "https://github.com/" + r.Repo + "/actions/workflows/" + r.Workflow}
	perm := actions.PermissionFor(r.Action)
	can := p.Allowed(perm, sc)
	pending := r.Status == "pending_approval"
	v.CanApprove = pending && p.CanApprove(sc, r.RequestedBy, uid)
	v.CanReject = pending && (v.CanApprove || r.RequestedBy == uid)
	v.CanCancel = can && r.GHRunID != nil && (r.Status == "queued" || r.Status == "in_progress" || r.Status == "waiting")
	switch r.Status {
	case "failed", "cancelled", "unknown", "succeeded", "rejected":
		v.CanRerun = can
	}
	return v
}

func (s *Server) writeActionErr(w http.ResponseWriter, err error) {
	var ae *actions.Error
	if errors.As(err, &ae) {
		body := map[string]any{"error": ae.Msg}
		if ae.Run != nil {
			body["active_run"] = ae.Run.ID
		}
		writeJSON(w, ae.Status, body)
		return
	}
	writeErr(w, http.StatusInternalServerError, "internal error")
}

// envRuns is the Runs tab: recent runs, and whether actions are usable at all.
func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	app, env := chi.URLParam(r, "app"), chi.URLParam(r, "env")
	t, err := s.Actions.Resolve(r.Context(), app, env)
	if err != nil {
		s.writeActionErr(w, err)
		return
	}
	runs, err := s.Store.ListRuns(r.Context(), t.Env.ID, 30)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	c := rc(r)
	sc := rbac.Scope{AppID: app, Env: env, Tier: t.Env.Tier}
	out := make([]runView, 0, len(runs))
	for _, x := range runs {
		out = append(out, s.viewRun(x, c.Principal, c.User.ID))
	}
	problem, disabled := s.Actions.RepoProblem(t.App.Repo)
	writeJSON(w, http.StatusOK, map[string]any{
		"runs":            out,
		"can_migrate":     c.Principal.Allowed(rbac.ActionMigrate, sc) && !disabled,
		"can_seed":        c.Principal.Allowed(rbac.ActionSeed, sc) && t.Spec.SeedAllowed() && !disabled,
		"seed_allowed":    t.Spec.SeedAllowed(),
		"needs_approval":  t.Spec.NeedsApproval(),
		"disabled_reason": problem,
		"repo":            t.App.Repo,
	})
}

func (s *Server) startRun(command string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := s.Actions.Request(r.Context(), s.actor(r), chi.URLParam(r, "app"), chi.URLParam(r, "env"), command, "")
		if err != nil {
			s.writeActionErr(w, err)
			return
		}
		c := rc(r)
		code := http.StatusAccepted
		if res.Deduped {
			code = http.StatusOK
		}
		writeJSON(w, code, map[string]any{"run": s.viewRun(res.Run, c.Principal, c.User.ID), "deduped": res.Deduped})
	}
}

// runInEnv loads a run and makes sure it belongs to the env in the URL, so a permission
// on one env can never reach another env's run by guessing its id.
func (s *Server) runInEnv(w http.ResponseWriter, r *http.Request) (store.ActionRun, bool) {
	run, _, err := s.Actions.Load(r.Context(), chi.URLParam(r, "id"))
	if err != nil || run.App != chi.URLParam(r, "app") || run.Env != chi.URLParam(r, "env") {
		writeErr(w, http.StatusNotFound, "not found")
		return run, false
	}
	return run, true
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	run, ok := s.runInEnv(w, r)
	if !ok {
		return
	}
	c := rc(r)
	writeJSON(w, http.StatusOK, s.viewRun(run, c.Principal, c.User.ID))
}

func (s *Server) handleRunLogs(w http.ResponseWriter, r *http.Request) {
	run, ok := s.runInEnv(w, r)
	if !ok {
		return
	}
	logs, err := s.Store.RunLogs(r.Context(), run.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) runOp(op func(r *http.Request, a actions.Actor, id string) (store.ActionRun, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.runInEnv(w, r); !ok {
			return
		}
		run, err := op(r, s.actor(r), chi.URLParam(r, "id"))
		if err != nil {
			s.writeActionErr(w, err)
			return
		}
		c := rc(r)
		writeJSON(w, http.StatusOK, map[string]any{"run": s.viewRun(run, c.Principal, c.User.ID)})
	}
}

func (s *Server) handleApprove() http.HandlerFunc {
	return s.runOp(func(r *http.Request, a actions.Actor, id string) (store.ActionRun, error) {
		return s.Actions.Approve(r.Context(), a, id)
	})
}

func (s *Server) handleReject() http.HandlerFunc {
	return s.runOp(func(r *http.Request, a actions.Actor, id string) (store.ActionRun, error) {
		return s.Actions.Reject(r.Context(), a, id, "")
	})
}

func (s *Server) handleCancel() http.HandlerFunc {
	return s.runOp(func(r *http.Request, a actions.Actor, id string) (store.ActionRun, error) {
		return s.Actions.Cancel(r.Context(), a, id)
	})
}

func (s *Server) handleRerun(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.runInEnv(w, r); !ok {
		return
	}
	res, err := s.Actions.Rerun(r.Context(), s.actor(r), chi.URLParam(r, "id"))
	if err != nil {
		s.writeActionErr(w, err)
		return
	}
	c := rc(r)
	writeJSON(w, http.StatusAccepted, map[string]any{"run": s.viewRun(res.Run, c.Principal, c.User.ID)})
}

func (s *Server) handleDeployments(w http.ResponseWriter, r *http.Request) {
	v, err := s.Actions.Deployments(r.Context(), chi.URLParam(r, "app"), chi.URLParam(r, "env"))
	if err != nil {
		s.writeActionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func deployIDs(r *http.Request) (run, job int64, ok bool) {
	run, err := strconv.ParseInt(chi.URLParam(r, "run"), 10, 64)
	if err != nil || run <= 0 {
		return 0, 0, false
	}
	if j := chi.URLParam(r, "job"); j != "" {
		job, err = strconv.ParseInt(j, 10, 64)
		if err != nil || job <= 0 {
			return 0, 0, false
		}
	}
	return run, job, true
}

// handleDeploymentJobs lists the jobs and steps of one deploy run.
func (s *Server) handleDeploymentJobs(w http.ResponseWriter, r *http.Request) {
	run, _, ok := deployIDs(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	jobs, err := s.Actions.DeploymentJobs(r.Context(), chi.URLParam(r, "app"), chi.URLParam(r, "env"), run)
	if err != nil {
		s.writeActionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

// handleDeploymentJobLog returns the tail of one job's log, read live from GitHub.
func (s *Server) handleDeploymentJobLog(w http.ResponseWriter, r *http.Request) {
	run, job, ok := deployIDs(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	text, cut, err := s.Actions.DeploymentJobLog(r.Context(), s.actor(r), chi.URLParam(r, "app"), chi.URLParam(r, "env"), run, job)
	if err != nil {
		s.writeActionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": text, "truncated": cut})
}

// handleApprovals lists pending production requests the caller may decide on.
func (s *Server) handleApprovals(w http.ResponseWriter, r *http.Request) {
	runs, err := s.Store.PendingApprovals(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	c := rc(r)
	out := []runView{}
	for _, x := range runs {
		v := s.viewRun(x, c.Principal, c.User.ID)
		if v.CanApprove || v.CanReject {
			out = append(out, v)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

// orgView is one organization on the GitHub page: whether people may sign in from it, who has, and how many
// catalog apps live in it. People signed in is the proof that the OAuth App is approved for the organization.
type orgView struct {
	Org     string   `json:"org"`
	Allowed bool     `json:"allowed"`
	Members []string `json:"members"`
	Apps    int      `json:"apps"`
	// PolicyURL is where an owner approves the OAuth App for the organization.
	PolicyURL string `json:"policy_url,omitempty"`
}

// githubOrgs lists the allowed organizations and any other organization that owns a catalog repository.
func (s *Server) githubOrgs(ctx context.Context) []orgView {
	byOrg := map[string]*orgView{}
	get := func(org string) *orgView {
		k := strings.ToLower(org)
		if v, ok := byOrg[k]; ok {
			return v
		}
		v := &orgView{Org: org, Members: []string{}}
		if s.Cfg.GitHubMode == "real" {
			v.PolicyURL = "https://github.com/organizations/" + org + "/settings/oauth_application_policy"
		}
		byOrg[k] = v
		return v
	}
	for _, o := range s.Cfg.AllowedOrgs {
		get(o).Allowed = true
	}
	if apps, err := s.Store.ListApps(ctx); err == nil {
		for _, a := range apps {
			if org, _, ok := strings.Cut(a.Repo, "/"); ok {
				get(org).Apps++
			}
		}
	}
	if rows, err := s.Store.Pool.Query(ctx, `SELECT o.org, u.login FROM user_orgs o JOIN users u ON u.id=o.user_id WHERE u.disabled_at IS NULL ORDER BY u.login`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var org, login string
			if rows.Scan(&org, &login) == nil {
				v := get(org)
				v.Members = append(v.Members, login)
			}
		}
	}
	out := make([]orgView, 0, len(byOrg))
	for _, v := range byOrg {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Org) < strings.ToLower(out[j].Org) })
	return out
}

// githubAuthorizeURL is the page where a person grants the OAuth App access to their organizations.
func (s *Server) githubAuthorizeURL() string {
	if s.Cfg.GitHubMode != "real" || s.Cfg.OAuthClientID == "" {
		return ""
	}
	return "https://github.com/settings/connections/applications/" + s.Cfg.OAuthClientID
}

func (s *Server) githubPayload(ctx context.Context, checks any) map[string]any {
	return map[string]any{"mode": s.Cfg.GitHubMode, "api_url": s.Cfg.GitHubAppAPIURL, "repos": checks,
		"orgs": s.githubOrgs(ctx), "authorize_url": s.githubAuthorizeURL()}
}

func (s *Server) handleGitHubStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.githubPayload(r.Context(), s.Actions.Checks()))
}

func (s *Server) handleGitHubCheck(w http.ResponseWriter, r *http.Request) {
	checks := s.Actions.CheckAll(r.Context())
	c := rc(r)
	_ = s.Audit.Write(r.Context(), auditEvent(c.User.Login, "github.check", "github", auth.ClientIP(r), map[string]any{"repos": len(checks)}))
	writeJSON(w, http.StatusOK, s.githubPayload(r.Context(), checks))
}

func auditEvent(actor, action, target, ip string, d map[string]any) audit.Event {
	return audit.Event{ActorType: audit.ActorUser, ActorID: actor, Action: action, Target: target, Result: "ok", IP: ip, Details: d}
}
