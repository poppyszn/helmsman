package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/poppyszn/helmsman/pkg/protocol"
)

type timelineResp struct {
	Entries []struct {
		At     time.Time `json:"at"`
		Kind   string    `json:"kind"`
		Level  string    `json:"level"`
		Title  string    `json:"title"`
		Detail string    `json:"detail"`
		Actor  string    `json:"actor"`
	} `json:"entries"`
}

func (t timelineResp) has(kind, title string) bool {
	for _, e := range t.Entries {
		if e.Kind == kind && strings.Contains(e.Title, title) {
			return true
		}
	}
	return false
}

func TestTimelineMergesWhatHappenedToAnEnvironment(t *testing.T) {
	r := newRunsEnv(t)
	ctx := context.Background()
	st := r.st
	exit := 1
	now := time.Now()
	for _, ev := range []protocol.Event{
		{Kind: protocol.EventDie, ComposeProject: "billing", Service: "api", ExitCode: &exit, At: now.Add(-40 * time.Minute)},
		{Kind: protocol.EventStart, ComposeProject: "billing", Service: "api", At: now.Add(-39 * time.Minute)},
	} {
		if err := st.AddEvent(ctx, "agent-1", ev); err != nil {
			t.Fatal(err)
		}
	}
	var envID int64
	if err := st.Pool.QueryRow(ctx, `SELECT id FROM app_envs WHERE app_id = 'billing-api' AND env = 'staging'`).Scan(&envID); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"fail", "fail", "ok"} {
		if err := st.AddHealth(ctx, envID, s, 502, 10); err != nil {
			t.Fatal(err)
		}
	}
	// A migration, a deployment, and a restart from Helmsman.
	_, out := r.start(r.bob, "billing-api", "staging", "migrate")
	r.settle(out.Run.ID, "succeeded")
	r.gh.AddRun("acme-org/billing-api", "deploy.yml", "1111111111111111111111111111111111111111", "bob", "failure", now.Add(-20*time.Minute))
	if _, err := st.Pool.Exec(ctx, `INSERT INTO audit_events (actor_type, actor_id, action, target, result) VALUES ('user', 'bob', 'action.restart', 'billing-api/staging', 'ok')`); err != nil {
		t.Fatal(err)
	}
	// Something about another environment must not appear.
	if _, err := st.Pool.Exec(ctx, `INSERT INTO audit_events (actor_type, actor_id, action, target, result) VALUES ('user', 'bob', 'action.restart', 'billing-api/prod', 'ok')`); err != nil {
		t.Fatal(err)
	}

	var tl timelineResp
	if code := r.bob.json("GET", "/api/apps/billing-api/envs/staging/timeline", nil, &tl); code != 200 {
		t.Fatalf("timeline: %d", code)
	}
	for _, want := range []struct{ kind, title string }{
		{"container", "Container exited"}, {"container", "Container started"},
		{"health", "started failing"}, {"health", "recovered"},
		{"run", "Migration requested"}, {"run", "Migration succeeded"},
		{"deploy", "failed"}, {"restart", "Restarted from Helmsman"},
	} {
		if !tl.has(want.kind, want.title) {
			t.Errorf("missing %s %q in %+v", want.kind, want.title, tl.Entries)
		}
	}
	restarts := 0
	for i, e := range tl.Entries {
		if e.Kind == "restart" {
			restarts++
		}
		if i > 0 && e.At.After(tl.Entries[i-1].At) {
			t.Errorf("entries must be newest first: %v after %v", e.At, tl.Entries[i-1].At)
		}
		if e.Kind == "container" && e.Title == "Container exited" && e.Detail != "exit 1" {
			t.Errorf("exit code: %+v", e)
		}
	}
	if restarts != 1 {
		t.Errorf("only this environment's restarts: %d", restarts)
	}
	// Someone without access to the app gets nothing.
	carol := r.client()
	carol.login("carol")
	if code := carol.json("GET", "/api/apps/billing-api/envs/staging/timeline", nil, nil); code != 403 {
		t.Fatalf("no binding: %d", code)
	}
}
