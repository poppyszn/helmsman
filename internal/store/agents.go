package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/poppyszn/helmsman/pkg/protocol"
)

// ErrConflict means the row already exists or is in the wrong state.
var ErrConflict = errors.New("conflict")

// AgentRow is an agent as the admin page shows it. Credentials never leave the store.
type AgentRow struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Version         *string    `json:"version"`
	Hostname        *string    `json:"hostname"`
	ProtocolVersion *int       `json:"protocol_version"`
	DockerVersion   *string    `json:"docker_version"`
	DockerOK        *bool      `json:"docker_ok"`
	AllowlistDigest *string    `json:"allowlist_digest"`
	LastSeenAt      *time.Time `json:"last_seen_at"`
	RevokedAt       *time.Time `json:"revoked_at"`
	CreatedAt       time.Time  `json:"created_at"`
	Alert           *string    `json:"alert"`
	AlertAt         *time.Time `json:"alert_at"`
	Joined          bool       `json:"joined"`
	JoinPending     bool       `json:"join_pending"`
}

const agentCols = `id, name, version, hostname, protocol_version, docker_version, docker_ok, allowlist_digest,
	last_seen_at, revoked_at, created_at, alert, alert_at, credential_hash IS NOT NULL,
	(join_token_hash IS NOT NULL AND join_expires_at > now())`

func scanAgent(r pgx.Row) (AgentRow, error) {
	var a AgentRow
	err := r.Scan(&a.ID, &a.Name, &a.Version, &a.Hostname, &a.ProtocolVersion, &a.DockerVersion, &a.DockerOK,
		&a.AllowlistDigest, &a.LastSeenAt, &a.RevokedAt, &a.CreatedAt, &a.Alert, &a.AlertAt, &a.Joined, &a.JoinPending)
	return a, err
}

// ListAgents returns every agent, newest first.
func (s *Store) ListAgents(ctx context.Context) ([]AgentRow, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+agentCols+` FROM agents ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentRow{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAgent returns one agent.
func (s *Store) GetAgent(ctx context.Context, id string) (AgentRow, error) {
	a, err := scanAgent(s.Pool.QueryRow(ctx, `SELECT `+agentCols+` FROM agents WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// CreateAgent registers an agent with a one-time join token (stored as a hash).
func (s *Store) CreateAgent(ctx context.Context, id, name string, tokenHash []byte, expires time.Time) error {
	tag, err := s.Pool.Exec(ctx, `INSERT INTO agents (id, name, join_token_hash, join_expires_at)
		VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, id, name, tokenHash, expires)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// ReissueJoin replaces the join token of an agent that has not joined yet.
func (s *Store) ReissueJoin(ctx context.Context, id string, tokenHash []byte, expires time.Time) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE agents SET join_token_hash=$2, join_expires_at=$3
		WHERE id=$1 AND credential_hash IS NULL AND revoked_at IS NULL`, id, tokenHash, expires)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// ExchangeJoin atomically consumes a valid join token and stores the credential hash.
func (s *Store) ExchangeJoin(ctx context.Context, tokenHash, credHash []byte, hostname, version string) (string, error) {
	var id string
	err := s.Pool.QueryRow(ctx, `UPDATE agents SET credential_hash=$2, join_token_hash=NULL, join_expires_at=NULL,
		hostname=NULLIF($3,''), version=NULLIF($4,'')
		WHERE join_token_hash=$1 AND join_expires_at > now() AND revoked_at IS NULL RETURNING id`,
		tokenHash, credHash, hostname, version).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// AgentByCredential finds a live agent by credential hash.
func (s *Store) AgentByCredential(ctx context.Context, credHash []byte) (AgentRow, error) {
	a, err := scanAgent(s.Pool.QueryRow(ctx, `SELECT `+agentCols+` FROM agents
		WHERE credential_hash=$1 AND revoked_at IS NULL`, credHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// RevokeAgent disables an agent and forgets its credential.
func (s *Store) RevokeAgent(ctx context.Context, id string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE agents SET revoked_at=now(), credential_hash=NULL,
		join_token_hash=NULL, join_expires_at=NULL WHERE id=$1 AND revoked_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AgentHello records what the agent announced.
func (s *Store) AgentHello(ctx context.Context, id string, h protocol.Hello) error {
	_, err := s.Pool.Exec(ctx, `UPDATE agents SET version=$2, hostname=$3, protocol_version=$4,
		docker_version=NULLIF($5,''), allowlist_digest=$6, last_seen_at=now() WHERE id=$1`,
		id, h.AgentVersion, h.Hostname, h.ProtocolVersion, h.DockerVersion, h.AllowlistDigest)
	return err
}

// TouchAgent updates last_seen_at.
func (s *Store) TouchAgent(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE agents SET last_seen_at=now() WHERE id=$1`, id)
	return err
}

// SetAgentAlert raises (msg != "") or clears an admin alert.
func (s *Store) SetAgentAlert(ctx context.Context, id, msg string) error {
	if msg == "" {
		_, err := s.Pool.Exec(ctx, `UPDATE agents SET alert=NULL, alert_at=NULL WHERE id=$1 AND alert IS NOT NULL`, id)
		return err
	}
	_, err := s.Pool.Exec(ctx, `UPDATE agents SET alert=$2, alert_at=now() WHERE id=$1`, id, msg)
	return err
}

// SaveSnapshot replaces the container state of an agent. When Docker is down the previous
// rows stay (they are marked stale by the agent's docker_ok flag).
func (s *Store) SaveSnapshot(ctx context.Context, agentID string, snap protocol.Snapshot) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `UPDATE agents SET docker_ok=$2, last_seen_at=now() WHERE id=$1`, agentID, snap.DockerOK); err != nil {
		return err
	}
	if snap.DockerOK {
		if _, err := tx.Exec(ctx, `DELETE FROM container_state WHERE agent_id=$1`, agentID); err != nil {
			return err
		}
		for _, c := range snap.Containers {
			if _, err := tx.Exec(ctx, `INSERT INTO container_state
				(agent_id, compose_project, service, container_id, image, revision, state, health, started_at, restart_count)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
				ON CONFLICT (agent_id, compose_project, service) DO UPDATE SET container_id=EXCLUDED.container_id,
				image=EXCLUDED.image, revision=EXCLUDED.revision, state=EXCLUDED.state, health=EXCLUDED.health,
				started_at=EXCLUDED.started_at, restart_count=EXCLUDED.restart_count, updated_at=now()`,
				agentID, c.ComposeProject, c.Service, c.ID, c.Image, c.Revision, c.State, c.Health, c.StartedAt, c.RestartCount); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

// AddEvent stores a container event. Ordering uses server receive time.
func (s *Store) AddEvent(ctx context.Context, agentID string, e protocol.Event) error {
	kind := e.Kind
	if e.Kind == protocol.EventHealth {
		kind = "health:" + e.Detail
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO container_events (agent_id, container, event, agent_ts, detail)
		VALUES ($1,$2,$3,$4,$5)`, agentID, e.ComposeProject+"/"+e.Service, kind, e.At, exitDetail(e))
	return err
}

func exitDetail(e protocol.Event) *string {
	if e.ExitCode == nil {
		return nil
	}
	s := "exit " + strconv.Itoa(*e.ExitCode)
	return &s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ContainerStateRow is the last snapshot row for a service.
type ContainerStateRow struct {
	ContainerID  string     `json:"container_id"`
	Image        string     `json:"image"`
	Revision     string     `json:"revision"`
	State        string     `json:"state"`
	Health       string     `json:"health"`
	StartedAt    *time.Time `json:"started_at"`
	RestartCount int        `json:"restart_count"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// ContainerFor returns the stored state of a service, or ErrNotFound.
func (s *Store) ContainerFor(ctx context.Context, agentID, project, service string) (ContainerStateRow, error) {
	var c ContainerStateRow
	err := s.Pool.QueryRow(ctx, `SELECT container_id, COALESCE(image,''), COALESCE(revision,''), COALESCE(state,''),
		COALESCE(health,''), started_at, restart_count, updated_at FROM container_state
		WHERE agent_id=$1 AND compose_project=$2 AND service=$3`, agentID, project, service).
		Scan(&c.ContainerID, &c.Image, &c.Revision, &c.State, &c.Health, &c.StartedAt, &c.RestartCount, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// EventRow is one entry of the restart history.
type EventRow struct {
	At      time.Time  `json:"at"`
	AgentTS *time.Time `json:"agent_ts"`
	Event   string     `json:"event"`
	Detail  *string    `json:"detail"`
}

// RecentEvents returns events newest first.
func (s *Store) RecentEvents(ctx context.Context, agentID, project, service string, since time.Time, limit int) ([]EventRow, error) {
	rows, err := s.Pool.Query(ctx, `SELECT at, agent_ts, event, detail FROM container_events
		WHERE agent_id=$1 AND container=$2 AND at >= $3 ORDER BY at DESC, id DESC LIMIT $4`,
		agentID, project+"/"+service, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventRow{}
	for rows.Next() {
		var e EventRow
		if err := rows.Scan(&e.At, &e.AgentTS, &e.Event, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountEvents counts events of a kind since a time (crash loop detection).
func (s *Store) CountEvents(ctx context.Context, agentID, project, service, kind string, since time.Time) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM container_events
		WHERE agent_id=$1 AND container=$2 AND event=$3 AND at >= $4`, agentID, project+"/"+service, kind, since).Scan(&n)
	return n, err
}

// HealthRow is one stored probe.
type HealthRow struct {
	Status    string    `json:"status"` // ok | slow | fail
	HTTPCode  int       `json:"http_code"`
	LatencyMS int       `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
}

// AddHealth stores a probe result.
func (s *Store) AddHealth(ctx context.Context, envID int64, status string, code, latencyMS int) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO health_checks (app_env_id, status, http_code, latency_ms) VALUES ($1,$2,$3,$4)`,
		envID, status, code, latencyMS)
	return err
}

// RecentHealth returns the latest probes, newest first.
func (s *Store) RecentHealth(ctx context.Context, envID int64, n int) ([]HealthRow, error) {
	rows, err := s.Pool.Query(ctx, `SELECT status, COALESCE(http_code,0), COALESCE(latency_ms,0), checked_at
		FROM health_checks WHERE app_env_id=$1 ORDER BY checked_at DESC, id DESC LIMIT $2`, envID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HealthRow{}
	for rows.Next() {
		var h HealthRow
		if err := rows.Scan(&h.Status, &h.HTTPCode, &h.LatencyMS, &h.CheckedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Retention windows. Audit events are kept one year and are removed only by the audit_purge
// function (see migration 00004), never by the server.
const (
	RetainEvents  = 14 * 24 * time.Hour
	RetainHealth  = 24 * time.Hour
	RetainRunLogs = 30 * 24 * time.Hour
)

// Prune applies the retention windows: container events 14 days, health checks 24 hours,
// stored run logs 30 days (the run rows stay, only their captured logs go).
func (s *Store) Prune(ctx context.Context, now time.Time) error {
	if _, err := s.Pool.Exec(ctx, `DELETE FROM container_events WHERE at < $1`, now.Add(-RetainEvents)); err != nil {
		return err
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM health_checks WHERE checked_at < $1`, now.Add(-RetainHealth)); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `DELETE FROM action_run_logs WHERE run_id IN
		(SELECT id FROM action_runs WHERE finished_at IS NOT NULL AND finished_at < $1)`, now.Add(-RetainRunLogs))
	return err
}

// EnvAgent returns the agent id of a live env (used before talking to an agent).
func (s *Store) EnvByName(ctx context.Context, appID, env string) (EnvRow, error) {
	var e EnvRow
	err := s.Pool.QueryRow(ctx, `SELECT e.id, e.env, e.tier, e.agent_id, e.spec FROM app_envs e JOIN apps a ON a.id=e.app_id
		WHERE e.app_id=$1 AND e.env=$2 AND e.deleted_at IS NULL AND a.deleted_at IS NULL`, appID, env).
		Scan(&e.ID, &e.Env, &e.Tier, &e.Agent, &e.Spec)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}
