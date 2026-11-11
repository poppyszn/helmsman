package actions

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/poppyszn/helmsman/internal/catalog"
	"github.com/poppyszn/helmsman/internal/gh"
)

// WorkflowCheck is the result of looking at one workflow file.
type WorkflowCheck struct {
	File        string `json:"file"`
	Role        string `json:"role"` // migrate | deploy
	Exists      bool   `json:"exists"`
	HasDispatch bool   `json:"has_dispatch"`
	Error       string `json:"error,omitempty"`
}

// RepoCheck is the startup validation of one repository.
type RepoCheck struct {
	Repo      string          `json:"repo"`
	Installed bool            `json:"installed"`
	Workflows []WorkflowCheck `json:"workflows"`
	Problem   string          `json:"problem"` // empty when actions are enabled
	CheckedAt time.Time       `json:"checked_at"`
}

// RepoProblem says why actions are disabled for a repository. Repositories that have not
// been checked yet are not blocked: a real failure surfaces on the run itself.
func (s *Service) RepoProblem(repo string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.checks[repo]
	if !ok || c.Problem == "" {
		return "", false
	}
	return c.Problem, true
}

func (s *Service) setProblem(repo, problem string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.checks[repo]
	c.Repo, c.Problem, c.CheckedAt = repo, problem, s.now()
	s.checks[repo] = c
}

// Checks returns the latest results, sorted by repository.
func (s *Service) Checks() []RepoCheck {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RepoCheck, 0, len(s.checks))
	for _, c := range s.checks {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })
	return out
}

// CheckAll validates every repository in the catalog: the App is installed, a token can be
// minted, and the migrate workflow exists and has a workflow_dispatch trigger. A repository
// that fails has its actions disabled and the reason shown on the admin page.
func (s *Service) CheckAll(ctx context.Context) []RepoCheck {
	apps, err := s.Store.ListApps(ctx)
	if err != nil {
		s.Log.Error("github check: list apps", "err", err)
		return s.Checks()
	}
	type want struct{ file, role string }
	repos := map[string]map[want]bool{}
	for _, a := range apps {
		if repos[a.Repo] == nil {
			repos[a.Repo] = map[want]bool{}
		}
		for _, e := range a.Envs {
			var spec catalog.Env
			_ = jsonUnmarshal(e.Spec, &spec)
			if spec.Workflows.Migrate != "" {
				repos[a.Repo][want{spec.Workflows.Migrate, "migrate"}] = true
			}
			if spec.Workflows.Deploy != "" {
				repos[a.Repo][want{spec.Workflows.Deploy, "deploy"}] = true
			}
		}
	}
	fresh := map[string]RepoCheck{}
	for repo, files := range repos {
		c := RepoCheck{Repo: repo, CheckedAt: s.now()}
		if err := s.GH.CheckAccess(ctx, repo); err != nil {
			c.Problem = s.describe(repo, err)
			fresh[repo] = c
			continue
		}
		c.Installed = true
		var list []want
		for f := range files {
			list = append(list, f)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].file < list[j].file })
		for _, w := range list {
			wc := WorkflowCheck{File: w.file, Role: w.role}
			text, err := s.GH.WorkflowFile(ctx, repo, w.file)
			switch {
			case gh.IsNotFound(err):
			case err != nil:
				wc.Error = err.Error()
			default:
				wc.Exists, wc.HasDispatch = true, gh.HasDispatchTrigger(text)
			}
			if w.role == "migrate" && c.Problem == "" {
				switch {
				case wc.Error != "":
					c.Problem = "could not read " + w.file + ": " + wc.Error
				case !wc.Exists:
					c.Problem = "workflow " + w.file + " was not found in .github/workflows"
				case !wc.HasDispatch:
					c.Problem = "workflow " + w.file + " has no workflow_dispatch trigger"
				}
			}
			c.Workflows = append(c.Workflows, wc)
		}
		fresh[repo] = c
	}
	s.mu.Lock()
	s.checks = fresh
	s.mu.Unlock()
	return s.Checks()
}

// WatchRepos validates on startup and then every interval. While any repository is failing it
// retries every minute, so a GitHub outage at startup or a freshly installed App is picked up
// without waiting for the full interval.
func (s *Service) WatchRepos(ctx context.Context, every time.Duration) {
	for {
		wait := every
		for _, c := range s.CheckAll(ctx) {
			if c.Problem != "" {
				wait = min(every, time.Minute)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
