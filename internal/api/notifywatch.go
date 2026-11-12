package api

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/poppyszn/helmsman/internal/gh"
	"github.com/poppyszn/helmsman/internal/health"
	"github.com/poppyszn/helmsman/internal/notify"
	"github.com/poppyszn/helmsman/internal/rbac"
	"github.com/poppyszn/helmsman/internal/store"
)

// watchEvery is how often the watcher looks. A change must show on two looks in a row before it is
// announced, so a blip never reaches anyone.
const watchEvery = 30 * time.Second

// RunNotifyWatcher announces what changes without a click: environments going down or recovering,
// crash loops, agents going offline or coming back, and finished deployments. It remembers what it
// announced in the database, and the first time it sees something it only records it, so restarting
// Helmsman (or a deploy of Helmsman itself) never causes a burst of notifications.
func (s *Server) RunNotifyWatcher(ctx context.Context) {
	if s.Notify == nil {
		return
	}
	t := time.NewTicker(watchEvery)
	defer t.Stop()
	var n int
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.WatchOnce(ctx, n%2 == 0) // deployments are looked at every other round (once a minute)
			n++
		}
	}
}

// WatchOnce makes one pass of the watcher. The server calls it every 30 seconds; tests call it directly.
func (s *Server) WatchOnce(ctx context.Context, deploys bool) {
	if s.Notify == nil {
		return
	}
	s.watchMu.Lock()
	if s.watch == nil {
		s.watch = &watcher{s: s, pending: map[string]pendingChange{}}
	}
	w := s.watch
	s.watchMu.Unlock()
	w.tick(ctx, deploys)
}

type pendingChange struct {
	to    string
	count int
}

type watcher struct {
	s       *Server
	pending map[string]pendingChange
}

// observe returns true when value has differed from the remembered state on two looks in a row. The first
// observation of a key is remembered without announcing anything.
func (w *watcher) observe(ctx context.Context, key, value string) (old string, changed bool) {
	cur, ok := w.s.Notify.State(ctx, key)
	if !ok {
		w.s.Notify.SetState(ctx, key, value)
		return "", false
	}
	if cur == value {
		delete(w.pending, key)
		return cur, false
	}
	// Before degraded existed the state was "ok" for healthy and degraded alike: take the new word silently.
	if cur == "ok" && (value == "healthy" || value == "degraded") {
		w.s.Notify.SetState(ctx, key, value)
		return cur, false
	}
	p := w.pending[key]
	if p.to == value {
		p.count++
	} else {
		p = pendingChange{to: value, count: 1}
	}
	w.pending[key] = p
	if p.count < 2 {
		return cur, false
	}
	delete(w.pending, key)
	w.s.Notify.SetState(ctx, key, value)
	return cur, true
}

func (w *watcher) tick(ctx context.Context, deploys bool) {
	apps, err := w.s.Store.ListApps(ctx)
	if err != nil {
		return
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
	agentApps := map[string][]string{}
	for _, a := range apps {
		for _, e := range a.Envs {
			if !slices.Contains(agentApps[e.Agent], a.ID) {
				agentApps[e.Agent] = append(agentApps[e.Agent], a.ID)
			}
			w.env(ctx, a, e)
		}
	}
	for id, list := range agentApps {
		w.agent(ctx, id, list)
	}
	if deploys {
		w.deploys(ctx, apps)
		w.workflows(ctx, apps)
	}
}

func (w *watcher) env(ctx context.Context, a store.AppRow, e store.EnvRow) {
	v, err := w.s.computeStatus(ctx, a.ID, e, rbac.Principal{}, true)
	if err != nil {
		return
	}
	where := a.ID + " / " + e.Env
	base := notify.Event{App: a.ID, Env: e.Env, Tier: e.Tier, Path: "/apps/" + a.ID + "/" + e.Env, Details: map[string]string{"environment": where}}
	// Unknown (agent offline, no fresh probe) says nothing about the app, so it is not a change.
	if v.Status != health.Unknown {
		obs := "healthy"
		switch v.Status {
		case health.Down:
			obs = "down"
		case health.Degraded:
			obs = "degraded"
		}
		if old, changed := w.observe(ctx, "env:"+a.ID+"/"+e.Env, obs); changed {
			ev := base
			switch {
			case obs == "down":
				ev.Type, ev.Title = notify.EnvDown, where+" is down"
				ev.Message = "Two health checks in a row failed, or its container is not running."
			case old == "down" || obs == "healthy":
				ev.Type, ev.Title = notify.EnvRecovered, where+" recovered"
			case obs == "degraded":
				ev.Type, ev.Title = notify.EnvDegraded, where+" is slow or unhealthy"
				ev.Message = "Its health check answers slowly, or Docker reports the container as unhealthy."
			}
			if ev.Type != "" {
				ev.Key = ev.Type + ":" + a.ID + "/" + e.Env
				w.s.Notify.Emit(ctx, ev)
			}
		}
	}
	loop := "off"
	if v.CrashLoop {
		loop = "on"
	}
	if cur, ok := w.s.Notify.State(ctx, "loop:"+a.ID+"/"+e.Env); !ok || cur != loop {
		w.s.Notify.SetState(ctx, "loop:"+a.ID+"/"+e.Env, loop)
		if ok && loop == "on" {
			ev := base
			ev.Type, ev.Title = notify.EnvCrashLoop, where+" is crash looping"
			ev.Message = "Its container exited repeatedly in the last few minutes."
			ev.Key = ev.Type + ":" + a.ID + "/" + e.Env
			w.s.Notify.Emit(ctx, ev)
		}
	}
}

func (w *watcher) agent(ctx context.Context, id string, apps []string) {
	a, err := w.s.Store.GetAgent(ctx, id)
	if err != nil || !a.Joined || a.RevokedAt != nil {
		return
	}
	online, _ := w.s.Hub.Online(id)
	obs := "offline"
	if online {
		obs = "online"
	}
	if _, changed := w.observe(ctx, "agent:"+id, obs); !changed {
		return
	}
	sort.Strings(apps)
	ev := notify.Event{Path: "/admin/agents", Apps: apps, Details: map[string]string{"agent": id, "apps": strings.Join(apps, ", ")}, Key: "agent:" + id + ":" + obs}
	if obs == "offline" {
		ev.Type, ev.Title = notify.AgentOffline, "Agent "+id+" went offline"
		ev.Message = "Status and actions for the environments on that host are unavailable until it reconnects."
	} else {
		ev.Type, ev.Title = notify.AgentOnline, "Agent "+id+" is back online"
	}
	w.s.Notify.Emit(ctx, ev)
}

// deploys announces finished runs of each repository's deploy workflow once.
func (w *watcher) deploys(ctx context.Context, apps []store.AppRow) {
	type target struct {
		repo, workflow string
		envs           []envRef
	}
	targets := map[string]*target{}
	for _, a := range apps {
		for _, e := range a.Envs {
			spec := parseSpec(e.Spec)
			if spec.Workflows.Deploy == "" {
				continue
			}
			k := a.Repo + "|" + spec.Workflows.Deploy
			t := targets[k]
			if t == nil {
				t = &target{repo: a.Repo, workflow: spec.Workflows.Deploy}
				targets[k] = t
			}
			t.envs = append(t.envs, envRef{app: a.ID, env: e.Env, tier: e.Tier, gh: spec.GitHubEnvironment})
		}
	}
	for _, t := range targets {
		if _, bad := w.s.Actions.RepoProblem(t.repo); bad {
			continue
		}
		runs, err := w.s.Actions.GH.ListRuns(ctx, t.repo, t.workflow, 10, 0)
		if err != nil {
			continue
		}
		key := "deploy:" + t.repo + "/" + t.workflow
		last, seeded := w.s.Notify.State(ctx, key)
		lastID, _ := strconv.ParseInt(last, 10, 64)
		startedKey := "deploy-started:" + t.repo + "/" + t.workflow
		startedLast, startedSeeded := w.s.Notify.State(ctx, startedKey)
		startedID, _ := strconv.ParseInt(startedLast, 10, 64)
		var done, started []gh.Run
		var newest, newestAny int64
		for _, r := range runs {
			newestAny = max(newestAny, r.ID)
			if r.Status != "completed" {
				if startedSeeded && r.ID > startedID {
					started = append(started, r)
				}
				continue
			}
			newest = max(newest, r.ID)
			if seeded && r.ID > lastID {
				done = append(done, r)
			}
		}
		if newestAny > startedID {
			w.s.Notify.SetState(ctx, startedKey, strconv.FormatInt(newestAny, 10))
		}
		sort.Slice(started, func(i, j int) bool { return started[i].ID < started[j].ID })
		for _, r := range started {
			w.deployEvent(ctx, t.repo, t.workflow, t.envs, r, true)
		}
		if !seeded {
			// First time this workflow is seen: remember where it is, announce nothing.
			w.s.Notify.SetState(ctx, key, strconv.FormatInt(newest, 10))
			continue
		}
		sort.Slice(done, func(i, j int) bool { return done[i].ID < done[j].ID })
		for _, r := range done {
			w.deployEvent(ctx, t.repo, t.workflow, t.envs, r, false)
		}
		if newest > lastID {
			w.s.Notify.SetState(ctx, key, strconv.FormatInt(newest, 10))
		}
	}
}

type envRef struct{ app, env, tier, gh string }

func (w *watcher) deployEvent(ctx context.Context, repo, workflow string, envs []envRef, r gh.Run, started bool) {
	var typ, word string
	switch {
	case started:
		typ, word = notify.DeployStarted, "started"
	case r.Conclusion == "success":
		typ, word = notify.DeploySucceeded, "succeeded"
	case r.Conclusion == "cancelled" || r.Conclusion == "skipped" || r.Conclusion == "neutral":
		return
	default:
		typ, word = notify.DeployFailed, "failed"
	}
	ev := notify.Event{Type: typ, Path: "/apps/", Key: fmt.Sprintf("%s:%s/%s:%d", typ, repo, workflow, r.ID),
		Details: map[string]string{"repository": repo, "branch": r.HeadBranch, "commit": short(r.HeadSHA), "started by": r.Actor.Login, "run": r.HTMLURL}}
	// Which environment? The branch names it for the usual develop and production setups. When it does not,
	// and every environment of the workflow has the same tier, that tier still decides who is told.
	match := -1
	for i, e := range envs {
		if r.HeadBranch != "" && (e.gh == r.HeadBranch || e.env == r.HeadBranch) {
			match = i
			break
		}
	}
	switch {
	case match >= 0:
		ev.App, ev.Env, ev.Tier = envs[match].app, envs[match].env, envs[match].tier
		ev.Path = "/apps/" + ev.App + "/" + ev.Env + "/deployments"
	case len(envs) > 0:
		tier := envs[0].tier
		same := true
		for _, e := range envs {
			same = same && e.tier == tier
		}
		if same {
			ev.Tier = tier
		}
		ev.App = envs[0].app
		ev.Path = "/apps/" + ev.App
		for _, e := range envs {
			if !slices.Contains(ev.Apps, e.app) {
				ev.Apps = append(ev.Apps, e.app)
			}
		}
	}
	where := repo
	if ev.Env != "" {
		where = ev.App + " / " + ev.Env
	}
	ev.Title = "Deployment " + word + ": " + where
	if r.HeadBranch != "" {
		ev.Message = "The " + workflow + " workflow " + word + " on " + r.HeadBranch + " (" + short(r.HeadSHA) + ")."
	}
	w.s.Notify.Emit(ctx, ev)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// workflows announces finished runs of the other GitHub workflows of each app repository (tests, builds,
// and so on), once. The deploy and migrate workflows named in the catalog have their own events and are
// left out here.
func (w *watcher) workflows(ctx context.Context, apps []store.AppRow) {
	type repoInfo struct {
		apps    []string
		covered map[string]bool
	}
	repos := map[string]*repoInfo{}
	for _, a := range apps {
		ri := repos[a.Repo]
		if ri == nil {
			ri = &repoInfo{covered: map[string]bool{}}
			repos[a.Repo] = ri
		}
		if !slices.Contains(ri.apps, a.ID) {
			ri.apps = append(ri.apps, a.ID)
		}
		for _, e := range a.Envs {
			spec := parseSpec(e.Spec)
			ri.covered[spec.Workflows.Deploy] = true
			ri.covered[spec.Workflows.Migrate] = true
		}
	}
	for repo, ri := range repos {
		if _, bad := w.s.Actions.RepoProblem(repo); bad {
			continue
		}
		runs, err := w.s.Actions.GH.ListRepoRuns(ctx, repo, 20)
		if err != nil {
			continue
		}
		key := "workflow:" + repo
		last, seeded := w.s.Notify.State(ctx, key)
		lastID, _ := strconv.ParseInt(last, 10, 64)
		var newest int64
		var done []gh.Run
		for _, r := range runs {
			if r.Status != "completed" {
				continue
			}
			newest = max(newest, r.ID)
			if seeded && r.ID > lastID {
				done = append(done, r)
			}
		}
		if !seeded {
			w.s.Notify.SetState(ctx, key, strconv.FormatInt(newest, 10))
			continue
		}
		sort.Slice(done, func(i, j int) bool { return done[i].ID < done[j].ID })
		sort.Strings(ri.apps)
		for _, r := range done {
			file := r.Path
			if i := strings.IndexByte(file, '@'); i >= 0 {
				file = file[:i]
			}
			file = file[strings.LastIndexByte(file, '/')+1:]
			if ri.covered[file] {
				continue
			}
			typ, word := notify.WorkflowFailed, "failed"
			switch r.Conclusion {
			case "success":
				typ, word = notify.WorkflowSucceeded, "succeeded"
			case "cancelled", "skipped", "neutral":
				continue
			}
			name := r.Name
			if name == "" {
				name = file
			}
			w.s.Notify.Emit(ctx, notify.Event{Type: typ, Apps: ri.apps, Path: "/apps/" + ri.apps[0],
				Key:   fmt.Sprintf("%s:%s:%d", typ, repo, r.ID),
				Title: "Workflow " + word + ": " + name + " in " + repo,
				Details: map[string]string{"repository": repo, "workflow": name, "branch": r.HeadBranch, "commit": short(r.HeadSHA),
					"started by": r.Actor.Login, "run": r.HTMLURL}})
		}
		if newest > lastID {
			w.s.Notify.SetState(ctx, key, strconv.FormatInt(newest, 10))
		}
	}
}
