package store_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/poppyszn/helmsman/internal/store"
)

func openBoth(t *testing.T) (*store.Store, *store.Store) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; run via make test")
	}
	ctx := context.Background()
	owner, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	if _, err := owner.Pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := owner.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	app, err := store.OpenAs(ctx, dsn, "helmsman_app")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return owner, app
}

func TestAppRoleCannotChangeOrRemoveAuditEvents(t *testing.T) {
	_, app := openBoth(t)
	ctx := context.Background()
	if _, err := app.Pool.Exec(ctx, `INSERT INTO audit_events (actor_type, actor_id, action, result) VALUES ('system','t','x','ok')`); err != nil {
		t.Fatalf("insert must work: %v", err)
	}
	for _, q := range []string{
		`UPDATE audit_events SET result='tampered'`,
		`DELETE FROM audit_events`,
		`TRUNCATE audit_events`,
		`SELECT audit_purge(interval '100 days')`,
	} {
		_, err := app.Pool.Exec(ctx, q)
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s: want permission denied, got %v", q, err)
		}
	}
	var n int
	if err := app.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("select: %d %v", n, err)
	}
	if _, err := app.Pool.Exec(ctx, `SELECT * FROM goose_db_version`); err == nil {
		t.Error("the app role must not read the migration table")
	}
}

func TestOwnerCannotEditAuditButMayPurgeOldRows(t *testing.T) {
	owner, _ := openBoth(t)
	ctx := context.Background()
	if _, err := owner.Pool.Exec(ctx, `INSERT INTO audit_events (at, actor_type, actor_id, action, result) VALUES (now() - interval '400 days','system','t','old','ok'), (now(),'system','t','new','ok')`); err != nil {
		t.Fatal(err)
	}
	// The triggers still block ordinary edits for everyone, owner included.
	for _, q := range []string{`UPDATE audit_events SET result='x'`, `DELETE FROM audit_events`, `TRUNCATE audit_events`} {
		if _, err := owner.Pool.Exec(ctx, q); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s: want append-only, got %v", q, err)
		}
	}
	if _, err := owner.Pool.Exec(ctx, `SELECT audit_purge(interval '30 days')`); err == nil || !strings.Contains(err.Error(), "refuses") {
		t.Errorf("must refuse to keep under 90 days: %v", err)
	}
	var deleted int64
	if err := owner.Pool.QueryRow(ctx, `SELECT audit_purge(interval '365 days')`).Scan(&deleted); err != nil || deleted != 1 {
		t.Fatalf("purge: %d %v", deleted, err)
	}
	var left, purges int
	_ = owner.Pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE action='audit.purge') FROM audit_events`).Scan(&left, &purges)
	if left != 2 || purges != 1 {
		t.Fatalf("expected the new row plus a purge record, got %d rows %d purge records", left, purges)
	}
	// The purge switch does not leak into later statements.
	if _, err := owner.Pool.Exec(ctx, `DELETE FROM audit_events`); err == nil {
		t.Error("delete must be blocked again after a purge")
	}
}

func TestRetentionPrune(t *testing.T) {
	owner, app := openBoth(t)
	ctx := context.Background()
	if _, err := owner.Pool.Exec(ctx, `INSERT INTO agents (id,name) VALUES ('a','a');
		INSERT INTO apps (id,name,repo,catalog_rev) VALUES ('x','x','o/x','r');
		INSERT INTO app_envs (app_id, env, tier, agent_id, spec, catalog_rev) VALUES ('x','s','nonprod','a','{}','r');
		INSERT INTO health_checks (app_env_id, status, checked_at) SELECT id,'ok', now() - interval '2 days' FROM app_envs;
		INSERT INTO health_checks (app_env_id, status, checked_at) SELECT id,'ok', now() FROM app_envs;
		INSERT INTO users (github_id, login) VALUES (1,'u');
		INSERT INTO action_runs (id, correlation_id, requested_by, app_env_id, action, status, finished_at)
		  SELECT '11111111-1111-1111-1111-111111111111','22222222-2222-2222-2222-222222222222',u.id,e.id,'migrate','succeeded', now() - interval '40 days' FROM users u, app_envs e;
		INSERT INTO action_run_logs (run_id, job_id, job_name, content) VALUES ('11111111-1111-1111-1111-111111111111', 1, 'run', 'x');`); err != nil {
		t.Fatal(err)
	}
	if err := app.Prune(ctx, timeNow()); err != nil {
		t.Fatal(err)
	}
	var h, l, r int
	_ = owner.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM health_checks), (SELECT count(*) FROM action_run_logs), (SELECT count(*) FROM action_runs)`).Scan(&h, &l, &r)
	if h != 1 || l != 0 || r != 1 {
		t.Fatalf("health=%d (want 1) logs=%d (want 0) runs=%d (want 1, only logs expire)", h, l, r)
	}
}

func timeNow() time.Time { return time.Now() }
