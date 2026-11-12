package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/poppyszn/helmsman/internal/audit"
)

// Status is shown on the admin catalog page.
type Status struct {
	Path        string     `json:"path"`
	Rev         string     `json:"rev"`       // revision of the active (last good) catalog
	LoadedAt    *time.Time `json:"loaded_at"` // when it became active
	Apps        int        `json:"apps"`
	LastError   string     `json:"last_error"` // empty when the latest attempt succeeded
	LastErrorAt *time.Time `json:"last_error_at"`
	AttemptRev  string     `json:"attempt_rev"` // revision of the file last tried
}

// Manager keeps the last good catalog active and records why a reload failed.
type Manager struct {
	Pool        *pgxpool.Pool
	Path        string
	Audit       *audit.Writer
	AgentExists func(ctx context.Context, id string) (bool, error)

	mu        sync.Mutex
	status    Status
	lastTried string
}

// NewManager builds a manager.
func NewManager(pool *pgxpool.Pool, path string, a *audit.Writer, agentExists func(context.Context, string) (bool, error)) *Manager {
	return &Manager{Pool: pool, Path: path, Audit: a, AgentExists: agentExists, status: Status{Path: path}}
}

// Status returns a copy of the current status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Reload loads the file. On any error the previous catalog stays active and the error
// is recorded. force re-processes a file whose content has not changed.
func (m *Manager) Reload(ctx context.Context, actor string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.Path)
	if err != nil {
		return m.fail(ctx, actor, "", err)
	}
	sum := sha256.Sum256(data)
	rev := hex.EncodeToString(sum[:])[:12]
	if !force && rev == m.lastTried && m.status.LastError == "" {
		return nil
	}
	m.lastTried = rev

	cat, err := Parse(data)
	if err != nil {
		return m.fail(ctx, actor, rev, err)
	}
	// Resolve agent existence up front so Validate stays pure.
	known := map[string]bool{}
	for _, a := range cat.Apps {
		for _, e := range a.Environments {
			if e.Agent == "" {
				continue
			}
			ok, qerr := m.AgentExists(ctx, e.Agent)
			if qerr != nil {
				return m.fail(ctx, actor, rev, qerr)
			}
			known[e.Agent] = ok
		}
	}
	if errs := Validate(cat, func(id string) bool { return known[id] }); len(errs) > 0 {
		return m.fail(ctx, actor, rev, fmt.Errorf("%s", JoinErrors(errs)))
	}
	if err := m.sync(ctx, cat, rev); err != nil {
		return m.fail(ctx, actor, rev, err)
	}
	now := time.Now().UTC()
	m.status = Status{Path: m.Path, Rev: rev, LoadedAt: &now, Apps: len(cat.Apps), AttemptRev: rev}
	_ = m.Audit.Write(ctx, audit.Event{ActorType: audit.ActorSystem, ActorID: actor, Action: "catalog.reload",
		Target: rev, Result: "ok", Details: map[string]any{"apps": len(cat.Apps)}})
	return nil
}

func (m *Manager) fail(ctx context.Context, actor, rev string, err error) error {
	now := time.Now().UTC()
	m.status.LastError = err.Error()
	m.status.LastErrorAt = &now
	m.status.AttemptRev = rev
	_ = m.Audit.Write(ctx, audit.Event{ActorType: audit.ActorSystem, ActorID: actor, Action: "catalog.reload",
		Target: rev, Result: "error", Details: map[string]any{"error": err.Error()}})
	return err
}

// sync upserts apps and envs and soft-deletes anything no longer present.
func (m *Manager) sync(ctx context.Context, c *Catalog, rev string) error {
	tx, err := m.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var appIDs, envKeys []string
	for _, a := range c.Apps {
		appIDs = append(appIDs, a.ID)
		if _, err := tx.Exec(ctx, `
			INSERT INTO apps (id, name, repo, catalog_rev, deleted_at) VALUES ($1,$2,$3,$4,NULL)
			ON CONFLICT (id) DO UPDATE SET name=$2, repo=$3, catalog_rev=$4, deleted_at=NULL`,
			a.ID, a.Name, a.Repo, rev); err != nil {
			return err
		}
		for _, e := range a.Environments {
			allow := e.SeedAllowed()
			e.AllowSeed = &allow
			e.Links = append(append([]Link{}, a.Links...), e.Links...)
			spec, _ := json.Marshal(e)
			envKeys = append(envKeys, a.ID+"/"+e.Env)
			if _, err := tx.Exec(ctx, `
				INSERT INTO app_envs (app_id, env, tier, agent_id, spec, catalog_rev, deleted_at)
				VALUES ($1,$2,$3,$4,$5,$6,NULL)
				ON CONFLICT (app_id, env) DO UPDATE SET tier=$3, agent_id=$4, spec=$5, catalog_rev=$6, deleted_at=NULL`,
				a.ID, e.Env, e.Tier, e.Agent, spec, rev); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE apps SET deleted_at=now() WHERE deleted_at IS NULL AND NOT (id = ANY($1))`, appIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE app_envs SET deleted_at=now() WHERE deleted_at IS NULL AND NOT ((app_id||'/'||env) = ANY($1))`, envKeys); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Watch reloads the catalog on an interval until ctx ends. Unchanged files are skipped.
func (m *Manager) Watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = m.Reload(ctx, "watcher", false)
		}
	}
}
