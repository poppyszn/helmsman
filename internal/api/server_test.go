package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func newTestServer(t *testing.T, db Pinger) *Server {
	t.Helper()
	s, err := New(db, fstest.MapFS{"index.html": {Data: []byte("<html>shell</html>")}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func get(s *Server, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthAndReady(t *testing.T) {
	if c := get(newTestServer(t, fakeDB{}), "/healthz").Code; c != 200 {
		t.Fatalf("healthz %d", c)
	}
	if c := get(newTestServer(t, fakeDB{}), "/readyz").Code; c != 200 {
		t.Fatalf("readyz %d", c)
	}
	if c := get(newTestServer(t, fakeDB{errors.New("down")}), "/readyz").Code; c != 503 {
		t.Fatalf("readyz down %d", c)
	}
}

func TestEveryRouteDeclaresPermission(t *testing.T) {
	s := newTestServer(t, fakeDB{})
	for _, r := range s.Routes() {
		if r.Permission == "" {
			t.Errorf("%s %s has no permission", r.Method, r.Path)
		}
	}
}

func TestUnknownAPIPathIs404NotShell(t *testing.T) {
	s := newTestServer(t, fakeDB{})
	if c := get(s, "/api/nope").Code; c != 404 {
		t.Fatalf("got %d", c)
	}
	if rec := get(s, "/apps/x"); rec.Code != 200 || rec.Body.String() != "<html>shell</html>" {
		t.Fatalf("spa fallback failed: %d", rec.Code)
	}
}
