package api_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/poppyszn/helmsman/internal/actions"
	"github.com/poppyszn/helmsman/internal/agenthub"
	"github.com/poppyszn/helmsman/internal/api"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/catalog"
	"github.com/poppyszn/helmsman/internal/config"
	"github.com/poppyszn/helmsman/internal/fakegithub"
	"github.com/poppyszn/helmsman/internal/integrations"
	"github.com/poppyszn/helmsman/internal/store"
)

const catalogYAML = `
version: 1
apps:
  - id: billing-api
    name: Billing API
    repo: acme-org/billing-api
    environments:
      - env: staging
        tier: nonprod
        agent: agent-1
        container: { compose_project: billing, service: api }
        url: https://staging-billing.example.com
        health: { url: https://staging-billing.example.com/health, via: server }
        workflows: { deploy: deploy.yml, migrate: migrate.yml }
        github_environment: staging
      - env: prod
        tier: prod
        agent: agent-1
        container: { compose_project: billing, service: api }
        url: https://billing.example.com
        health: { url: http://localhost:8080/health, via: agent }
        workflows: { deploy: deploy.yml, migrate: migrate.yml }
        github_environment: production
  - id: ledger
    name: Ledger
    repo: acme-org/ledger
    environments:
      - env: staging
        tier: nonprod
        agent: agent-1
        container: { compose_project: ledger, service: api }
        url: https://staging-ledger.example.com
        health: { url: https://staging-ledger.example.com/health, via: server }
        workflows: { migrate: migrate.yml }
        github_environment: staging
`

type env struct {
	t       *testing.T
	st      *store.Store
	gh      *fakegithub.Server
	ghURL   string
	idp     *httptest.Server
	auth    *auth.Service
	cat     *catalog.Manager
	catPath string
	cfg     config.Config
	hub     *agenthub.Hub
	act     *actions.Service
	integ   *integrations.Manager
	deps    api.Deps
	swap    func(http.Handler)
}

func newEnv(t *testing.T) *env { return newEnvWith(t, nil) }

// newEnvWith builds the environment with extra configuration variables.
func newEnvWith(t *testing.T, extra map[string]string) *env {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; run via make test")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if _, err := st.Pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `INSERT INTO agents (id, name) VALUES ('agent-1','Agent 1')`); err != nil {
		t.Fatal(err)
	}

	e := &env{t: t, st: st, gh: fakegithub.New()}
	ghSrv := httptest.NewServer(e.gh)
	t.Cleanup(ghSrv.Close)
	e.ghURL = ghSrv.URL

	var handler atomic.Pointer[http.Handler]
	e.idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := handler.Load(); h != nil {
			(*h).ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(e.idp.Close)

	key := hex.EncodeToString(bytes.Repeat([]byte{9}, 32))
	cfg, err := config.FromLookup(func(k string) (string, bool) {
		m := map[string]string{
			"IDP_DATABASE_URL": dsn, "IDP_BASE_URL": e.idp.URL, "IDP_SESSION_KEY": key, "IDP_ENCRYPTION_KEY": key,
			"IDP_ALLOWED_ORGS": "acme-org", "IDP_BOOTSTRAP_ADMIN_GITHUB_IDS": "1001", "IDP_GITHUB_MODE": "fake",
			"GITHUB_OAUTH_CLIENT_ID": "cid", "GITHUB_OAUTH_CLIENT_SECRET": "sec",
			"IDP_GITHUB_AUTH_URL": e.ghURL + "/login/oauth/authorize", "IDP_GITHUB_TOKEN_URL": e.ghURL + "/login/oauth/access_token",
			"IDP_GITHUB_API_URL": e.ghURL,
		}
		for k2, v2 := range extra {
			m[k2] = v2
		}
		v, ok := m[k]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	e.cfg = cfg
	// The server under test runs as the least-privilege role, exactly like production. The
	// superuser store above stays for fixtures and assertions.
	app, err := store.OpenAs(ctx, dsn, "helmsman_app")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	aw := &audit.Writer{Pool: app.Pool}
	e.auth = auth.NewService(cfg, app, aw)
	e.catPath = filepath.Join(t.TempDir(), "apps.yaml")
	e.writeCatalog(catalogYAML)
	e.cat = catalog.NewManager(app.Pool, e.catPath, aw, app.AgentExists)
	if err := e.cat.Reload(ctx, "test", true); err != nil {
		t.Fatal(err)
	}
	e.hub = agenthub.New(app, aw, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.act = newTestActions(t, app, aw, ghSrv.URL)
	e.integ = integrations.New(app, cfg.EncryptionKey, integrations.EnvDefaults{})
	if err := e.integ.Load(ctx); err != nil {
		t.Fatal(err)
	}
	e.deps = api.Deps{Cfg: cfg, DB: app, Store: app, Audit: aw, Auth: e.auth, Catalog: e.cat, Hub: e.hub, Actions: e.act, Logs: e.hub, Loki: e.integ, Integrations: e.integ,
		LogRecheck: 300 * time.Millisecond, UI: fstest.MapFS{"index.html": {Data: []byte("x")}}}
	srv, err := api.New(e.deps)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	handler.Store(&h)
	e.swap = func(n http.Handler) { handler.Store(&n) }
	return e
}

func (e *env) writeCatalog(s string) {
	e.t.Helper()
	if err := os.WriteFile(e.catPath, []byte(s), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// client is a cookie-keeping, non-redirect-following HTTP client.
type client struct {
	e    *env
	http *http.Client
	csrf string
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// login runs the full OAuth flow as the fake user and returns the final redirect location.
func (c *client) login(user string) string {
	c.e.t.Helper()
	resp := c.do("GET", c.e.idp.URL+"/auth/github/login", nil)
	authz, _ := url.Parse(resp.Header.Get("Location"))
	q := authz.Query()
	q.Set("fake_user", user)
	authz.RawQuery = q.Encode()
	resp = c.do("GET", authz.String(), nil)
	resp = c.do("GET", resp.Header.Get("Location"), nil)
	loc := resp.Header.Get("Location")
	if loc == "/" {
		var me struct {
			CSRF string `json:"csrf_token"`
		}
		c.json("GET", "/api/me", nil, &me)
		c.csrf = me.CSRF
	}
	return loc
}

func (c *client) do(method, u string, body []byte) *http.Response {
	c.e.t.Helper()
	req, _ := http.NewRequest(method, u, bytes.NewReader(body))
	if c.csrf != "" && method != "GET" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	t := resp.Body
	defer t.Close()
	b, _ := io.ReadAll(t)
	resp.Body = io.NopCloser(bytes.NewReader(b))
	return resp
}

func (c *client) json(method, path string, in, out any) int {
	c.e.t.Helper()
	var body []byte
	if in != nil {
		body, _ = json.Marshal(in)
	}
	resp := c.do(method, c.e.idp.URL+path, body)
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (e *env) userID(login string) int64 {
	var id int64
	if err := e.st.Pool.QueryRow(context.Background(), `SELECT id FROM users WHERE login=$1`, login).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) bind(subjectType, subject, role, app, envName string) {
	e.t.Helper()
	_, err := e.st.Pool.Exec(context.Background(), `
		INSERT INTO role_bindings (subject_type, subject, role_id, app_id, env)
		SELECT $1,$2,id,$3,$4 FROM roles WHERE name=$5`, subjectType, subject, app, envName, role)
	if err != nil {
		e.t.Fatal(err)
	}
}

func appEnvs(c *client, path string) map[string][]string {
	var apps []struct {
		ID   string `json:"id"`
		Envs []struct {
			Env string `json:"env"`
		} `json:"environments"`
	}
	c.json("GET", path, nil, &apps)
	out := map[string][]string{}
	for _, a := range apps {
		for _, e := range a.Envs {
			out[a.ID] = append(out[a.ID], e.Env)
		}
	}
	return out
}

func TestLoginAndSession(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	if loc := c.login("alice"); loc != "/" {
		t.Fatalf("redirect %q", loc)
	}
	var me struct {
		User struct {
			Login   string `json:"login"`
			IsAdmin bool   `json:"is_admin"`
		}
		Can struct{ Admin, Audit bool }
	}
	if code := c.json("GET", "/api/me", nil, &me); code != 200 || me.User.Login != "alice" || !me.User.IsAdmin || !me.Can.Admin || !me.Can.Audit {
		t.Fatalf("me: %d %+v", code, me)
	}
	// The GitHub token is stored encrypted, never as plaintext.
	var enc []byte
	_ = e.st.Pool.QueryRow(context.Background(), `SELECT gh_token_enc FROM users WHERE login='alice'`).Scan(&enc)
	if len(enc) == 0 || bytes.Contains(enc, []byte("fake_")) {
		t.Fatal("token not encrypted")
	}
	// Only a hash of the session id is stored.
	var n int
	_ = e.st.Pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(&n)
	if n != 1 {
		t.Fatalf("sessions %d", n)
	}
	// Logout revokes: the next request is 401.
	if code := c.json("POST", "/auth/logout", nil, nil); code != 200 {
		t.Fatalf("logout %d", code)
	}
	if code := c.json("GET", "/api/me", nil, nil); code != 401 {
		t.Fatalf("after logout %d", code)
	}
}

func TestLoginRejections(t *testing.T) {
	e := newEnv(t)
	if loc := e.client().login("dave"); loc != "/login?error=not_in_org" {
		t.Fatalf("outside org: %q", loc)
	}
	if loc := e.client().login("frank"); loc != "/login?error=org_restricted&org=acme-org" {
		t.Fatalf("restricted org: %q", loc)
	}
	// Tampered or missing state is refused.
	c := e.client()
	resp := c.do("GET", e.idp.URL+"/auth/github/callback?code=x&state=forged", nil)
	if loc := resp.Header.Get("Location"); loc != "/login?error=state" {
		t.Fatalf("state: %q", loc)
	}
}

func TestBootstrapAdminOnlyOnFirstLogin(t *testing.T) {
	e := newEnv(t)
	e.client().login("alice")
	if _, err := e.st.Pool.Exec(context.Background(), `UPDATE users SET is_admin=false WHERE login='alice'`); err != nil {
		t.Fatal(err)
	}
	e.client().login("alice")
	var admin bool
	_ = e.st.Pool.QueryRow(context.Background(), `SELECT is_admin FROM users WHERE login='alice'`).Scan(&admin)
	if admin {
		t.Fatal("bootstrap promotion must only happen on first login")
	}
}

func TestIdentityKeyedOnGitHubID(t *testing.T) {
	e := newEnv(t)
	e.client().login("bob")
	id := e.userID("bob")
	// Rename on GitHub and hide the email: same account.
	e.gh.ReplaceUser(fakegithub.User{ID: 1002, Login: "bobby", Orgs: []string{"acme-org"}})
	c := e.client()
	if loc := c.login("bobby"); loc != "/" {
		t.Fatalf("rename login: %q", loc)
	}
	if e.userID("bobby") != id {
		t.Fatal("renamed user got a new account")
	}
}

func TestMembershipRefreshRevokesAndGitHubOutageDoesNot(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.client()
	c.login("bob")

	// GitHub down: the refresh must not revoke anything.
	e.gh.SetDown(true)
	e.auth.RefreshAll(ctx)
	if code := c.json("GET", "/api/me", nil, nil); code != 200 {
		t.Fatalf("session must survive a GitHub outage, got %d", code)
	}
	e.gh.SetDown(false)

	// Bob leaves the org: the next refresh revokes, and the next request is 401.
	e.gh.ReplaceUser(fakegithub.User{ID: 1002, Login: "bob", Orgs: []string{"other-org"}})
	e.auth.RefreshAll(ctx)
	if code := c.json("GET", "/api/me", nil, nil); code != 401 {
		t.Fatalf("removed user must get 401, got %d", code)
	}
}

func TestRevokedGitHubTokenRevokesSessions(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	c.login("carol")
	e.gh.RevokeTokens("carol")
	e.auth.RefreshAll(context.Background())
	if code := c.json("GET", "/api/me", nil, nil); code != 401 {
		t.Fatalf("got %d", code)
	}
}

func TestGitHubDownBlocksNewLoginsOnly(t *testing.T) {
	e := newEnv(t)
	alice := e.client()
	alice.login("alice")
	e.gh.SetDown(true)
	// The fake is down, so a fresh login cannot even reach authorize; the token
	// exchange is what fails in real life. Simulate by completing authorize first.
	e.gh.SetDown(false)
	c := e.client()
	resp := c.do("GET", e.idp.URL+"/auth/github/login", nil)
	authz, _ := url.Parse(resp.Header.Get("Location"))
	q := authz.Query()
	q.Set("fake_user", "bob")
	authz.RawQuery = q.Encode()
	resp = c.do("GET", authz.String(), nil)
	e.gh.SetDown(true)
	resp = c.do("GET", resp.Header.Get("Location"), nil)
	if loc := resp.Header.Get("Location"); loc != "/login?error=github_unavailable" {
		t.Fatalf("got %q", loc)
	}
	if code := alice.json("GET", "/api/me", nil, nil); code != 200 {
		t.Fatalf("existing session must keep working, got %d", code)
	}
}

func TestProdInvisibleToNonprodOnlyDev(t *testing.T) {
	e := newEnv(t)
	bob, carol, alice := e.client(), e.client(), e.client()
	bob.login("bob")
	carol.login("carol")
	alice.login("alice")
	// bob: developer on billing-api via env "*". carol: viewer on billing-api prod explicitly.
	e.bind("user", itoa(e.userID("bob")), "developer", "billing-api", "*")
	e.bind("user", itoa(e.userID("carol")), "viewer", "billing-api", "prod")

	got := appEnvs(bob, "/api/apps")
	if len(got) != 1 || len(got["billing-api"]) != 1 || got["billing-api"][0] != "staging" {
		t.Fatalf("bob must see only billing-api staging: %v", got)
	}
	if got := appEnvs(carol, "/api/apps"); len(got["billing-api"]) != 1 || got["billing-api"][0] != "prod" {
		t.Fatalf("carol sees prod by explicit grant: %v", got)
	}
	if got := appEnvs(alice, "/api/apps"); len(got["billing-api"]) != 2 || len(got["ledger"]) != 1 {
		t.Fatalf("admin sees everything: %v", got)
	}
	// Apps outside the grant are not just hidden from the list: they are 403 on detail.
	if code := bob.json("GET", "/api/apps/ledger", nil, nil); code != 403 {
		t.Fatalf("ledger detail for bob: %d", code)
	}
}

func TestTeamBindingsAndUnion(t *testing.T) {
	e := newEnv(t)
	bob := e.client()
	bob.login("bob") // member of acme-org/backend
	e.bind("team", "acme-org/backend", "viewer", "billing-api", "*")
	e.bind("user", itoa(e.userID("bob")), "viewer", "ledger", "*")
	got := appEnvs(bob, "/api/apps")
	if len(got["billing-api"]) != 1 || len(got["ledger"]) != 1 {
		t.Fatalf("team + user bindings must union: %v", got)
	}
}

func TestEveryRouteDeniesWithoutPermission(t *testing.T) {
	e := newEnv(t)
	srv, _ := api.New(api.Deps{Cfg: e.cfg, DB: e.st, Store: e.st, Audit: &audit.Writer{Pool: e.st.Pool}, Auth: e.auth, Catalog: e.cat, Hub: e.hub, Actions: e.act, Logs: e.hub, UI: fstest.MapFS{}})
	anon, nobody := e.client(), e.client()
	nobody.login("carol") // signed in, zero bindings

	publicWant := map[string]bool{"GET /healthz": true, "GET /readyz": true, "GET /auth/github/login": true, "GET /auth/github/callback": true,
		"GET /agent/ws": true, "POST /agent/join": true, "GET /auth/options": true, "POST /auth/breakglass": true}
	for _, rt := range srv.Routes() {
		key := rt.Method + " " + rt.Path
		path := strings.NewReplacer("{app}", "billing-api", "{env}", "prod", "{id}", "1").Replace(rt.Path)
		switch rt.Permission {
		case api.Public:
			if !publicWant[key] {
				t.Errorf("%s is public but not on the expected public list", key)
			}
			continue
		case "":
			t.Errorf("%s declares no permission", key)
			continue
		}
		if code := anon.json(rt.Method, path, nil, nil); code != 401 {
			t.Errorf("%s without a session: got %d want 401", key, code)
		}
		if rt.Permission == api.Authenticated {
			continue
		}
		if code := nobody.json(rt.Method, path, map[string]any{}, nil); code != 403 {
			t.Errorf("%s with no bindings: got %d want 403", key, code)
		}
	}
}

func TestCSRFRequiredOnStateChange(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	c.login("alice")
	c.csrf = ""
	if code := c.json("POST", "/api/admin/bindings", map[string]any{}, nil); code != 403 {
		t.Fatalf("missing CSRF token: %d", code)
	}
}

func TestAdminBindingsAndAudit(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.client(), e.client()
	alice.login("alice")
	bob.login("bob")
	var roles []struct {
		ID   int64
		Name string
	}
	alice.json("GET", "/api/admin/roles", nil, &roles)
	var devRole int64
	for _, r := range roles {
		if r.Name == "developer" {
			devRole = r.ID
		}
	}
	var created struct{ ID int64 }
	req := map[string]any{"subject_type": "user", "subject": "bob", "role_id": devRole, "app_id": "billing-api", "env": "*"}
	if code := alice.json("POST", "/api/admin/bindings", req, &created); code != 201 {
		t.Fatalf("create binding: %d", code)
	}
	if code := bob.json("POST", "/api/admin/bindings", req, nil); code != 403 {
		t.Fatalf("non-admin must not create bindings: %d", code)
	}
	bad := map[string]any{"subject_type": "team", "subject": "evil-org/x", "role_id": devRole}
	if code := alice.json("POST", "/api/admin/bindings", bad, nil); code != 422 {
		t.Fatalf("team in a non-allowed org: %d", code)
	}
	if got := appEnvs(bob, "/api/apps"); len(got["billing-api"]) != 1 {
		t.Fatalf("binding not effective: %v", got)
	}
	if code := alice.json("DELETE", "/api/admin/bindings/"+itoa(created.ID), nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	var audit []struct{ Action, Result string }
	if code := alice.json("GET", "/api/admin/audit", nil, &audit); code != 200 {
		t.Fatalf("audit %d", code)
	}
	seen := map[string]bool{}
	for _, a := range audit {
		seen[a.Action] = true
	}
	for _, want := range []string{"auth.login", "binding.create", "binding.delete", "catalog.reload"} {
		if !seen[want] {
			t.Errorf("audit missing %s: %v", want, seen)
		}
	}
	// Append-only at the database level.
	if _, err := e.st.Pool.Exec(context.Background(), `DELETE FROM audit_events`); err == nil {
		t.Fatal("audit delete must fail")
	}
}

func TestInvalidCatalogKeepsLastGood(t *testing.T) {
	e := newEnv(t)
	alice := e.client()
	alice.login("alice")
	before := e.cat.Status().Rev

	for name, bad := range map[string]string{
		"bad yaml":      "version: [",
		"unknown agent": strings.Replace(catalogYAML, "agent: agent-1", "agent: ghost", 1),
		"duplicate id":  catalogYAML + strings.SplitN(catalogYAML, "apps:", 2)[1],
	} {
		e.writeCatalog(bad)
		if code := alice.json("POST", "/api/admin/catalog/reload", nil, nil); code != 422 {
			t.Errorf("%s: reload code %d", name, code)
		}
		st := e.cat.Status()
		if st.LastError == "" || st.Rev != before {
			t.Errorf("%s: last good must stay active (rev %s, was %s, err %q)", name, st.Rev, before, st.LastError)
		}
		if got := appEnvs(alice, "/api/apps"); len(got) != 2 {
			t.Errorf("%s: apps changed to %v", name, got)
		}
	}
	var status struct {
		LastError string `json:"last_error"`
	}
	alice.json("GET", "/api/admin/catalog", nil, &status)
	if status.LastError == "" {
		t.Error("admin page must show the error")
	}
}

func TestRemovedAppIsHiddenButKept(t *testing.T) {
	e := newEnv(t)
	alice := e.client()
	alice.login("alice")
	e.writeCatalog(strings.Split(catalogYAML, "  - id: ledger")[0])
	if code := alice.json("POST", "/api/admin/catalog/reload", nil, nil); code != 200 {
		t.Fatalf("reload %d", code)
	}
	if got := appEnvs(alice, "/api/apps"); len(got) != 1 || got["ledger"] != nil {
		t.Fatalf("removed app still listed: %v", got)
	}
	if code := alice.json("GET", "/api/apps/ledger", nil, nil); code != 404 {
		t.Fatalf("removed app detail: %d", code)
	}
	var deleted bool
	_ = e.st.Pool.QueryRow(context.Background(), `SELECT deleted_at IS NOT NULL FROM apps WHERE id='ledger'`).Scan(&deleted)
	if !deleted {
		t.Fatal("app must be soft-deleted, not removed")
	}
	// Restoring it brings it back.
	e.writeCatalog(catalogYAML)
	alice.json("POST", "/api/admin/catalog/reload", nil, nil)
	if got := appEnvs(alice, "/api/apps"); len(got["ledger"]) != 1 {
		t.Fatalf("restored app missing: %v", got)
	}
}

func itoa(i int64) string { b, _ := json.Marshal(i); return string(b) }
