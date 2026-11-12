package api_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/poppyszn/helmsman/internal/fakegithub"
	"github.com/poppyszn/helmsman/internal/notify"
)

type hookCall struct {
	URL, Secret, Event string
	P                  notify.Payload
}

type fakeHook struct {
	mu    sync.Mutex
	calls []hookCall
	fail  int // how many calls fail before one succeeds
}

func (f *fakeHook) Post(_ context.Context, url, secret, event string, p notify.Payload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail > 0 {
		f.fail--
		return errString("the receiver answered HTTP 500")
	}
	f.calls = append(f.calls, hookCall{url, secret, event, p})
	return nil
}

func (f *fakeHook) all() []hookCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]hookCall{}, f.calls...)
}

type mailCall struct {
	To            []string
	Subject, Body string
}

type fakeMail struct {
	mu    sync.Mutex
	calls []mailCall
}

func (f *fakeMail) Send(_ context.Context, to []string, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, mailCall{to, subject, body})
	return nil
}

func (f *fakeMail) all() []mailCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mailCall{}, f.calls...)
}

type errString string

func (e errString) Error() string { return string(e) }

type chanResp struct {
	Channel struct {
		ID        int64    `json:"id"`
		Name      string   `json:"name"`
		Kind      string   `json:"kind"`
		HasSecret bool     `json:"has_secret"`
		Events    []string `json:"events"`
		Tier      string   `json:"tier"`
	} `json:"channel"`
	Secret string `json:"secret"`
	Error  string `json:"error"`
}

func TestNotificationChannelsAreAdminOnlyAndValidated(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.client(), e.client()
	alice.login("alice")
	bob.login("bob")
	e.bind("user", itoa(e.userID("bob")), "developer", "*", "*")
	for _, c := range []struct{ m, p string }{{"GET", "/api/admin/notifications"}, {"POST", "/api/admin/notifications/channels"}, {"PUT", "/api/admin/notifications/channels/1"},
		{"DELETE", "/api/admin/notifications/channels/1"}, {"POST", "/api/admin/notifications/channels/1/test"}, {"PUT", "/api/admin/notifications/smtp"}, {"POST", "/api/admin/notifications/smtp/test"}} {
		if code := bob.json(c.m, c.p, map[string]any{}, nil); code != 403 {
			t.Errorf("%s %s: %d", c.m, c.p, code)
		}
	}
	bad := []map[string]any{
		{"name": "", "kind": "webhook", "url": "https://x.example.com", "events": []string{"env.down"}},
		{"name": "a", "kind": "webhook", "url": "http://169.254.169.254/latest", "events": []string{"env.down"}},
		{"name": "a", "kind": "webhook", "url": "https://user:pw@x.example.com", "events": []string{"env.down"}},
		{"name": "a", "kind": "webhook", "url": "ftp://x.example.com", "events": []string{"env.down"}},
		{"name": "a", "kind": "webhook", "url": "https://x.example.com", "events": []string{}},
		{"name": "a", "kind": "webhook", "url": "https://x.example.com", "events": []string{"made.up"}},
		{"name": "a", "kind": "email", "to": []string{"bad"}, "events": []string{"env.down"}},
		{"name": "a", "kind": "email", "to": []string{"a@x.com\r\nBcc: z@y.com"}, "events": []string{"env.down"}},
		{"name": "a", "kind": "email", "to": []string{}, "events": []string{"env.down"}},
		{"name": "a", "kind": "pigeon", "events": []string{"env.down"}},
	}
	for i, b := range bad {
		if code := alice.json("POST", "/api/admin/notifications/channels", b, nil); code != 422 {
			t.Errorf("bad input %d must be refused: %d", i, code)
		}
	}
	var c chanResp
	if code := alice.json("POST", "/api/admin/notifications/channels", map[string]any{"name": "Ops hook", "kind": "webhook", "url": "https://hooks.example.com/x", "events": []string{"env.down", "run.failed"}, "enabled": true}, &c); code != 201 {
		t.Fatalf("create: %d %+v", code, c)
	}
	if c.Secret == "" || !c.Channel.HasSecret || c.Channel.Tier != "all" {
		t.Fatalf("a signing secret is generated and returned once: %+v", c)
	}
	var list struct {
		Channels []struct {
			Name      string `json:"name"`
			HasSecret bool   `json:"has_secret"`
		} `json:"channels"`
	}
	raw := alice.do("GET", e.idp.URL+"/api/admin/notifications", nil)
	body := make([]byte, 1<<16)
	n, _ := raw.Body.Read(body)
	if strings.Contains(string(body[:n]), c.Secret) {
		t.Fatal("the signing secret must never be returned again")
	}
	_ = list
	if code := alice.json("POST", "/api/admin/notifications/channels", map[string]any{"name": "ops HOOK", "kind": "webhook", "url": "https://hooks.example.com/y", "events": []string{"env.down"}}, nil); code != 422 {
		t.Fatalf("names are unique, ignoring case: %d", code)
	}
}

func TestEventsReachMatchingChannelsOnceAndRetry(t *testing.T) {
	e := newEnv(t)
	alice := e.client()
	alice.login("alice")
	ctx := context.Background()
	mk := func(body map[string]any) int64 {
		var c chanResp
		if code := alice.json("POST", "/api/admin/notifications/channels", body, &c); code != 201 {
			t.Fatalf("create %v: %d %+v", body["name"], code, c)
		}
		return c.Channel.ID
	}
	mk(map[string]any{"name": "everything", "kind": "webhook", "url": "https://hooks.example.com/all", "events": []string{"env.down", "run.failed"}, "enabled": true})
	mk(map[string]any{"name": "prod only", "kind": "email", "to": []string{"oncall@example.com"}, "events": []string{"env.down"}, "tier": "prod", "enabled": true})
	mk(map[string]any{"name": "off", "kind": "webhook", "url": "https://hooks.example.com/off", "events": []string{"env.down"}, "enabled": false})

	nonprod := notify.Event{Type: notify.EnvDown, App: "billing-api", Env: "staging", Tier: "nonprod", Title: "billing-api / staging is down", Path: "/apps/billing-api"}
	e.notify.Emit(ctx, nonprod)
	e.notify.Emit(ctx, nonprod) // the same thing twice
	prod := nonprod
	prod.Env, prod.Tier, prod.Title = "prod", "prod", "billing-api / prod is down"
	e.notify.Emit(ctx, prod)
	e.notify.SendDue(ctx)

	hooks, mails := e.hooks.all(), e.mails.all()
	if len(hooks) != 2 {
		t.Fatalf("the enabled webhook gets the staging and the prod event once each, the disabled one nothing: %+v", hooks)
	}
	if hooks[0].Secret == "" || hooks[0].Event != "env.down" || hooks[0].P.URL != e.idp.URL+"/apps/billing-api" || hooks[0].P.App != "billing-api" {
		t.Fatalf("payload: %+v", hooks[0])
	}
	if len(mails) != 1 || mails[0].To[0] != "oncall@example.com" || !strings.Contains(mails[0].Subject, "prod is down") || !strings.Contains(mails[0].Body, e.idp.URL+"/apps/billing-api") {
		t.Fatalf("the prod-only email channel gets only the prod event: %+v", mails)
	}
	// A flapping environment is held back: the same event again within ten minutes is not queued.
	e.notify.Emit(ctx, prod)
	e.notify.SendDue(ctx)
	if len(e.hooks.all()) != 2 {
		t.Fatalf("a repeat within the cooldown must not send: %d", len(e.hooks.all()))
	}

	// A receiver that fails is retried; after enough failures the delivery is marked failed.
	e.hooks.fail = 100
	other := notify.Event{Type: notify.RunFailed, App: "billing-api", Env: "staging", Tier: "nonprod", Title: "Migration failed", Key: "run.failed:run:x"}
	e.notify.Emit(ctx, other)
	for i := 0; i < 5; i++ {
		e.notify.SendDue(ctx)
		if _, err := e.st.Pool.Exec(ctx, `UPDATE notification_deliveries SET next_attempt_at = now() - interval '1 second' WHERE status = 'pending'`); err != nil {
			t.Fatal(err)
		}
	}
	var status string
	var attempts int
	if err := e.st.Pool.QueryRow(ctx, `SELECT status, attempts FROM notification_deliveries WHERE dedupe_key = 'run.failed:run:x'`).Scan(&status, &attempts); err != nil || status != "failed" || attempts != 4 {
		t.Fatalf("after repeated failures: %s %d %v", status, attempts, err)
	}
	var d struct {
		Deliveries []struct {
			Status    string `json:"status"`
			LastError string `json:"last_error"`
		} `json:"deliveries"`
	}
	if code := alice.json("GET", "/api/admin/notifications", nil, &d); code != 200 || len(d.Deliveries) == 0 {
		t.Fatalf("deliveries list: %d", code)
	}
	if d.Deliveries[0].Status != "failed" || !strings.Contains(d.Deliveries[0].LastError, "HTTP 500") {
		t.Fatalf("the log shows why: %+v", d.Deliveries[0])
	}

	// Deleting a channel removes its history with it.
	if code := alice.json("DELETE", "/api/admin/notifications/channels/1", nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
}

func TestRunsNotifyWhenTheyNeedApprovalOrFail(t *testing.T) {
	r := newRunsEnv(t)
	ctx := context.Background()
	var c chanResp
	if code := r.alice.json("POST", "/api/admin/notifications/channels", map[string]any{"name": "runs", "kind": "email", "to": []string{"team@example.com"},
		"events": []string{"run.failed", "run.pending_approval"}, "enabled": true}, &c); code != 201 {
		t.Fatalf("create: %d", code)
	}
	// A production run waits for approval, and that is announced.
	r.bind("user", itoa(r.userID("bob")), "developer", "billing-api", "prod")
	code, out := r.start(r.bob, "billing-api", "prod", "migrate")
	if code != 202 && code != 200 && code != 201 {
		t.Fatalf("start prod: %d %+v", code, out)
	}
	r.notify.SendDue(ctx)
	mails := r.mails.all()
	if len(mails) != 1 || !strings.Contains(mails[0].Subject, "waiting for approval") || !strings.Contains(mails[0].Body, "billing-api / prod") {
		t.Fatalf("approval notice: %+v", mails)
	}
	// A failed run is announced with the reason.
	r.gh.SetScenario(fakegithub.Scenario{Conclusion: "failure"})
	_, out = r.start(r.bob, "billing-api", "staging", "migrate")
	r.settle(out.Run.ID, "failed")
	r.notify.SendDue(ctx)
	mails = r.mails.all()
	if len(mails) != 2 || !strings.Contains(mails[1].Subject, "failed on billing-api / staging") {
		t.Fatalf("failure notice: %+v", mails)
	}
	time.Sleep(10 * time.Millisecond)
}

func TestWatcherAnnouncesAChangeOnlyAfterTwoLooks(t *testing.T) {
	r := newRunsEnv(t)
	ctx := context.Background()
	if code := r.alice.json("POST", "/api/admin/notifications/channels", map[string]any{"name": "w", "kind": "webhook", "url": "https://hooks.example.com/w",
		"events": []string{"env.down", "env.recovered", "deploy.failed", "deploy.succeeded"}, "enabled": true}, nil); code != 201 {
		t.Fatalf("create: %d", code)
	}
	srv := r.srv
	probe := func(status string) {
		if _, err := r.st.Pool.Exec(ctx, `INSERT INTO health_checks (app_env_id, status, http_code, latency_ms)
			SELECT id, $1, 0, 5 FROM app_envs WHERE app_id = 'billing-api' AND env = 'staging'`, status); err != nil {
			t.Fatal(err)
		}
	}
	events := func() []string {
		r.notify.SendDue(ctx)
		var out []string
		for _, c := range r.hooks.all() {
			out = append(out, c.Event)
		}
		return out
	}
	probe("ok")
	srv.WatchOnce(ctx, false) // the first look only remembers
	if got := events(); len(got) != 0 {
		t.Fatalf("the first look announces nothing: %v", got)
	}
	probe("fail")
	probe("fail")
	srv.WatchOnce(ctx, false)
	if got := events(); len(got) != 0 {
		t.Fatalf("one look is not enough: %v", got)
	}
	srv.WatchOnce(ctx, false)
	got := events()
	if len(got) != 1 || got[0] != "env.down" {
		t.Fatalf("two looks in a row announce the change: %v", got)
	}
	srv.WatchOnce(ctx, false)
	if len(events()) != 1 {
		t.Fatal("the same state is not announced again")
	}
	probe("ok")
	probe("ok")
	srv.WatchOnce(ctx, false)
	srv.WatchOnce(ctx, false)
	if got := events(); len(got) != 2 || got[1] != "env.recovered" {
		t.Fatalf("recovery: %v", got)
	}

	// Deployments: the first look at a workflow remembers it, a later finished run is announced once.
	now := time.Now()
	r.gh.AddRun("acme-org/billing-api", "deploy.yml", "1111111111111111111111111111111111111111", "bob", "success", now.Add(-2*time.Hour))
	srv.WatchOnce(ctx, true)
	before := len(events())
	r.gh.AddRun("acme-org/billing-api", "deploy.yml", "2222222222222222222222222222222222222222", "bob", "failure", now.Add(-time.Minute))
	srv.WatchOnce(ctx, true)
	srv.WatchOnce(ctx, true)
	got = events()
	if len(got) != before+1 || got[len(got)-1] != "deploy.failed" {
		t.Fatalf("a failed deployment is announced once: %v", got)
	}
}

func TestChannelsCanBeLimitedToAppsAndEnvironmentsAndSeeNewEvents(t *testing.T) {
	r := newRunsEnv(t)
	ctx := context.Background()
	mk := func(name string, extra map[string]any) {
		body := map[string]any{"name": name, "kind": "webhook", "url": "https://hooks.example.com/" + name, "enabled": true,
			"events": []string{"env.down", "env.degraded", "workflow.failed", "deploy.started", "agent.offline", "run.started"}}
		for k, v := range extra {
			body[k] = v
		}
		if code := r.alice.json("POST", "/api/admin/notifications/channels", body, nil); code != 201 {
			t.Fatalf("create %s: %d", name, code)
		}
	}
	mk("billing", map[string]any{"apps": []string{"billing-api"}})
	mk("ledger", map[string]any{"apps": []string{"ledger"}})
	mk("prodonly", map[string]any{"envs": []string{"prod"}})
	if code := r.alice.json("POST", "/api/admin/notifications/channels", map[string]any{"name": "bad", "kind": "webhook", "url": "https://hooks.example.com/b",
		"events": []string{"env.down"}, "apps": []string{"Not A Slug"}}, nil); code != 422 {
		t.Fatalf("an app name that is not a slug is refused: %d", code)
	}
	urls := func() map[string]int {
		r.notify.SendDue(ctx)
		n := map[string]int{}
		for _, c := range r.hooks.all() {
			n[c.URL[strings.LastIndex(c.URL, "/")+1:]]++
		}
		return n
	}

	// A host event reaches the channels of the apps on that host (agent-1 serves billing-api and ledger).
	srv := r.srv
	probe := func(env, status string) {
		if _, err := r.st.Pool.Exec(ctx, `INSERT INTO health_checks (app_env_id, status, http_code, latency_ms)
			SELECT id, $1, 0, 5 FROM app_envs WHERE app_id = 'billing-api' AND env = $2`, status, env); err != nil {
			t.Fatal(err)
		}
	}
	probe("staging", "ok")
	srv.WatchOnce(ctx, true) // first look: remember everything
	probe("staging", "slow")
	srv.WatchOnce(ctx, false)
	srv.WatchOnce(ctx, false)
	got := urls()
	if got["billing"] != 1 || got["ledger"] != 0 || got["prodonly"] != 0 {
		t.Fatalf("a degraded environment of billing-api reaches only the billing channel: %v", got)
	}
	var last notify.Payload
	for _, c := range r.hooks.all() {
		last = c.P
	}
	if last.Title != "billing-api / staging is slow or unhealthy" {
		t.Fatalf("degraded title: %+v", last)
	}

	// Other workflows of the repository: a failing CI run reaches the app's channel, not another app's.
	now := time.Now()
	r.gh.AddRun("acme-org/billing-api", "ci.yml", "1111111111111111111111111111111111111111", "bob", "success", now.Add(-time.Hour))
	srv.WatchOnce(ctx, true)
	r.gh.AddRun("acme-org/billing-api", "ci.yml", "2222222222222222222222222222222222222222", "bob", "failure", now.Add(-time.Minute))
	r.gh.AddRun("acme-org/billing-api", "deploy.yml", "3333333333333333333333333333333333333333", "bob", "failure", now.Add(-time.Minute))
	srv.WatchOnce(ctx, true)
	got = urls()
	if got["billing"] != 2 || got["ledger"] != 0 {
		t.Fatalf("workflow.failed for billing-api's CI run (the deploy workflow is a deploy event instead): %v", got)
	}
	titles := ""
	for _, c := range r.hooks.all() {
		titles += c.P.Title + "|"
	}
	if !strings.Contains(titles, "Workflow failed: ci.yml in acme-org/billing-api") || strings.Contains(titles, "Workflow failed: deploy.yml") {
		t.Fatalf("titles: %s", titles)
	}

	// A deployment that has started is announced once.
	r.gh.AddActiveRun("acme-org/billing-api", "deploy.yml", "4444444444444444444444444444444444444444", "bob")
	srv.WatchOnce(ctx, true)
	srv.WatchOnce(ctx, true)
	got = urls()
	if got["billing"] != 3 {
		t.Fatalf("deploy.started once: %v", got)
	}
}
