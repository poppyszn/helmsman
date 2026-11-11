package gh_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/poppyszn/helmsman/internal/fakegithub"
	"github.com/poppyszn/helmsman/internal/gh"
)

func key(t *testing.T) *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestJWTShape(t *testing.T) {
	a := &gh.AppAuth{AppID: "42", Key: key(t)}
	j, err := a.JWT()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(j, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt: %s", j)
	}
	b, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c struct {
		Iss string `json:"iss"`
		Exp int64  `json:"exp"`
		Iat int64  `json:"iat"`
	}
	_ = json.Unmarshal(b, &c)
	if c.Iss != "42" || c.Exp-c.Iat > 600+60 || c.Exp <= time.Now().Unix() {
		t.Fatalf("claims: %+v", c)
	}
}

func TestInstallationTokenCachedAndRefreshedAtFiftyMinutes(t *testing.T) {
	f := fakegithub.New()
	var mints atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			mints.Add(1)
		}
		f.ServeHTTP(w, r)
	}))
	defer srv.Close()
	now := time.Now()
	a := &gh.AppAuth{AppID: "1", Key: key(t), APIURL: srv.URL, Now: func() time.Time { return now }}
	ctx := context.Background()
	t1, err := a.Token(ctx, "acme-org/demo-app")
	if err != nil {
		t.Fatal(err)
	}
	if t2, _ := a.Token(ctx, "acme-org/demo-app"); t2 != t1 || mints.Load() != 1 {
		t.Fatal("token must be cached")
	}
	now = now.Add(49 * time.Minute)
	if t2, _ := a.Token(ctx, "acme-org/demo-app"); t2 != t1 {
		t.Fatal("still fresh at 49 minutes")
	}
	now = now.Add(2 * time.Minute)
	if t3, _ := a.Token(ctx, "acme-org/demo-app"); t3 == t1 || mints.Load() != 2 {
		t.Fatal("must refresh after 50 minutes")
	}
	if _, err := a.Token(ctx, "acme-org/unknown"); err != gh.ErrNoInstallation {
		t.Fatalf("not installed: %v", err)
	}
}

func TestETagSavesRequestsAndCacheTTLSkipsThem(t *testing.T) {
	f := fakegithub.New()
	var runs, notModified atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/runs") {
			runs.Add(1)
		}
		rec := httptest.NewRecorder()
		f.ServeHTTP(rec, r)
		if rec.Code == 304 {
			notModified.Add(1)
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	defer srv.Close()
	c := gh.NewClient(srv.URL, &gh.AppAuth{AppID: "1", Key: key(t), APIURL: srv.URL})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := c.ListRuns(ctx, "acme-org/demo-app", "deploy.yml", 10, 0); err != nil {
			t.Fatal(err)
		}
	}
	if runs.Load() != 3 || notModified.Load() != 2 {
		t.Fatalf("expected conditional requests: calls=%d 304s=%d", runs.Load(), notModified.Load())
	}
	for i := 0; i < 5; i++ {
		if _, err := c.ListRuns(ctx, "acme-org/demo-app", "deploy.yml", 10, 30*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if runs.Load() != 3 {
		t.Fatalf("the 30s cache must avoid requests: %d", runs.Load())
	}
}

func TestRateLimitBlocksFurtherCallsUntilRetryAfter(t *testing.T) {
	f := fakegithub.New()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/acme-org/demo-app/actions") {
			calls.Add(1)
		}
		f.ServeHTTP(w, r)
	}))
	defer srv.Close()
	c := gh.NewClient(srv.URL, &gh.AppAuth{AppID: "1", Key: key(t), APIURL: srv.URL})
	f.SetScenario(fakegithub.Scenario{RateLimitCount: 1, RateLimitRetryAfter: 30})
	_, err := c.ListRuns(context.Background(), "acme-org/demo-app", "deploy.yml", 10, 0)
	rl, ok := err.(*gh.RateLimitError)
	if !ok || rl.RetryAfter != 30*time.Second {
		t.Fatalf("want RateLimitError(30s): %v", err)
	}
	if _, err := c.ListRuns(context.Background(), "acme-org/demo-app", "deploy.yml", 10, 0); err == nil || calls.Load() != 1 {
		t.Fatalf("must fail fast without calling GitHub: err=%v calls=%d", err, calls.Load())
	}
}

func TestHasDispatchTrigger(t *testing.T) {
	for in, want := range map[string]bool{
		"on:\n  workflow_dispatch:\n":      true,
		"on: [push, workflow_dispatch]\n":  true,
		"on: push\n":                       false,
		"# workflow_dispatch:\non: push\n": false,
	} {
		if gh.HasDispatchTrigger(in) != want {
			t.Errorf("%q", in)
		}
	}
}
