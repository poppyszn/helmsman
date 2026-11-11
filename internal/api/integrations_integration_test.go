package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeLokiServer answers like Loki: /ready, /loki/api/v1/labels and /loki/api/v1/query_range. With
// user set it demands basic auth, the way Caddy does in front of a shared Loki.
func fakeLokiServer(t *testing.T, user, pass string, labels []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != pass {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		switch r.URL.Path {
		case "/ready":
			_, _ = w.Write([]byte("ready"))
		case "/loki/api/v1/labels":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": labels})
		case "/loki/api/v1/query_range":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []any{}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

var helmsmanLabels = []string{"source", "kind", "level", "compose_service", "compose_project", "host"}

type lokiStatusResp struct {
	Loki struct {
		Connected   bool   `json:"connected"`
		Enabled     bool   `json:"enabled"`
		URL         string `json:"url"`
		User        string `json:"user"`
		HasPassword bool   `json:"has_password"`
		Source      string `json:"source"`
		UpdatedBy   string `json:"updated_by"`
	} `json:"loki"`
	Test struct {
		Reachable bool     `json:"reachable"`
		HasLogs   bool     `json:"has_logs"`
		Missing   []string `json:"missing_labels"`
		Error     string   `json:"error"`
	} `json:"test"`
	Error string `json:"error"`
}

type testResp struct {
	Reachable bool     `json:"reachable"`
	HasLogs   bool     `json:"has_logs"`
	Missing   []string `json:"missing_labels"`
	Error     string   `json:"error"`
}

func histCode(c *client) int {
	return c.json("GET", "/api/apps/billing-api/envs/staging/logs/history", nil, nil)
}

func TestHistoryIsConnectedFromTheIntegrationsPage(t *testing.T) {
	e := newEnv(t)
	loki := fakeLokiServer(t, "", "", helmsmanLabels)
	alice, bob := e.client(), e.client()
	alice.login("alice")
	bob.login("bob")
	e.bind("user", itoa(e.userID("bob")), "developer", "billing-api", "*")

	var st lokiStatusResp
	if alice.json("GET", "/api/admin/integrations", nil, &st); st.Loki.Source != "none" || st.Loki.Connected {
		t.Fatalf("nothing is connected out of the box: %+v", st.Loki)
	}
	if code := histCode(alice); code != 503 {
		t.Fatalf("history is off until Loki is connected: %d", code)
	}

	// Test first: reachable, and Helmsman's labels are already there.
	var tr testResp
	if code := alice.json("POST", "/api/admin/integrations/loki/test", map[string]any{"url": loki.URL}, &tr); code != 200 {
		t.Fatalf("test: %d", code)
	}
	if !tr.Reachable || !tr.HasLogs {
		t.Fatalf("expected reachable with logs: %+v", tr)
	}
	if code := histCode(alice); code != 503 {
		t.Fatal("testing a connection must not turn it on")
	}

	// Save and enable: history works at once, for everyone with logs.view, without a restart.
	if code := alice.json("PUT", "/api/admin/integrations/loki", map[string]any{"url": loki.URL + "/", "enabled": true}, &st); code != 200 {
		t.Fatalf("save: %d %+v", code, st)
	}
	if !st.Loki.Connected || st.Loki.URL != loki.URL || st.Loki.Source != "database" || st.Loki.UpdatedBy != "alice" {
		t.Fatalf("status after save: %+v", st.Loki)
	}
	if code := histCode(alice); code != 200 {
		t.Fatalf("history after connecting: %d", code)
	}
	if code := histCode(bob); code != 200 {
		t.Fatalf("a developer reads history too: %d", code)
	}

	// Turn it off again: history stops, settings are kept.
	alice.json("PUT", "/api/admin/integrations/loki", map[string]any{"url": loki.URL, "enabled": false}, &st)
	if st.Loki.Connected || st.Loki.URL != loki.URL {
		t.Fatalf("disabled but remembered: %+v", st.Loki)
	}
	if code := histCode(alice); code != 503 {
		t.Fatalf("history after disabling: %d", code)
	}

	// Remove forgets everything.
	alice.json("DELETE", "/api/admin/integrations/loki", nil, &st)
	if st.Loki.URL != "" || st.Loki.Source != "none" {
		t.Fatalf("removed: %+v", st.Loki)
	}
}

func TestLokiPasswordIsEncryptedNeverReturnedAndKeptWhenOmitted(t *testing.T) {
	e := newEnv(t)
	loki := fakeLokiServer(t, "alloy", "s3cret-push-pw", helmsmanLabels)
	alice := e.client()
	alice.login("alice")

	var res lokiStatusResp
	var probe testResp
	alice.json("POST", "/api/admin/integrations/loki/test", map[string]any{"url": loki.URL}, &probe)
	if probe.Reachable || !strings.Contains(probe.Error, "refused the login") {
		t.Fatalf("without credentials Loki says no, and the message says why: %+v", probe)
	}
	if code := alice.json("PUT", "/api/admin/integrations/loki", map[string]any{"url": loki.URL, "enabled": true}, &res); code != 400 || !strings.Contains(res.Error, "not turned on") {
		t.Fatalf("a connection that does not work must not be switched on: %d %+v", code, res)
	}

	pw := "s3cret-push-pw"
	if code := alice.json("PUT", "/api/admin/integrations/loki", map[string]any{"url": loki.URL, "user": "alloy", "password": pw, "enabled": true}, &res); code != 200 {
		t.Fatalf("%d %+v", code, res)
	}
	if !res.Loki.HasPassword || res.Loki.User != "alloy" {
		t.Fatalf("%+v", res.Loki)
	}
	raw := alice.do("GET", e.idp.URL+"/api/admin/integrations", nil)
	body, _ := io.ReadAll(raw.Body)
	if strings.Contains(string(body), pw) {
		t.Fatal("the password must never be sent back to the browser")
	}
	// Stored encrypted.
	var enc []byte
	if err := e.st.Pool.QueryRow(context.Background(), `SELECT secret_enc FROM integrations WHERE kind='loki'`).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	if len(enc) == 0 || strings.Contains(string(enc), pw) {
		t.Fatal("the password must be stored encrypted")
	}
	// Editing without a password keeps the stored one, so the connection still works.
	if code := alice.json("PUT", "/api/admin/integrations/loki", map[string]any{"url": loki.URL, "user": "alloy", "enabled": true}, &res); code != 200 || !res.Loki.HasPassword {
		t.Fatalf("omitted password must be kept: %d %+v", code, res)
	}
	if code := histCode(alice); code != 200 {
		t.Fatalf("history with the kept password: %d", code)
	}
	// The audit trail says who and what, never the password.
	rows, err := e.st.Pool.Query(context.Background(), `SELECT details::text FROM audit_events WHERE action LIKE 'integration.loki.%'`)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for rows.Next() {
		var d string
		_ = rows.Scan(&d)
		n++
		if strings.Contains(d, pw) {
			t.Fatalf("password in audit details: %s", d)
		}
	}
	if n < 3 {
		t.Fatalf("expected audit entries for test, failed save and save: %d", n)
	}
}

func TestLokiAddressesAreChecked(t *testing.T) {
	e := newEnv(t)
	alice := e.client()
	alice.login("alice")
	for _, u := range []string{"", "loki:3100", "ftp://loki:3100", "http://user:pw@loki:3100", "http://169.254.169.254/latest", "http://0.0.0.0:3100", "http://metadata.google.internal/", "http://loki:3100/?x=1", "javascript:alert(1)"} {
		if code := alice.json("POST", "/api/admin/integrations/loki/test", map[string]any{"url": u}, nil); code != 400 {
			t.Errorf("%q must be rejected, got %d", u, code)
		}
		if code := alice.json("PUT", "/api/admin/integrations/loki", map[string]any{"url": u, "enabled": false}, nil); code != 400 {
			t.Errorf("%q must be rejected on save, got %d", u, code)
		}
	}
	var probe testResp
	alice.json("POST", "/api/admin/integrations/loki/test", map[string]any{"url": "http://127.0.0.1:1"}, &probe)
	if probe.Reachable || !strings.Contains(probe.Error, "Nothing is listening") {
		t.Fatalf("a closed port gets a plain explanation: %+v", probe)
	}
}

func TestEmptyLokiIsReachableButSaysAlloyHasNotSentAnything(t *testing.T) {
	e := newEnv(t)
	loki := fakeLokiServer(t, "", "", nil)
	alice := e.client()
	alice.login("alice")
	var probe testResp
	alice.json("POST", "/api/admin/integrations/loki/test", map[string]any{"url": loki.URL}, &probe)
	if !probe.Reachable || probe.HasLogs || len(probe.Missing) == 0 {
		t.Fatalf("%+v", probe)
	}
	if code := alice.json("PUT", "/api/admin/integrations/loki", map[string]any{"url": loki.URL, "enabled": true}, nil); code != 200 {
		t.Fatalf("an empty Loki can still be connected, logs will arrive later: %d", code)
	}
}

func TestIntegrationsAreAdminOnly(t *testing.T) {
	e := newEnv(t)
	bob := e.client()
	bob.login("bob")
	e.bind("user", itoa(e.userID("bob")), "developer", "billing-api", "*")
	for _, c := range []struct{ m, p string }{{"GET", "/api/admin/integrations"}, {"POST", "/api/admin/integrations/loki/test"}, {"PUT", "/api/admin/integrations/loki"}, {"DELETE", "/api/admin/integrations/loki"}} {
		if code := bob.json(c.m, c.p, map[string]any{"url": "http://loki:3100"}, nil); code != 403 {
			t.Errorf("%s %s: %d", c.m, c.p, code)
		}
	}
}
