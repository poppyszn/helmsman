package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type doctorResp struct {
	Checks []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Detail string `json:"detail"`
		Fix    string `json:"fix"`
	} `json:"checks"`
	Counts map[string]int `json:"counts"`
}

func (d doctorResp) check(id string) (status, detail, fix string, ok bool) {
	for _, c := range d.Checks {
		if c.ID == id {
			return c.Status, c.Detail, c.Fix, true
		}
	}
	return "", "", "", false
}

func TestSetupCheckFindsWhatIsWrong(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.client(), e.client()
	alice.login("alice")
	bob.login("bob")
	e.bind("user", itoa(e.userID("bob")), "developer", "*", "*")

	if code := bob.json("GET", "/api/admin/doctor", nil, nil); code != 403 {
		t.Fatalf("the setup check is admin only: %d", code)
	}
	var d doctorResp
	if code := alice.json("GET", "/api/admin/doctor", nil, &d); code != 200 || len(d.Checks) < 8 {
		t.Fatalf("doctor: %d %+v", code, d)
	}
	for id, want := range map[string]string{
		"host-match":    "ok",   // the test server's address is IDP_BASE_URL
		"db-role":       "ok",   // the server runs as the limited role
		"audit":         "ok",   // the audit triggers exist
		"catalog":       "ok",   // the catalog loaded
		"oauth":         "warn", // fake GitHub mode on a "deployed" server
		"agent-agent-1": "fail", // registered but never joined
	} {
		if got, detail, _, ok := d.check(id); !ok || got != want {
			t.Errorf("%s: want %s, got %q (%s) present=%v", id, want, got, detail, ok)
		}
	}
	// An agent that is online but does not see the catalog's container names the likely cause.
	if st, detail, fix, ok := d.check("env-billing-api-staging"); ok && st == "warn" {
		if !strings.Contains(fix, "compose labels") || !strings.Contains(detail, "billing") {
			t.Errorf("container_missing advice: %s / %s", detail, fix)
		}
	}
}

func TestSetupCheckSpotsAGitHubAppUsedForSignIn(t *testing.T) {
	e := newEnvWith(t, map[string]string{"IDP_GITHUB_MODE": "real", "GITHUB_OAUTH_CLIENT_ID": "Iv1.abcdef0123456789"})
	alice := e.client()
	alice.login("alice")
	var d doctorResp
	if code := alice.json("GET", "/api/admin/doctor", nil, &d); code != 200 {
		t.Fatalf("doctor: %d", code)
	}
	st, detail, fix, ok := d.check("oauth")
	if !ok || st != "fail" || !strings.Contains(detail, "GitHub App") || !strings.Contains(fix, "OAuth App") {
		t.Fatalf("a GitHub App client id must be flagged: %q %q %q", st, detail, fix)
	}
}

func TestSetupCheckWebSocketEndpoint(t *testing.T) {
	e := newEnv(t)
	alice := e.client()
	alice.login("alice")
	u, _ := http.NewRequest("GET", e.idp.URL, nil)
	var cookies []string
	for _, ck := range alice.http.Jar.Cookies(u.URL) {
		cookies = append(cookies, ck.Name+"="+ck.Value)
	}
	dial := func(origin string, withCookie bool) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hdr := http.Header{}
		if withCookie {
			hdr.Set("Cookie", strings.Join(cookies, "; "))
		}
		if origin != "" {
			hdr.Set("Origin", origin)
		}
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.idp.URL, "http")+"/ws/doctor", &websocket.DialOptions{HTTPHeader: hdr})
		if err != nil {
			return err
		}
		defer c.CloseNow()
		_, msg, err := c.Read(ctx)
		if err != nil || string(msg) != "ok" {
			t.Fatalf("expected ok, got %q %v", msg, err)
		}
		return nil
	}
	if err := dial(e.idp.URL, true); err != nil {
		t.Fatalf("same origin works: %v", err)
	}
	if err := dial("https://evil.example", true); err == nil {
		t.Fatal("another site must not open it")
	}
	if err := dial("", false); err == nil {
		t.Fatal("it needs a session")
	}
}
