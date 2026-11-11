package api_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/poppyszn/helmsman/internal/actions"
	"github.com/poppyszn/helmsman/internal/api"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/fakegithub"
	"github.com/poppyszn/helmsman/internal/gh"
	"github.com/poppyszn/helmsman/internal/store"
)

func newTestActions(t *testing.T, st *store.Store, aw *audit.Writer, ghURL string) *actions.Service {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	api := gh.NewClient(ghURL, &gh.AppAuth{AppID: "1", Key: key, APIURL: ghURL})
	s := actions.New(st, api, aw, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.CorrelateEvery, s.CorrelateFor, s.PollEvery = 20*time.Millisecond, 500*time.Millisecond, 20*time.Millisecond
	return s
}

const dispatchWF = "name: x\non:\n  workflow_dispatch:\n"

// runsEnv prepares the fake GitHub for billing-api and signs in the usual cast.
type runsEnv struct {
	*env
	alice, bob, erin *client
}

func newRunsEnv(t *testing.T) *runsEnv {
	t.Helper()
	e := newEnv(t)
	e.gh.SetRepo("acme-org/billing-api", true, map[string]string{"deploy.yml": dispatchWF, "migrate.yml": dispatchWF})
	e.gh.SetRepo("acme-org/ledger", true, map[string]string{"migrate.yml": dispatchWF})
	r := &runsEnv{env: e, alice: e.client(), bob: e.client(), erin: e.client()}
	r.alice.login("alice")
	r.bob.login("bob")
	r.erin.login("erin")
	e.bind("user", itoa(e.userID("bob")), "developer", "billing-api", "*")
	e.bind("user", itoa(e.userID("erin")), "approver", "billing-api", "*")
	e.act.CheckAll(context.Background())
	return r
}

type runResp struct {
	Run struct {
		ID          string  `json:"id"`
		Status      string  `json:"status"`
		Action      string  `json:"action"`
		Ref         *string `json:"ref"`
		Warning     *string `json:"warning"`
		Error       *string `json:"error"`
		GHRunURL    *string `json:"gh_run_url"`
		WorkflowURL string  `json:"workflow_url"`
		RerunOf     *string `json:"rerun_of"`
		CanApprove  bool    `json:"can_approve"`
		CanCancel   bool    `json:"can_cancel"`
		CanRerun    bool    `json:"can_rerun"`
		Jobs        []struct {
			Name  string `json:"name"`
			Steps []struct {
				Name string `json:"name"`
			} `json:"steps"`
		} `json:"jobs"`
	} `json:"run"`
	Deduped   bool   `json:"deduped"`
	ActiveRun string `json:"active_run"`
	Error     string `json:"error"`
}

func (r *runsEnv) start(c *client, app, env, cmd string) (int, runResp) {
	var out runResp
	code := c.json("POST", "/api/apps/"+app+"/envs/"+env+"/actions/"+cmd, map[string]any{}, &out)
	return code, out
}

// settle polls the service until the run reaches the wanted status.
func (r *runsEnv) settle(id, want string) store.ActionRun {
	r.t.Helper()
	var last store.ActionRun
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		r.act.Poll(context.Background())
		var err error
		if last, err = r.st.GetRun(context.Background(), id); err != nil {
			r.t.Fatal(err)
		}
		if last.Status == want {
			return last
		}
		time.Sleep(25 * time.Millisecond)
	}
	r.t.Fatalf("run %s stuck in %q, wanted %q (error: %v)", id, last.Status, want, last.Error)
	return last
}

func (r *runsEnv) auditActions() []string {
	rows, err := r.st.Pool.Query(context.Background(), `SELECT action || ':' || result FROM audit_events WHERE action LIKE 'action.run.%' ORDER BY id`)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestMigrateOnStagingRunsToSuccessAndKeepsLogs(t *testing.T) {
	r := newRunsEnv(t)
	code, out := r.start(r.bob, "billing-api", "staging", "migrate")
	if code != 202 || out.Run.Status != "dispatching" && out.Run.Status != "queued" {
		t.Fatalf("start: %d %+v", code, out)
	}
	if out.Run.Ref == nil || *out.Run.Ref != "main" || out.Run.Warning == nil {
		t.Fatalf("with no known revision the default branch is used with a warning: %+v", out.Run)
	}
	d := r.gh.Dispatches()
	if len(d) != 1 || d[0].Inputs["environment"] != "staging" || d[0].Inputs["command"] != "migrate" ||
		d[0].Inputs["requested_by"] != "bob" || d[0].Inputs["correlation_id"] == "" || d[0].Workflow != "migrate.yml" {
		t.Fatalf("dispatch inputs wrong: %+v", d)
	}
	done := r.settle(out.Run.ID, "succeeded")
	if done.GHRunURL == nil || !strings.Contains(*done.GHRunURL, "/actions/runs/") || done.FinishedAt == nil {
		t.Fatalf("finished run incomplete: %+v", done)
	}
	var detail runResp
	if c := r.bob.json("GET", "/api/apps/billing-api/envs/staging/runs/"+out.Run.ID, nil, &detail.Run); c != 200 || len(detail.Run.Jobs) != 1 || len(detail.Run.Jobs[0].Steps) != 3 {
		t.Fatalf("detail: %d %+v", c, detail.Run)
	}
	var logs struct {
		Logs []struct{ Content string } `json:"logs"`
	}
	if c := r.bob.json("GET", "/api/apps/billing-api/envs/staging/runs/"+out.Run.ID+"/logs", nil, &logs); c != 200 || len(logs.Logs) != 1 || !strings.Contains(logs.Logs[0].Content, "Run migrate") {
		t.Fatalf("logs: %d %+v", c, logs)
	}
	got := strings.Join(r.auditActions(), ",")
	for _, w := range []string{"action.run.requested:ok", "action.run.dispatched:ok", "action.run.completed:ok"} {
		if !strings.Contains(got, w) {
			t.Fatalf("audit missing %s in %s", w, got)
		}
	}
}

func TestRefIsTheTagOfTheDeployedVersion(t *testing.T) {
	r := newRunsEnv(t)
	sha := "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	r.gh.SetTag("acme-org/billing-api", "v2.3.1", sha)
	_, err := r.st.Pool.Exec(context.Background(), `INSERT INTO container_state (agent_id, compose_project, service, container_id, revision, state)
		VALUES ('agent-1','billing','api','c1',$1,'running')`, sha)
	if err != nil {
		t.Fatal(err)
	}
	_, out := r.start(r.bob, "billing-api", "staging", "migrate")
	if out.Run.Ref == nil || *out.Run.Ref != "v2.3.1" || out.Run.Warning != nil {
		t.Fatalf("expected tag ref without warning: %+v", out.Run)
	}
}

func TestDoubleClickIsDeduped(t *testing.T) {
	r := newRunsEnv(t)
	r.gh.SetScenario(fakegithub.Scenario{RunMS: 60000})
	_, a := r.start(r.bob, "billing-api", "staging", "migrate")
	code, b := r.start(r.bob, "billing-api", "staging", "migrate")
	if code != 200 || !b.Deduped || b.Run.ID != a.Run.ID || len(r.gh.Dispatches()) != 1 {
		t.Fatalf("second click must return the first run: %d %+v dispatches=%d", code, b, len(r.gh.Dispatches()))
	}
}

func TestOnlyOneActiveRunPerEnv(t *testing.T) {
	r := newRunsEnv(t)
	r.gh.SetScenario(fakegithub.Scenario{RunMS: 60000})
	_, a := r.start(r.bob, "billing-api", "staging", "migrate")
	code, b := r.start(r.alice, "billing-api", "staging", "migrate")
	if code != 409 || b.ActiveRun != a.Run.ID {
		t.Fatalf("a second person must be refused while one run is active: %d %+v", code, b)
	}
	if len(r.gh.Dispatches()) != 1 {
		t.Fatal("only one dispatch expected")
	}
}

func TestProductionNeedsASecondPerson(t *testing.T) {
	r := newRunsEnv(t)
	r.bind("user", itoa(r.userID("bob")), "developer", "billing-api", "prod")
	r.bind("user", itoa(r.userID("bob")), "approver", "billing-api", "prod")
	r.bind("user", itoa(r.userID("erin")), "approver", "billing-api", "prod")

	code, out := r.start(r.bob, "billing-api", "prod", "migrate")
	if code != 202 || out.Run.Status != "pending_approval" || len(r.gh.Dispatches()) != 0 {
		t.Fatalf("prod request must wait: %d %+v dispatches=%d", code, out, len(r.gh.Dispatches()))
	}
	base := "/api/apps/billing-api/envs/prod/runs/" + out.Run.ID
	var self runResp
	if c := r.bob.json("POST", base+"/approve", map[string]any{}, &self); c != 403 || !strings.Contains(self.Error, "own request") {
		t.Fatalf("self approval must be 403: %d %+v", c, self)
	}
	if len(r.gh.Dispatches()) != 0 {
		t.Fatal("nothing may be dispatched yet")
	}
	// An admin who is also the requester is no exception.
	var ok runResp
	if c := r.erin.json("POST", base+"/approve", map[string]any{}, &ok); c != 200 {
		t.Fatalf("approval by a second person: %d %+v", c, ok)
	}
	d := r.gh.Dispatches()
	if len(d) != 1 || d[0].Inputs["environment"] != "production" {
		t.Fatalf("approved run must dispatch to production: %+v", d)
	}
	r.settle(out.Run.ID, "succeeded")
	var again runResp
	if c := r.erin.json("POST", base+"/approve", map[string]any{}, &again); c != 409 {
		t.Fatalf("approving twice: %d", c)
	}
	got := strings.Join(r.auditActions(), ",")
	if !strings.Contains(got, "action.run.approved:denied") || !strings.Contains(got, "action.run.approved:ok") {
		t.Fatalf("both the refusal and the approval must be audited: %s", got)
	}
}

func TestProductionRejectAndWithdraw(t *testing.T) {
	r := newRunsEnv(t)
	r.bind("user", itoa(r.userID("bob")), "developer", "billing-api", "prod")
	r.bind("user", itoa(r.userID("erin")), "approver", "billing-api", "prod")
	_, a := r.start(r.bob, "billing-api", "prod", "migrate")
	var rej runResp
	if c := r.erin.json("POST", "/api/apps/billing-api/envs/prod/runs/"+a.Run.ID+"/reject", map[string]any{}, &rej); c != 200 || rej.Run.Status != "rejected" {
		t.Fatalf("reject: %d %+v", c, rej)
	}
	_, b := r.start(r.bob, "billing-api", "prod", "migrate")
	var w runResp
	if c := r.bob.json("POST", "/api/apps/billing-api/envs/prod/runs/"+b.Run.ID+"/reject", map[string]any{}, &w); c != 200 || w.Run.Status != "rejected" {
		t.Fatalf("a requester may withdraw: %d %+v", c, w)
	}
	if len(r.gh.Dispatches()) != 0 {
		t.Fatal("rejected runs must never dispatch")
	}
}

func TestSeedRefusedWhenEnvForbidsItEvenForAdmins(t *testing.T) {
	r := newRunsEnv(t)
	if code, out := r.start(r.alice, "billing-api", "prod", "seed"); code != 403 {
		t.Fatalf("admin seeding prod must be refused: %d %+v", code, out)
	}
	if code, _ := r.start(r.bob, "billing-api", "staging", "seed"); code != 202 {
		t.Fatalf("seed on staging should work: %d", code)
	}
	if d := r.gh.Dispatches(); len(d) != 1 || d[0].Inputs["command"] != "seed" {
		t.Fatalf("dispatches: %+v", d)
	}
}

func TestRunIsFoundEvenWhenGitHubIsSlowToShowIt(t *testing.T) {
	r := newRunsEnv(t)
	r.gh.SetScenario(fakegithub.Scenario{AppearMS: 200})
	_, out := r.start(r.bob, "billing-api", "staging", "migrate")
	r.settle(out.Run.ID, "succeeded")
}

func TestRunThatNeverAppearsBecomesUnknownWithALink(t *testing.T) {
	r := newRunsEnv(t)
	r.gh.SetScenario(fakegithub.Scenario{Dispatch: "never_appears"})
	_, out := r.start(r.bob, "billing-api", "staging", "migrate")
	run := r.settle(out.Run.ID, "unknown")
	if run.Error == nil || !strings.Contains(*run.Error, "did not report a run") {
		t.Fatalf("explanation missing: %+v", run.Error)
	}
	var d runResp
	r.bob.json("GET", "/api/apps/billing-api/envs/staging/runs/"+out.Run.ID, nil, &d.Run)
	if !strings.Contains(d.Run.WorkflowURL, "acme-org/billing-api/actions/workflows/migrate.yml") {
		t.Fatalf("workflow link: %q", d.Run.WorkflowURL)
	}
	// An unknown run frees the environment.
	r.gh.SetScenario(fakegithub.Scenario{})
	if code, _ := r.start(r.alice, "billing-api", "staging", "migrate"); code != 202 {
		t.Fatalf("env should be free again: %d", code)
	}
}

func TestDispatchRejectedByGitHubFailsClearly(t *testing.T) {
	r := newRunsEnv(t)
	r.gh.SetScenario(fakegithub.Scenario{Dispatch: "422"})
	_, out := r.start(r.bob, "billing-api", "staging", "migrate")
	if out.Run.Status != "failed" || out.Run.Error == nil || !strings.Contains(*out.Run.Error, "rejected the dispatch") {
		t.Fatalf("422 must fail the run with an explanation: %+v", out.Run)
	}
}

func TestRateLimitOnDispatchAndOnPolling(t *testing.T) {
	r := newRunsEnv(t)
	r.gh.SetScenario(fakegithub.Scenario{RateLimitCount: 1, RateLimitRetryAfter: 1})
	_, out := r.start(r.bob, "billing-api", "staging", "migrate")
	if out.Run.Status != "failed" || out.Run.Error == nil || !strings.Contains(*out.Run.Error, "rate limit") {
		t.Fatalf("dispatch under a rate limit must fail with a retry hint: %+v", out.Run)
	}
	time.Sleep(1100 * time.Millisecond)

	// Polling honours Retry-After and then carries on.
	r.gh.SetScenario(fakegithub.Scenario{RunMS: 150})
	_, ok := r.start(r.alice, "billing-api", "staging", "migrate")
	r.gh.SetScenario(fakegithub.Scenario{RateLimitCount: 1, RateLimitRetryAfter: 1})
	r.settle(ok.Run.ID, "succeeded")
}

func TestExpiredInstallationTokenIsRefreshedTransparently(t *testing.T) {
	r := newRunsEnv(t)
	if code, _ := r.start(r.bob, "billing-api", "staging", "migrate"); code != 202 {
		t.Fatal(code)
	}
	r.gh.ExpireTokens()
	r.gh.SetScenario(fakegithub.Scenario{})
	r.act.Poll(context.Background())
	runs, _ := r.st.ListRuns(context.Background(), 1, 5)
	if len(runs) == 0 {
		t.Fatal("no runs")
	}
	r.settle(runs[0].ID, "succeeded") // GetRun hits a 401, drops the token and retries once
	r.gh.ExpireTokens()
	r.gh.SetScenario(fakegithub.Scenario{})
	time.Sleep(11 * time.Second / 10)
	if code, out := r.start(r.alice, "billing-api", "staging", "seed"); code != 202 || out.Run.Status == "failed" {
		t.Fatalf("dispatch after token expiry: %d %+v", code, out.Run)
	}
}

func TestWaitingForEnvironmentApprovalThenFinishes(t *testing.T) {
	r := newRunsEnv(t)
	r.gh.SetScenario(fakegithub.Scenario{Waiting: true})
	_, out := r.start(r.bob, "billing-api", "staging", "migrate")
	run := r.settle(out.Run.ID, "waiting")
	if run.GHRunID == nil {
		t.Fatal("waiting run must be linked")
	}
	r.gh.ApproveRun(*run.GHRunID)
	r.settle(out.Run.ID, "succeeded")
}

func TestCancelThenRerun(t *testing.T) {
	r := newRunsEnv(t)
	r.gh.SetScenario(fakegithub.Scenario{RunMS: 60000})
	_, out := r.start(r.bob, "billing-api", "staging", "migrate")
	r.settle(out.Run.ID, "in_progress")
	base := "/api/apps/billing-api/envs/staging/runs/" + out.Run.ID
	if c := r.erin.json("POST", base+"/cancel", map[string]any{}, nil); c != 403 {
		t.Fatalf("an approver without action.migrate cannot cancel: %d", c)
	}
	var cx runResp
	if c := r.bob.json("POST", base+"/cancel", map[string]any{}, &cx); c != 200 {
		t.Fatalf("cancel: %d %+v", c, cx)
	}
	r.settle(out.Run.ID, "cancelled")
	r.gh.SetScenario(fakegithub.Scenario{})
	var again runResp
	if c := r.bob.json("POST", base+"/rerun", map[string]any{}, &again); c != 202 || again.Run.RerunOf == nil || *again.Run.RerunOf != out.Run.ID {
		t.Fatalf("rerun: %d %+v", c, again)
	}
	r.settle(again.Run.ID, "succeeded")
}

func TestFailedWorkflowIsFailedAndKeepsItsLog(t *testing.T) {
	r := newRunsEnv(t)
	r.gh.SetScenario(fakegithub.Scenario{Conclusion: "failure"})
	_, out := r.start(r.bob, "billing-api", "staging", "migrate")
	run := r.settle(out.Run.ID, "failed")
	logs, _ := r.st.RunLogs(context.Background(), run.ID)
	if len(logs) != 1 || !strings.Contains(logs[0].Content, "Error: migrate failed") {
		t.Fatalf("failure log missing: %+v", logs)
	}
}

func TestRepoWithoutInstallationOrDispatchTriggerIsDisabled(t *testing.T) {
	r := newRunsEnv(t)
	r.gh.SetRepo("acme-org/ledger", false, nil)
	r.act.CheckAll(context.Background())
	code, out := r.start(r.alice, "ledger", "staging", "migrate")
	if code != 409 || !strings.Contains(out.Error, "not installed") {
		t.Fatalf("uninstalled repo: %d %+v", code, out)
	}
	r.gh.SetRepo("acme-org/ledger", true, map[string]string{"migrate.yml": "name: x\non: push\n"})
	r.act.CheckAll(context.Background())
	if code, out := r.start(r.alice, "ledger", "staging", "migrate"); code != 409 || !strings.Contains(out.Error, "workflow_dispatch") {
		t.Fatalf("missing trigger: %d %+v", code, out)
	}
	var gh struct {
		Repos []struct {
			Repo    string `json:"repo"`
			Problem string `json:"problem"`
		} `json:"repos"`
	}
	if c := r.alice.json("GET", "/api/admin/github", nil, &gh); c != 200 || len(gh.Repos) != 2 {
		t.Fatalf("admin page: %d %+v", c, gh)
	}
	if code, _ := r.start(r.bob, "billing-api", "staging", "migrate"); code != 202 {
		t.Fatalf("other repos keep working: %d", code)
	}
}

func TestDeploymentsAndRevisionMismatch(t *testing.T) {
	r := newRunsEnv(t)
	now := time.Now()
	r.gh.AddRun("acme-org/billing-api", "deploy.yml", "1111111111111111111111111111111111111111", "bob", "success", now.Add(-2*time.Hour))
	r.gh.AddRun("acme-org/billing-api", "deploy.yml", "2222222222222222222222222222222222222222", "bob", "success", now.Add(-time.Hour))
	r.gh.AddRun("acme-org/billing-api", "deploy.yml", "3333333333333333333333333333333333333333", "bob", "failure", now.Add(-30*time.Minute))
	_, err := r.st.Pool.Exec(context.Background(), `INSERT INTO container_state (agent_id, compose_project, service, container_id, revision, state)
		VALUES ('agent-1','billing','api','c1','1111111',  'running')`)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Configured  bool   `json:"configured"`
		Mismatch    bool   `json:"mismatch"`
		Latest      string `json:"latest_success_commit"`
		Deployments []struct {
			Status string `json:"status"`
			Commit string `json:"commit"`
		} `json:"deployments"`
	}
	if c := r.bob.json("GET", "/api/apps/billing-api/envs/staging/deployments", nil, &v); c != 200 {
		t.Fatalf("deployments: %d", c)
	}
	if !v.Configured || len(v.Deployments) != 3 || v.Deployments[0].Status != "failed" || !strings.HasPrefix(v.Latest, "2222") || !v.Mismatch {
		t.Fatalf("expected a mismatch badge: %+v", v)
	}
	// ledger has no deploy workflow in the catalog.
	r.bind("user", itoa(r.userID("bob")), "viewer", "ledger", "*")
	var none struct {
		Configured bool `json:"configured"`
	}
	if c := r.bob.json("GET", "/api/apps/ledger/envs/staging/deployments", nil, &none); c != 200 || none.Configured {
		t.Fatalf("no deploy workflow: %d %+v", c, none)
	}
}

func TestRunsAreScopedToTheirEnv(t *testing.T) {
	r := newRunsEnv(t)
	r.bind("user", itoa(r.userID("bob")), "viewer", "billing-api", "prod")
	_, out := r.start(r.bob, "billing-api", "staging", "migrate")
	if c := r.bob.json("GET", "/api/apps/billing-api/envs/prod/runs/"+out.Run.ID, nil, nil); c != 404 {
		t.Fatalf("a run id must not be readable through another env: %d", c)
	}
	var ap struct {
		Runs []struct{ ID string } `json:"runs"`
	}
	if c := r.erin.json("GET", "/api/approvals", nil, &ap); c != 200 || len(ap.Runs) != 0 {
		t.Fatalf("approvals: %d %+v", c, ap)
	}
}

func TestInFlightRunsResumeAfterRestart(t *testing.T) {
	r := newRunsEnv(t)
	r.gh.SetScenario(fakegithub.Scenario{RunMS: 400})
	_, out := r.start(r.bob, "billing-api", "staging", "migrate")
	// A brand new service instance (as after a restart) picks the run up from the database.
	fresh := newTestActions(t, r.st, &audit.Writer{Pool: r.st.Pool}, r.ghURL)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		fresh.Poll(context.Background())
		if run, _ := r.st.GetRun(context.Background(), out.Run.ID); run.Status == "succeeded" {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("run was not resumed")
}

func TestActionsAndLogOpensAreRateLimitedPerUser(t *testing.T) {
	r := newRunsEnv(t)
	d := r.deps
	d.ActionsPerMinute, d.LogOpensPerMinute = 3, 2
	srv, err := api.New(d)
	if err != nil {
		t.Fatal(err)
	}
	r.swap(srv.Handler())
	r.gh.SetScenario(fakegithub.Scenario{RunMS: 60000})
	got := []int{}
	for i := 0; i < 5; i++ {
		code, _ := r.start(r.bob, "billing-api", "staging", "migrate")
		got = append(got, code)
	}
	if got[3] != 429 || got[4] != 429 || got[0] == 429 {
		t.Fatalf("the 4th action in a minute must be 429: %v", got)
	}
	// Another user has their own budget, and reads are not counted.
	if code, _ := r.start(r.alice, "billing-api", "staging", "migrate"); code == 429 {
		t.Fatal("limits are per user")
	}
	for i := 0; i < 6; i++ {
		if c := r.bob.json("GET", "/api/apps", nil, nil); c != 200 {
			t.Fatalf("reads must not be limited: %d", c)
		}
	}
	for i, want := range []int{409, 409, 429} {
		if c := r.bob.json("GET", "/ws/apps/billing-api/envs/staging/logs", nil, nil); c != want {
			t.Fatalf("log open %d: got %d want %d (agent offline, then limited)", i, c, want)
		}
	}
}
