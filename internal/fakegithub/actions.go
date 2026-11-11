package fakegithub

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Scenario scripts how the fake Actions API misbehaves. Zero values mean "normal":
// the run appears at once, is queued briefly, runs briefly and succeeds.
type Scenario struct {
	// Dispatch: "" (accept) | "never_appears" (204 but no run is ever created) | "422".
	Dispatch string `json:"dispatch"`
	// AppearMS delays the run showing up in the run list (GitHub takes seconds).
	AppearMS int `json:"appear_ms"`
	QueuedMS int `json:"queued_ms"`
	RunMS    int `json:"run_ms"`
	// Conclusion of the run: success (default) | failure.
	Conclusion string `json:"conclusion"`
	// Waiting holds a run in the "waiting" state until /_fake/runs/{id}/approve.
	Waiting bool `json:"waiting"`
	// RateLimit makes the next Count API calls answer 403 with Retry-After.
	RateLimitCount      int `json:"rate_limit_count"`
	RateLimitRetryAfter int `json:"rate_limit_retry_after"`
}

type fakeRepo struct {
	DefaultBranch string
	Installed     bool
	Tags          map[string]string // tag -> commit sha
	Workflows     map[string]string // file -> yaml
}

type fakeRun struct {
	ID         int64
	Repo       string
	Workflow   string
	Title      string
	Event      string
	HeadSHA    string
	Actor      string
	Command    string
	CreatedAt  time.Time
	AppearAt   time.Time
	StartAt    time.Time
	EndAt      time.Time
	Conclusion string
	Waiting    bool
	Cancelled  bool
	CancelAt   time.Time
}

// Dispatch records one workflow_dispatch call, for test assertions.
type Dispatch struct {
	Repo     string            `json:"repo"`
	Workflow string            `json:"workflow"`
	Ref      string            `json:"ref"`
	Inputs   map[string]string `json:"inputs"`
}

const dispatchYAML = "name: %s\nrun-name: \"${{ inputs.command }} ${{ inputs.environment }} [${{ inputs.correlation_id }}]\"\non:\n  workflow_dispatch:\n    inputs:\n      environment: { type: string }\n"

type actionsState struct {
	repos      map[string]*fakeRepo
	runs       []*fakeRun
	nextRun    int64
	scenario   Scenario
	dispatches []Dispatch
	appTokens  map[string]bool // issued installation tokens (valid)
	now        func() time.Time
}

func newActionsState() *actionsState {
	a := &actionsState{repos: map[string]*fakeRepo{}, appTokens: map[string]bool{}, nextRun: 7000, now: time.Now}
	a.repos["acme-org/demo-app"] = &fakeRepo{
		DefaultBranch: "main", Installed: true,
		Tags:      map[string]string{"v1.0.0": "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"},
		Workflows: map[string]string{"deploy.yml": fmt.Sprintf(dispatchYAML, "deploy"), "migrate.yml": fmt.Sprintf(dispatchYAML, "migrate")},
	}
	// A little deploy history so the Deployments tab has something in dev.
	base := a.now().Add(-26 * time.Hour)
	for i, sha := range []string{"3f9c1d27aa0b4c5d6e7f8091a2b3c4d5e6f70811", "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"} {
		t := base.Add(time.Duration(i) * 12 * time.Hour)
		a.runs = append(a.runs, &fakeRun{ID: a.nextRun, Repo: "acme-org/demo-app", Workflow: "deploy.yml", Title: "deploy staging", Event: "push",
			HeadSHA: sha, Actor: "bob", CreatedAt: t, AppearAt: t, StartAt: t, EndAt: t.Add(3 * time.Minute), Conclusion: "success", Command: "deploy"})
		a.nextRun++
	}
	return a
}

// state returns the actions state, creating it lazily so Server literals in tests still work.
func (s *Server) actions() *actionsState {
	if s.act == nil {
		s.act = newActionsState()
	}
	return s.act
}

func (s *Server) registerActions(m *http.ServeMux) {
	m.HandleFunc("GET /repos/{owner}/{repo}/installation", s.appJWT(s.getInstallation))
	m.HandleFunc("POST /app/installations/{id}/access_tokens", s.appJWT(s.mintToken))
	m.HandleFunc("GET /repos/{owner}/{repo}", s.repoAPI(s.getRepo))
	m.HandleFunc("GET /repos/{owner}/{repo}/tags", s.repoAPI(s.getTags))
	m.HandleFunc("GET /repos/{owner}/{repo}/contents/.github/workflows/{file}", s.repoAPI(s.getWorkflowFile))
	m.HandleFunc("POST /repos/{owner}/{repo}/actions/workflows/{file}/dispatches", s.repoAPI(s.dispatch))
	m.HandleFunc("GET /repos/{owner}/{repo}/actions/workflows/{file}/runs", s.repoAPI(s.listRuns))
	m.HandleFunc("GET /repos/{owner}/{repo}/actions/runs/{id}", s.repoAPI(s.getRun))
	m.HandleFunc("GET /repos/{owner}/{repo}/actions/runs/{id}/jobs", s.repoAPI(s.getJobs))
	m.HandleFunc("GET /repos/{owner}/{repo}/actions/jobs/{id}/logs", s.repoAPI(s.getJobLogs))
	m.HandleFunc("POST /repos/{owner}/{repo}/actions/runs/{id}/cancel", s.repoAPI(s.cancelRun))
	m.HandleFunc("PUT /_fake/scenario", s.putScenario)
	m.HandleFunc("PUT /_fake/repos/{owner}/{repo}", s.putRepo)
	m.HandleFunc("POST /_fake/expire-tokens", s.expireTokens)
	m.HandleFunc("POST /_fake/runs/{id}/approve", s.approveRun)
	m.HandleFunc("GET /_fake/dispatches", s.getDispatches)
	m.HandleFunc("POST /_fake/reset-actions", s.resetActions)
}

// --- scripting helpers usable from Go tests ---

// SetScenario replaces the scenario.
func (s *Server) SetScenario(sc Scenario) { s.mu.Lock(); s.actions().scenario = sc; s.mu.Unlock() }

// Dispatches returns the dispatch calls received so far.
func (s *Server) Dispatches() []Dispatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Dispatch(nil), s.actions().dispatches...)
}

// ExpireTokens invalidates every issued installation token.
func (s *Server) ExpireTokens() {
	s.mu.Lock()
	s.actions().appTokens = map[string]bool{}
	s.mu.Unlock()
}

// ApproveRun releases a waiting run.
func (s *Server) ApproveRun(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.actions()
	for _, r := range a.runs {
		if r.ID == id && r.Waiting {
			r.Waiting = false
			st := a.now().Add(time.Duration(a.scenario.QueuedMS) * time.Millisecond)
			r.StartAt, r.EndAt = st, st.Add(time.Duration(a.scenario.RunMS)*time.Millisecond)
		}
	}
}

// SetRepo creates or updates a repository.
func (s *Server) SetRepo(name string, installed bool, workflows map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.actions()
	r := a.repos[name]
	if r == nil {
		r = &fakeRepo{DefaultBranch: "main", Tags: map[string]string{}}
		a.repos[name] = r
	}
	r.Installed = installed
	if workflows != nil {
		r.Workflows = workflows
	}
}

// SetTag points a tag at a commit.
func (s *Server) SetTag(repo, tag, sha string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions().repos[repo].Tags[tag] = sha
}

// --- auth ---

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// appJWT accepts any three-part token as the App JWT (the fake does not verify signatures).
func (s *Server) appJWT(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(strings.Split(bearer(r), ".")) != 3 {
			writeJSON(w, 401, map[string]string{"message": "A JSON web token could not be decoded"})
			return
		}
		h(w, r)
	}
}

// repoAPI requires a valid installation token, applies the rate-limit scenario and
// takes the lock for the handler.
func (s *Server) repoAPI(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		a := s.actions()
		ok := a.appTokens[bearer(r)]
		limited := a.scenario.RateLimitCount > 0
		retry := a.scenario.RateLimitRetryAfter
		if limited {
			a.scenario.RateLimitCount--
		}
		s.mu.Unlock()
		if !ok {
			writeJSON(w, 401, map[string]string{"message": "Bad credentials"})
			return
		}
		if limited {
			if retry <= 0 {
				retry = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			w.Header().Set("X-RateLimit-Remaining", "0")
			writeJSON(w, 403, map[string]string{"message": "You have exceeded a secondary rate limit."})
			return
		}
		h(w, r)
	}
}

func repoName(r *http.Request) string { return r.PathValue("owner") + "/" + r.PathValue("repo") }

func (s *Server) getInstallation(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	rp := s.actions().repos[repoName(r)]
	s.mu.Unlock()
	if rp == nil || !rp.Installed {
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
		return
	}
	writeJSON(w, 200, map[string]any{"id": 4242})
}

func (s *Server) mintToken(w http.ResponseWriter, _ *http.Request) {
	tok := "ghs_" + randHex()
	s.mu.Lock()
	s.actions().appTokens[tok] = true
	s.mu.Unlock()
	writeJSON(w, 201, map[string]any{"token": tok, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
}

// withETag answers 304 when the client already holds this exact body.
func withETag(w http.ResponseWriter, r *http.Request, v any) {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	et := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", et)
	if r.Header.Get("If-None-Match") == et {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func (s *Server) repo(w http.ResponseWriter, r *http.Request) (*fakeRepo, bool) {
	s.mu.Lock()
	rp := s.actions().repos[repoName(r)]
	s.mu.Unlock()
	if rp == nil {
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
		return nil, false
	}
	return rp, true
}

func (s *Server) getRepo(w http.ResponseWriter, r *http.Request) {
	if rp, ok := s.repo(w, r); ok {
		withETag(w, r, map[string]any{"full_name": repoName(r), "default_branch": rp.DefaultBranch})
	}
}

func (s *Server) getTags(w http.ResponseWriter, r *http.Request) {
	rp, ok := s.repo(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	names := make([]string, 0, len(rp.Tags))
	for t := range rp.Tags {
		names = append(names, t)
	}
	sort.Strings(names)
	out := []map[string]any{}
	for _, t := range names {
		out = append(out, map[string]any{"name": t, "commit": map[string]string{"sha": rp.Tags[t]}})
	}
	s.mu.Unlock()
	withETag(w, r, out)
}

func (s *Server) getWorkflowFile(w http.ResponseWriter, r *http.Request) {
	rp, ok := s.repo(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	y, has := rp.Workflows[r.PathValue("file")]
	s.mu.Unlock()
	if !has {
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
		return
	}
	withETag(w, r, map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(y))})
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	rp, ok := s.repo(w, r)
	if !ok {
		return
	}
	var body struct {
		Ref    string            `json:"ref"`
		Inputs map[string]string `json:"inputs"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeJSON(w, 400, map[string]string{"message": "Problems parsing JSON"})
		return
	}
	file := r.PathValue("file")
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.actions()
	y, has := rp.Workflows[file]
	if !has {
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
		return
	}
	a.dispatches = append(a.dispatches, Dispatch{Repo: repoName(r), Workflow: file, Ref: body.Ref, Inputs: body.Inputs})
	if !strings.Contains(y, "workflow_dispatch") {
		writeJSON(w, 422, map[string]string{"message": "Workflow does not have 'workflow_dispatch' trigger"})
		return
	}
	_, isTag := rp.Tags[body.Ref]
	if a.scenario.Dispatch == "422" || (body.Ref != rp.DefaultBranch && !isTag) {
		writeJSON(w, 422, map[string]string{"message": "Unexpected inputs provided or ref not found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
	if a.scenario.Dispatch == "never_appears" {
		return
	}
	now := a.now()
	sha := "9d8c7b6a5f4e3d2c1b0a99887766554433221100"
	if t, ok := rp.Tags[body.Ref]; ok {
		sha = t
	}
	sc := a.scenario
	run := &fakeRun{ID: a.nextRun, Repo: repoName(r), Workflow: file, Event: "workflow_dispatch", HeadSHA: sha, Actor: "helmsman[bot]",
		Command: body.Inputs["command"], CreatedAt: now, AppearAt: now.Add(time.Duration(sc.AppearMS) * time.Millisecond),
		Conclusion: sc.Conclusion, Waiting: sc.Waiting}
	a.nextRun++
	run.Title = fmt.Sprintf("%s %s [%s]", body.Inputs["command"], body.Inputs["environment"], body.Inputs["correlation_id"])
	if run.Conclusion == "" {
		run.Conclusion = "success"
	}
	run.StartAt = run.AppearAt.Add(time.Duration(sc.QueuedMS) * time.Millisecond)
	run.EndAt = run.StartAt.Add(time.Duration(sc.RunMS) * time.Millisecond)
	a.runs = append(a.runs, run)
}

type outRun struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Title      string    `json:"display_title"`
	Status     string    `json:"status"`
	Conclusion *string   `json:"conclusion"`
	HeadSHA    string    `json:"head_sha"`
	HTMLURL    string    `json:"html_url"`
	Event      string    `json:"event"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Actor      struct {
		Login string `json:"login"`
	} `json:"actor"`
}

// view computes a run's state at time t. Caller holds the lock.
func (a *actionsState) view(r *fakeRun, t time.Time) outRun {
	o := outRun{ID: r.ID, Name: r.Workflow, Title: r.Title, HeadSHA: r.HeadSHA, Event: r.Event, CreatedAt: r.CreatedAt, UpdatedAt: t,
		HTMLURL: fmt.Sprintf("https://github.example/%s/actions/runs/%d", r.Repo, r.ID)}
	o.Actor.Login = r.Actor
	done := func(c string) { o.Status = "completed"; o.Conclusion = &c }
	switch {
	case r.Cancelled && !t.Before(r.CancelAt):
		done("cancelled")
	case r.Waiting:
		o.Status = "waiting"
	case t.Before(r.StartAt):
		o.Status = "queued"
	case t.Before(r.EndAt):
		o.Status = "in_progress"
	default:
		done(r.Conclusion)
	}
	return o
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	a := s.actions()
	now := a.now()
	out := []outRun{}
	for i := len(a.runs) - 1; i >= 0; i-- {
		x := a.runs[i]
		if x.Repo == repoName(r) && x.Workflow == r.PathValue("file") && !now.Before(x.AppearAt) {
			v := a.view(x, now)
			v.UpdatedAt = x.CreatedAt // stable body while nothing changes, so ETags can hit
			out = append(out, v)
		}
	}
	s.mu.Unlock()
	withETag(w, r, map[string]any{"total_count": len(out), "workflow_runs": out})
}

func (s *Server) findRun(r *http.Request) *fakeRun {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	for _, x := range s.actions().runs {
		if x.ID == id && x.Repo == repoName(r) {
			return x
		}
	}
	return nil
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	x := s.findRun(r)
	var v outRun
	if x != nil {
		v = s.actions().view(x, s.actions().now())
		v.UpdatedAt = x.CreatedAt
	}
	s.mu.Unlock()
	if x == nil {
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
		return
	}
	withETag(w, r, v)
}

func (s *Server) getJobs(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	x := s.findRun(r)
	var jobs []map[string]any
	if x != nil {
		a := s.actions()
		v := a.view(x, a.now())
		jobStatus, conc := v.Status, ""
		if v.Conclusion != nil {
			conc = *v.Conclusion
		}
		steps := []map[string]any{}
		names := []string{"Set up job", "Run " + x.Command, "Complete job"}
		for i, n := range names {
			st, c := "completed", "success"
			switch {
			case jobStatus == "queued" || jobStatus == "waiting":
				st, c = "queued", ""
			case jobStatus == "in_progress" && i >= 1:
				st, c = "in_progress", ""
				if i == 2 {
					st = "pending"
				}
			case jobStatus == "completed" && conc == "failure" && i == 1:
				c = "failure"
			case jobStatus == "completed" && conc == "cancelled" && i >= 1:
				c = "cancelled"
			}
			steps = append(steps, map[string]any{"name": n, "status": st, "conclusion": nullable(c), "number": i + 1})
		}
		jobs = []map[string]any{{"id": x.ID*10 + 1, "name": "run", "status": jobStatus, "conclusion": nullable(conc),
			"html_url": v.HTMLURL, "started_at": x.StartAt, "steps": steps}}
	}
	s.mu.Unlock()
	if x == nil {
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
		return
	}
	withETag(w, r, map[string]any{"total_count": len(jobs), "jobs": jobs})
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Server) getJobLogs(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	s.mu.Lock()
	var x *fakeRun
	for _, c := range s.actions().runs {
		if c.ID*10+1 == id && c.Repo == repoName(r) {
			x = c
		}
	}
	var v outRun
	if x != nil {
		v = s.actions().view(x, s.actions().now())
	}
	s.mu.Unlock()
	if x == nil {
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
		return
	}
	ts := x.StartAt.UTC().Format(time.RFC3339)
	out := fmt.Sprintf("%s Set up job\n%s Run %s\n%s applying changes\n", ts, ts, x.Command, ts)
	if v.Conclusion != nil && *v.Conclusion == "failure" {
		out += ts + " Error: " + x.Command + " failed\n"
	} else if v.Conclusion != nil {
		out += ts + " done\n"
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(out))
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	x := s.findRun(r)
	if x != nil {
		x.Cancelled, x.CancelAt, x.Waiting = true, s.actions().now(), false
	}
	s.mu.Unlock()
	if x == nil {
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// --- /_fake controls ---

func (s *Server) putScenario(w http.ResponseWriter, r *http.Request) {
	var sc Scenario
	if json.NewDecoder(r.Body).Decode(&sc) != nil {
		http.Error(w, "bad json", 400)
		return
	}
	s.SetScenario(sc)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) putRepo(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Installed bool              `json:"installed"`
		Workflows map[string]string `json:"workflows"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		http.Error(w, "bad json", 400)
		return
	}
	s.SetRepo(repoName(r), b.Installed, b.Workflows)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) expireTokens(w http.ResponseWriter, _ *http.Request) {
	s.ExpireTokens()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) approveRun(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	s.ApproveRun(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getDispatches(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.Dispatches())
}

func (s *Server) resetActions(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.act = newActionsState()
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// AddRun inserts a finished historical run (for example a past deploy).
func (s *Server) AddRun(repo, workflow, sha, actor, conclusion string, at time.Time) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.actions()
	r := &fakeRun{ID: a.nextRun, Repo: repo, Workflow: workflow, Title: "deploy", Event: "push", HeadSHA: sha, Actor: actor,
		CreatedAt: at, AppearAt: at, StartAt: at, EndAt: at.Add(time.Minute), Conclusion: conclusion, Command: "deploy"}
	a.nextRun++
	a.runs = append(a.runs, r)
	return r.ID
}
