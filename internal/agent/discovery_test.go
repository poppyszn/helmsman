package agent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// fakeDockerSocket answers GET /containers/json with a fixed list.
func fakeDockerSocket(t *testing.T, list []map[string]any) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(list)
	})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return sock
}

func TestDiscoveredListsOnlyWhatIsNotExposed(t *testing.T) {
	labels := func(p, s string) map[string]string {
		return map[string]string{"com.docker.compose.project": p, "com.docker.compose.service": s}
	}
	sock := fakeDockerSocket(t, []map[string]any{
		{"Image": "shop/api:1", "State": "running", "Labels": labels("shop", "api")},
		{"Image": "shop/admin:1", "State": "running", "Labels": labels("shop", "admin")},
		{"Image": "shop/admin:0", "State": "exited", "Labels": labels("shop", "admin")}, // an old container of the same service
		{"Image": "redis:7", "State": "running", "Labels": labels("other", "redis")},
		{"Image": "tool", "State": "running", "Labels": map[string]string{}}, // not a compose container
	})
	cfg := Config{Allow: []Allow{{ComposeProject: "shop", Services: []string{"api"}, Actions: []string{ActLogs}}}}
	got, err := NewDocker(sock).Discovered(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ComposeProject != "other" || got[0].Service != "redis" ||
		got[1].ComposeProject != "shop" || got[1].Service != "admin" || got[1].State != "running" || got[1].Image != "shop/admin:1" {
		t.Fatalf("expected other/redis and the running shop/admin, got %+v", got)
	}
}

func TestSnapshotCarriesDiscoveryOnlyWhenTurnedOn(t *testing.T) {
	sock := fakeDockerSocket(t, []map[string]any{
		{"Image": "redis:7", "State": "running", "Labels": map[string]string{"com.docker.compose.project": "other", "com.docker.compose.service": "redis"}},
	})
	a := &Agent{docker: NewDocker(sock)}
	if s := a.snapshot(context.Background()); len(s.Discovered) != 0 {
		t.Fatalf("discovery is off by default: %+v", s.Discovered)
	}
	a.cfg.Discovery = true
	if s := a.snapshot(context.Background()); len(s.Discovered) != 1 {
		t.Fatalf("discovery on: %+v", s.Discovered)
	}
}
