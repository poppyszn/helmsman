package api_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/poppyszn/helmsman/internal/api"
	"github.com/poppyszn/helmsman/internal/loki"
)

// fakeLoki answers by looking for a substring of the query, and remembers every query it saw.
type fakeLoki struct {
	mu      sync.Mutex
	queries []string
	limits  []int
	reply   map[string][]loki.Entry
	err     error
}

func (f *fakeLoki) QueryRange(_ context.Context, q string, _, _ time.Time, limit int) ([]loki.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	f.limits = append(f.limits, limit)
	if f.err != nil {
		return nil, f.err
	}
	for sub, es := range f.reply {
		if strings.Contains(q, sub) {
			return es, nil
		}
	}
	return nil, nil
}
func (f *fakeLoki) Ready(context.Context) error { return f.err }
func (f *fakeLoki) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...)
}

type histResp struct {
	Entries []struct {
		Source, Kind, Level, Text, Session, Page, Version string
	} `json:"entries"`
	Sources    struct{ Container, Browser bool } `json:"sources"`
	Truncated  bool                              `json:"truncated"`
	NextBefore *string                           `json:"next_before"`
	Error      string                            `json:"error"`
	Code       string                            `json:"code"`
}

func withLoki(t *testing.T, e *env, f *fakeLoki) {
	t.Helper()
	d := e.deps
	d.Loki = f
	srv, err := api.New(d)
	if err != nil {
		t.Fatal(err)
	}
	e.swap(srv.Handler())
}

const faroCatalogPatch = "        github_environment: staging\n      - env: prod"

func enableFaro(t *testing.T, e *env) {
	t.Helper()
	patched := strings.Replace(catalogYAML, faroCatalogPatch, "        github_environment: staging\n        logs: { faro_app: billing-web }\n      - env: prod", 1)
	if patched == catalogYAML {
		t.Fatal("catalog patch did not apply")
	}
	e.writeCatalog(patched)
	if err := e.cat.Reload(context.Background(), "test", true); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryIsOffUntilLokiIsConfigured(t *testing.T) {
	e := newEnv(t)
	alice := e.client()
	alice.login("alice")
	var out histResp
	if code := alice.json("GET", "/api/apps/billing-api/envs/staging/logs/history", nil, &out); code != 503 || out.Code != "history_disabled" {
		t.Fatalf("%d %+v", code, out)
	}
}

func TestHistoryNeedsLogsViewOnThatEnvironment(t *testing.T) {
	e := newEnv(t)
	withLoki(t, e, &fakeLoki{})
	carol, bob := e.client(), e.client()
	carol.login("carol")
	bob.login("bob")
	e.bind("user", itoa(e.userID("carol")), "viewer", "billing-api", "staging")
	e.bind("user", itoa(e.userID("bob")), "developer", "billing-api", "staging")
	if code := carol.json("GET", "/api/apps/billing-api/envs/staging/logs/history", nil, nil); code != 403 {
		t.Fatalf("a viewer has no logs.view: %d", code)
	}
	if code := bob.json("GET", "/api/apps/billing-api/envs/staging/logs/history", nil, nil); code != 200 {
		t.Fatalf("developer: %d", code)
	}
	if code := bob.json("GET", "/api/apps/billing-api/envs/prod/logs/history", nil, nil); code != 403 {
		t.Fatalf("a staging only developer must not read prod history: %d", code)
	}
}

func TestHistoryMergesContainerAndBrowserLogsNewestFirst(t *testing.T) {
	e := newEnv(t)
	enableFaro(t, e)
	now := time.Now().UTC()
	f := &fakeLoki{reply: map[string][]loki.Entry{
		`source="container"`: {
			{TS: now.Add(-3 * time.Second), Line: `10.0.0.1 - - [x] "GET / HTTP/1.1" 200 5 "-" "curl"`, Labels: map[string]string{"kind": "access", "level": "info"}},
			{TS: now.Add(-1 * time.Second), Line: `2026/11/11 [error] 1#1: open() failed`, Labels: map[string]string{"kind": "error", "level": "error"}},
		},
		`source="browser"`: {
			{TS: now.Add(-2 * time.Second), Line: `kind=exception type=TypeError value="boom" session_id=s1 page_url=http://x/pay app_version=1.2`, Labels: map[string]string{"kind": "exception", "level": "error"}},
		},
	}}
	withLoki(t, e, f)
	alice := e.client()
	alice.login("alice")
	var out histResp
	if code := alice.json("GET", "/api/apps/billing-api/envs/staging/logs/history", nil, &out); code != 200 {
		t.Fatalf("%d %+v", code, out)
	}
	if len(out.Entries) != 3 || out.Entries[0].Kind != "error" || out.Entries[1].Source != "browser" || out.Entries[2].Kind != "access" {
		t.Fatalf("order or content wrong: %+v", out.Entries)
	}
	if got := out.Entries[1]; got.Text != "TypeError: boom" || got.Session != "s1" || got.Page != "http://x/pay" || got.Version != "1.2" {
		t.Fatalf("browser line must be rendered: %+v", got)
	}
	if !out.Sources.Browser || !out.Sources.Container {
		t.Fatalf("sources: %+v", out.Sources)
	}
	qs := strings.Join(f.seen(), "\n")
	// The agent id is the host label, so staging and prod on different hosts cannot mix.
	if !strings.Contains(qs, `host="agent-1"`) || !strings.Contains(qs, `compose_project="billing"`) || !strings.Contains(qs, `compose_service="api"`) ||
		!strings.Contains(qs, `app="billing-web"`) || !strings.Contains(qs, `env="staging"`) {
		t.Fatalf("selectors: %s", qs)
	}
}

func TestHistoryFiltersPickTheRightSources(t *testing.T) {
	e := newEnv(t)
	enableFaro(t, e)
	f := &fakeLoki{}
	withLoki(t, e, f)
	alice := e.client()
	alice.login("alice")
	get := func(query string) []string {
		before := len(f.seen())
		if code := alice.json("GET", "/api/apps/billing-api/envs/staging/logs/history?"+query, nil, nil); code != 200 {
			t.Fatalf("%s: %d", query, code)
		}
		return f.seen()[before:]
	}
	if q := get("kind=exception,event"); len(q) != 1 || !strings.Contains(q[0], `source="browser"`) || !strings.Contains(q[0], `kind=~"exception|event"`) {
		t.Fatalf("browser only kinds must query only the browser: %v", q)
	}
	if q := get("kind=access"); len(q) != 1 || !strings.Contains(q[0], `source="container"`) {
		t.Fatalf("access is a container kind: %v", q)
	}
	if q := get("source=container&level=error,warn"); len(q) != 1 || !strings.Contains(q[0], `level=~"error|warn"`) {
		t.Fatalf("%v", q)
	}
	if q := get("source=browser"); len(q) != 1 || !strings.Contains(q[0], `source="browser"`) {
		t.Fatalf("%v", q)
	}
	if q := get("kind=app"); len(q) != 2 {
		t.Fatalf("app lines exist in both sources: %v", q)
	}
	if q := get(`q=a.b"|c`); !strings.Contains(q[0], ` |~ "(?i)a\\.b\"\\|c"`) {
		t.Fatalf("free text must be an escaped literal: %v", q)
	}
}

func TestHistoryRejectsValuesOutsideTheFixedLists(t *testing.T) {
	e := newEnv(t)
	f := &fakeLoki{}
	withLoki(t, e, f)
	alice := e.client()
	alice.login("alice")
	for _, q := range []string{"kind=nope", `kind=app"}|~"x`, "level=fatal", "source=disk", "range=30d", "limit=0", "limit=abc", "before=yesterday", "q=" + strings.Repeat("a", 201)} {
		if code := alice.json("GET", "/api/apps/billing-api/envs/staging/logs/history?"+q, nil, nil); code != 400 {
			t.Errorf("%q must be 400, got %d", q, code)
		}
	}
	if len(f.seen()) != 0 {
		t.Fatalf("nothing invalid may reach Loki: %v", f.seen())
	}
}

func TestHistoryPagesBackwardsAndCapsTheLimit(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	var many []loki.Entry
	for i := 0; i < 6; i++ {
		many = append(many, loki.Entry{TS: now.Add(-time.Duration(i) * time.Second), Line: "line", Labels: map[string]string{"kind": "app", "level": "info"}})
	}
	f := &fakeLoki{reply: map[string][]loki.Entry{`source="container"`: many}}
	withLoki(t, e, f)
	alice := e.client()
	alice.login("alice")
	var out histResp
	alice.json("GET", "/api/apps/billing-api/envs/staging/logs/history?limit=5", nil, &out)
	if len(out.Entries) != 5 || !out.Truncated || out.NextBefore == nil {
		t.Fatalf("expected a page of 5 with a cursor: %+v", out)
	}
	alice.json("GET", "/api/apps/billing-api/envs/staging/logs/history?limit=99999", nil, &out)
	if f.limits[len(f.limits)-1] != 1001 {
		t.Fatalf("the limit must be capped at 1000 (+1 to detect more): %v", f.limits)
	}
}

func TestHistoryIsAuditedWithoutTheSearchText(t *testing.T) {
	e := newEnv(t)
	withLoki(t, e, &fakeLoki{})
	alice := e.client()
	alice.login("alice")
	alice.json("GET", "/api/apps/billing-api/envs/staging/logs/history?q=card-4111-secret&kind=error", nil, nil)
	var d string
	if err := e.st.Pool.QueryRow(context.Background(), `SELECT details::text FROM audit_events WHERE action='logs.history' ORDER BY id DESC LIMIT 1`).Scan(&d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d, "card-4111-secret") || !strings.Contains(d, `"text_filtered": true`) || !strings.Contains(d, "error") {
		t.Fatalf("audit details: %s", d)
	}
}

func TestHistoryHidesLokisOwnErrors(t *testing.T) {
	e := newEnv(t)
	withLoki(t, e, &fakeLoki{err: context.DeadlineExceeded})
	alice := e.client()
	alice.login("alice")
	var out histResp
	if code := alice.json("GET", "/api/apps/billing-api/envs/staging/logs/history", nil, &out); code != 502 || strings.Contains(out.Error, "deadline") {
		t.Fatalf("%d %+v", code, out)
	}
}
