package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// EnvRow is one environment of an app as stored.
type EnvRow struct {
	ID    int64           `json:"id"`
	Env   string          `json:"env"`
	Tier  string          `json:"tier"`
	Agent string          `json:"agent"`
	Spec  json.RawMessage `json:"spec"`
}

// AppRow is an app with its (unfiltered) environments.
type AppRow struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Repo string   `json:"repo"`
	Envs []EnvRow `json:"environments"`
}

// ListApps returns all non-deleted apps with their non-deleted envs.
func (s *Store) ListApps(ctx context.Context) ([]AppRow, error) {
	return s.loadApps(ctx, "")
}

// GetApp returns one non-deleted app.
func (s *Store) GetApp(ctx context.Context, id string) (AppRow, error) {
	apps, err := s.loadApps(ctx, id)
	if err != nil {
		return AppRow{}, err
	}
	if len(apps) == 0 {
		return AppRow{}, ErrNotFound
	}
	return apps[0], nil
}

func (s *Store) loadApps(ctx context.Context, only string) ([]AppRow, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT a.id, a.name, a.repo, e.id, e.env, e.tier, e.agent_id, e.spec
		FROM apps a JOIN app_envs e ON e.app_id=a.id AND e.deleted_at IS NULL
		WHERE a.deleted_at IS NULL AND ($1 = '' OR a.id = $1)
		ORDER BY a.id, e.id`, only)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AppRow{}
	for rows.Next() {
		var a AppRow
		var e EnvRow
		if err := rows.Scan(&a.ID, &a.Name, &a.Repo, &e.ID, &e.Env, &e.Tier, &e.Agent, &e.Spec); err != nil {
			return nil, err
		}
		if n := len(out); n > 0 && out[n-1].ID == a.ID {
			out[n-1].Envs = append(out[n-1].Envs, e)
		} else {
			a.Envs = []EnvRow{e}
			out = append(out, a)
		}
	}
	return out, rows.Err()
}

// EnvTier returns the tier of a live env, or ErrNotFound.
func (s *Store) EnvTier(ctx context.Context, appID, env string) (string, error) {
	var tier string
	err := s.Pool.QueryRow(ctx, `
		SELECT e.tier FROM app_envs e JOIN apps a ON a.id=e.app_id
		WHERE e.app_id=$1 AND e.env=$2 AND e.deleted_at IS NULL AND a.deleted_at IS NULL`, appID, env).Scan(&tier)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return tier, err
}

// AppExists reports whether a live app exists.
func (s *Store) AppExists(ctx context.Context, id string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM apps WHERE id=$1 AND deleted_at IS NULL)`, id).Scan(&ok)
	return ok, err
}

// AgentExists reports whether an agent id is registered and not revoked.
func (s *Store) AgentExists(ctx context.Context, id string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE id=$1 AND revoked_at IS NULL)`, id).Scan(&ok)
	return ok, err
}

// AllAgentIDs returns registered, non-revoked agent ids.
func (s *Store) AllAgentIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id FROM agents WHERE revoked_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
