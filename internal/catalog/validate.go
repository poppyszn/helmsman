package catalog

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	fileRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.ya?ml$`)
)

// Validate returns every problem found. agentExists reports whether an agent id
// is registered in the IDP.
func Validate(c *Catalog, agentExists func(id string) bool) []error {
	var errs []error
	add := func(f string, a ...any) { errs = append(errs, fmt.Errorf(f, a...)) }

	if c.Version != 1 {
		add("version must be 1, got %d", c.Version)
	}
	apps := map[string]bool{}
	for i, a := range c.Apps {
		at := fmt.Sprintf("apps[%d] (%s)", i, a.ID)
		switch {
		case !slugRE.MatchString(a.ID):
			add("%s: id must be a lowercase slug", at)
		case apps[a.ID]:
			add("%s: duplicate app id", at)
		}
		apps[a.ID] = true
		if a.Name == "" {
			add("%s: name is required", at)
		}
		if !repoRE.MatchString(a.Repo) {
			add("%s: repo must be owner/repo", at)
		}
		checkLinks(&errs, at+" links", a.Links)
		if len(a.Environments) == 0 {
			add("%s: at least one environment is required", at)
		}
		envs := map[string]bool{}
		for j, e := range a.Environments {
			et := fmt.Sprintf("%s env[%d] (%s)", at, j, e.Env)
			switch {
			case !slugRE.MatchString(e.Env):
				add("%s: env must be a lowercase slug", et)
			case e.Env == "*":
				add("%s: env name cannot be *", et)
			case envs[e.Env]:
				add("%s: duplicate env", et)
			}
			envs[e.Env] = true
			if e.Tier != "nonprod" && e.Tier != "prod" {
				add("%s: tier must be nonprod or prod", et)
			}
			if e.Agent == "" {
				add("%s: agent is required", et)
			} else if !agentExists(e.Agent) {
				add("%s: unknown agent %q", et, e.Agent)
			}
			if e.Container.ComposeProject == "" || e.Container.Service == "" {
				add("%s: container.compose_project and container.service are required", et)
			}
			checkURL(&errs, et+" url", e.URL, true)
			checkURL(&errs, et+" health.url", e.Health.URL, true)
			checkLinks(&errs, et+" links", e.Links)
			if e.Health.Via != "server" && e.Health.Via != "agent" {
				add("%s: health.via must be server or agent", et)
			}
			if e.Workflows.Deploy != "" && !fileRE.MatchString(e.Workflows.Deploy) {
				add("%s: workflows.deploy must be a workflow file name", et)
			}
			if !fileRE.MatchString(e.Workflows.Migrate) {
				add("%s: workflows.migrate must be a workflow file name", et)
			}
			if e.GitHubEnvironment == "" {
				add("%s: github_environment is required", et)
			}
			if e.Logs.FaroApp != "" && !faroAppRE.MatchString(e.Logs.FaroApp) {
				add("%s: logs.faro_app may use letters, digits, dot, dash and underscore (up to 100)", et)
			}
		}
	}
	return errs
}

// checkLinks keeps the shortcuts tidy: a short name, an http(s) address, and no more than ten.
func checkLinks(errs *[]error, field string, links []Link) {
	if len(links) > 10 {
		*errs = append(*errs, fmt.Errorf("%s: at most 10 links", field))
	}
	for i, l := range links {
		if n := len([]rune(strings.TrimSpace(l.Name))); n < 1 || n > 40 {
			*errs = append(*errs, fmt.Errorf("%s[%d]: name must be 1 to 40 characters", field, i))
		}
		checkURL(errs, fmt.Sprintf("%s[%d] url", field, i), l.URL, true)
	}
}

var faroAppRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

func checkURL(errs *[]error, field, v string, required bool) {
	if v == "" {
		if required {
			*errs = append(*errs, fmt.Errorf("%s is required", field))
		}
		return
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		*errs = append(*errs, fmt.Errorf("%s must be an http(s) URL", field))
	}
}

// JoinErrors renders a validation error list for display.
func JoinErrors(errs []error) string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "\n")
}
