package notify

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMatches(t *testing.T) {
	ev := Event{Type: EnvDown, Tier: "prod"}
	for _, c := range []struct {
		events []string
		tier   string
		ev     Event
		want   bool
	}{
		{[]string{EnvDown}, "all", ev, true},
		{[]string{EnvDown}, "prod", ev, true},
		{[]string{EnvDown}, "nonprod", ev, false},
		{[]string{RunFailed}, "all", ev, false},
		{[]string{EnvDown}, "prod", Event{Type: EnvDown}, false}, // no tier: only "all" channels
		{[]string{EnvDown}, "all", Event{Type: EnvDown}, true},
	} {
		if got := Matches(c.events, c.tier, c.ev); got != c.want {
			t.Errorf("%v %s %+v: got %v", c.events, c.tier, c.ev, got)
		}
	}
}

func TestWebhookURLValidation(t *testing.T) {
	for _, ok := range []string{"https://hooks.example.com/x", "http://10.0.0.5:9000/hook", "http://localhost:8080/h"} {
		if _, err := ValidateWebhookURL(ok); err != nil {
			t.Errorf("%s must be allowed: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "ftp://x.com", "https://u:p@x.com", "http://169.254.169.254/latest", "http://[fe80::1]/x", "http://0.0.0.0/", "http://metadata.google.internal/x", "https://x.com/#frag"} {
		if _, err := ValidateWebhookURL(bad); err == nil {
			t.Errorf("%s must be refused", bad)
		}
	}
}

func TestWebhookIsSignedAndDoesNotFollowRedirectsOrReachMetadata(t *testing.T) {
	var got struct {
		sig, ts, event string
		body           string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.sig, got.ts, got.event, got.body = r.Header.Get("X-Helmsman-Signature"), r.Header.Get("X-Helmsman-Timestamp"), r.Header.Get("X-Helmsman-Event"), string(b)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
			return
		}
	}))
	defer srv.Close()
	h := &httpWebhook{}
	if err := h.Post(context.Background(), srv.URL+"/hook", "s3cret", "env.down", Payload{Event: "env.down", Title: "x"}); err != nil {
		t.Fatal(err)
	}
	var ts int64
	for _, c := range got.ts {
		ts = ts*10 + int64(c-'0')
	}
	if got.event != "env.down" || got.sig != Sign("s3cret", ts, []byte(got.body)) || !strings.HasPrefix(got.sig, "sha256=") {
		t.Fatalf("signature: %+v", got)
	}
	if err := h.Post(context.Background(), srv.URL+"/redirect", "", "env.down", Payload{}); err == nil || !strings.Contains(err.Error(), "302") {
		t.Fatalf("a redirect is an error, not followed: %v", err)
	}
	// The connection itself refuses a blocked address, whatever the name resolved to.
	if err := h.Post(context.Background(), "http://169.254.169.254/latest", "", "e", Payload{}); err == nil {
		t.Fatal("the metadata address must be refused at connect time")
	}
}

func TestMessageHeadersCannotBeInjected(t *testing.T) {
	m := string(buildMessage("Helmsman <h@example.com>", []string{"a@example.com"}, "Down\r\nBcc: evil@example.com", "body\nline"))
	head := m[:strings.Index(m, "\r\n\r\n")]
	if strings.Contains(head, "\nBcc:") || strings.Count(head, "Subject:") != 1 {
		t.Fatalf("header injection:\n%s", head)
	}
	if !strings.Contains(m, "body\r\nline") {
		t.Fatal("body lines must use CRLF")
	}
}

// fakeSMTP is a tiny server with no TLS and no auth that records one message.
func fakeSMTP(t *testing.T) (addr string, got chan string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	got = make(chan string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		w("220 fake ESMTP")
		var rcpt []string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			up := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
				w("250 fake")
			case strings.HasPrefix(up, "MAIL"):
				w("250 ok")
			case strings.HasPrefix(up, "RCPT"):
				rcpt = append(rcpt, strings.TrimSpace(line))
				w("250 ok")
			case up == "DATA":
				w("354 go")
				var b strings.Builder
				for {
					l, _ := r.ReadString('\n')
					if l == ".\r\n" {
						break
					}
					b.WriteString(l)
				}
				got <- strings.Join(rcpt, "|") + "\n" + b.String()
				w("250 queued")
			case up == "QUIT":
				w("221 bye")
				return
			}
		}
	}()
	return l.Addr().String(), got
}

func TestSMTPInputIsChecked(t *testing.T) {
	good := SMTPInput{Host: "smtp.example.com", Port: 587, Security: "starttls", From: "helmsman@example.com"}
	if err := good.normalize(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*SMTPInput){
		"url as host":  func(i *SMTPInput) { i.Host = "https://smtp.example.com" },
		"host + port":  func(i *SMTPInput) { i.Host = "smtp.example.com:587" },
		"empty host":   func(i *SMTPInput) { i.Host = "" },
		"metadata":     func(i *SMTPInput) { i.Host = "169.254.169.254" },
		"bad security": func(i *SMTPInput) { i.Security = "maybe" },
		"bad port":     func(i *SMTPInput) { i.Port = 70000 },
		"bad from":     func(i *SMTPInput) { i.From = "not an address" },
		"from inject":  func(i *SMTPInput) { i.From = "a@b.com\r\nBcc: c@d.com" },
	} {
		in := good
		mut(&in)
		if err := in.normalize(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestChannelInputIsChecked(t *testing.T) {
	in := ChannelInput{Name: "x", Kind: "email", To: []string{"a@example.com", " b@example.com "}, Events: []string{EnvDown, EnvDown}}
	if err := in.normalize(); err != nil || len(in.Events) != 1 || in.To[1] != "b@example.com" || in.Tier != "all" {
		t.Fatalf("%v %+v", err, in)
	}
	long := ChannelInput{Name: "x", Kind: "email", Events: []string{EnvDown}}
	for i := 0; i < 21; i++ {
		long.To = append(long.To, "u"+string(rune('a'+i%26))+string(rune('a'+i/26))+"@example.com")
	}
	if err := long.normalize(); err == nil {
		t.Fatal("more than 20 recipients must be refused")
	}
}

func TestTextRendersEvent(t *testing.T) {
	subj, body := Text(Event{Title: "billing / prod is down", Message: "m", Details: map[string]string{"b": "2", "a": "1"}, Path: "/apps/billing"}, "https://portal.example.com/")
	if subj != "[Helmsman] billing / prod is down" || !strings.Contains(body, "a: 1\nb: 2") || !strings.Contains(body, "https://portal.example.com/apps/billing") {
		t.Fatalf("%q\n%s", subj, body)
	}
}

var _ = time.Second
var _ = fakeSMTP
