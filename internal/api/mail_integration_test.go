package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeMailpit answers the few Mailpit API calls Helmsman makes.
func fakeMailpit(t *testing.T, user, pass string) *httptest.Server {
	t.Helper()
	msgs := []map[string]any{
		{"ID": "abc1", "Read": false, "From": map[string]string{"Name": "Shop", "Address": "shop@example.com"},
			"To": []map[string]string{{"Name": "", "Address": "buyer@example.com"}}, "Subject": "Your order",
			"Created": "2026-11-11T10:00:00Z", "Snippet": "Thanks for your order", "Size": 1200, "Attachments": 1},
		{"ID": "abc2", "Read": true, "From": map[string]string{"Name": "", "Address": "noreply@example.com"},
			"To": []map[string]string{{"Name": "", "Address": "admin@example.com"}}, "Subject": "Reset your password",
			"Created": "2026-11-11T09:00:00Z", "Snippet": "Use the link", "Size": 800, "Attachments": 0},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != pass {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"Version": "v1.21.0"})
		case r.URL.Path == "/api/v1/messages" || r.URL.Path == "/api/v1/search":
			out := []map[string]any{}
			q := strings.ToLower(r.URL.Query().Get("query"))
			for _, m := range msgs {
				if q == "" || strings.Contains(strings.ToLower(m["Subject"].(string)), q) {
					out = append(out, m)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total": len(out), "unread": 1, "messages": out})
		case r.URL.Path == "/api/v1/message/abc1":
			_ = json.NewEncoder(w).Encode(map[string]any{"ID": "abc1", "Subject": "Your order", "Date": "2026-11-11T10:00:00Z",
				"From": map[string]string{"Name": "Shop", "Address": "shop@example.com"},
				"To":   []map[string]string{{"Name": "", "Address": "buyer@example.com"}},
				"HTML": "<p>Thanks for your order</p><script>alert(1)</script>", "Text": "Thanks for your order",
				"Attachments": []map[string]any{{"PartID": "2", "FileName": "receipt.html", "ContentType": "text/html", "Size": 30}}})
		case r.URL.Path == "/api/v1/message/abc1/part/2":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<script>alert('x')</script>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type mailList struct {
	Connected bool `json:"connected"`
	Total     int  `json:"total"`
	Messages  []struct {
		ID      string `json:"ID"`
		Subject string `json:"Subject"`
	} `json:"messages"`
	Error string `json:"error"`
}

func TestMailViewerIsConnectedFromTheIntegrationsPage(t *testing.T) {
	e := newEnv(t)
	mp := fakeMailpit(t, "dev", "s3cret")
	alice, bob, carol := e.client(), e.client(), e.client()
	alice.login("alice")
	bob.login("bob")
	carol.login("carol")
	e.bind("user", itoa(e.userID("bob")), "developer", "*", "*")
	e.bind("user", itoa(e.userID("carol")), "viewer", "*", "*")

	var l mailList
	if code := bob.json("GET", "/api/mail/messages", nil, &l); code != 200 || l.Connected {
		t.Fatalf("not connected out of the box: %d %+v", code, l)
	}

	// A wrong password is reported and nothing is turned on.
	var st struct {
		Mailpit struct {
			Connected   bool   `json:"connected"`
			URL         string `json:"url"`
			User        string `json:"user"`
			HasPassword bool   `json:"has_password"`
			Source      string `json:"source"`
		} `json:"mailpit"`
		Error string `json:"error"`
	}
	bad := "wrong"
	if code := alice.json("PUT", "/api/admin/integrations/mailpit", map[string]any{"url": mp.URL, "user": "dev", "password": bad, "enabled": true}, &st); code != 400 {
		t.Fatalf("a login Mailpit refuses must not be saved: %d %+v", code, st)
	}
	var tr struct {
		Reachable bool   `json:"reachable"`
		Version   string `json:"version"`
		Total     int    `json:"total"`
	}
	if code := alice.json("POST", "/api/admin/integrations/mailpit/test", map[string]any{"url": mp.URL, "user": "dev", "password": "s3cret"}, &tr); code != 200 || !tr.Reachable || tr.Version != "v1.21.0" || tr.Total != 2 {
		t.Fatalf("test: %d %+v", code, tr)
	}
	if code := alice.json("PUT", "/api/admin/integrations/mailpit", map[string]any{"url": mp.URL + "/", "user": "dev", "password": "s3cret", "enabled": true}, &st); code != 200 {
		t.Fatalf("save: %d", code)
	}
	if !st.Mailpit.Connected || st.Mailpit.Source != "database" || !st.Mailpit.HasPassword || st.Mailpit.URL != mp.URL {
		t.Fatalf("status: %+v", st.Mailpit)
	}
	var raw string
	if err := e.st.Pool.QueryRow(context.Background(), `SELECT encode(secret_enc, 'escape') FROM integrations WHERE kind = 'mailpit'`).Scan(&raw); err != nil || strings.Contains(raw, "s3cret") {
		t.Fatalf("the password must be stored encrypted: %v", err)
	}

	// People with mail.view on every app and environment read the mail; others do not.
	if code := bob.json("GET", "/api/mail/messages", nil, &l); code != 200 || !l.Connected || l.Total != 2 || l.Messages[0].Subject != "Your order" {
		t.Fatalf("list: %d %+v", code, l)
	}
	if code := bob.json("GET", "/api/mail/messages?q=password", nil, &l); code != 200 || l.Total != 1 || l.Messages[0].ID != "abc2" {
		t.Fatalf("search: %d %+v", code, l)
	}
	if code := carol.json("GET", "/api/mail/messages", nil, nil); code != 403 {
		t.Fatalf("a viewer has no mail.view: %d", code)
	}

	// Opening a message is audited without its text; an attachment is always a download.
	var m struct {
		Message struct {
			Subject string `json:"Subject"`
			HTML    string `json:"HTML"`
		} `json:"message"`
	}
	if code := bob.json("GET", "/api/mail/messages/abc1", nil, &m); code != 200 || m.Message.Subject != "Your order" || m.Message.HTML == "" {
		t.Fatalf("message: %d %+v", code, m)
	}
	var n int
	if err := e.st.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'mail.view' AND actor_id = 'bob' AND details::text NOT LIKE '%Your order%'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit rows for opening a message: %d %v", n, err)
	}
	res := bob.do("GET", e.idp.URL+"/api/mail/messages/abc1/attachments/2", nil)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/octet-stream" ||
		!strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("attachment must be a download: %d %v", res.StatusCode, res.Header)
	}
	_ = res.Body.Close()
	if code := bob.json("GET", "/api/mail/messages/abc1/attachments/9", nil, nil); code != 404 {
		t.Fatalf("a part the message does not have: %d", code)
	}
	if code := bob.json("GET", "/api/mail/messages/..%2Fetc", nil, nil); code != 404 {
		t.Fatalf("a malformed id: %d", code)
	}
	if code := bob.json("GET", "/api/mail/messages/nope", nil, nil); code != 404 {
		t.Fatalf("a message Mailpit does not have: %d", code)
	}

	// Disconnecting turns the page off again.
	if code := alice.json("DELETE", "/api/admin/integrations/mailpit", nil, nil); code != 200 {
		t.Fatalf("remove: %d", code)
	}
	if code := bob.json("GET", "/api/mail/messages", nil, &l); code != 200 || l.Connected {
		t.Fatalf("after remove: %d %+v", code, l)
	}
}
