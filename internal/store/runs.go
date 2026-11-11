package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrActiveRun means the environment already has an active run (the database's partial
// unique index fired).
var ErrActiveRun = errors.New("this environment already has an active run")

// ActiveStatuses are the states in which a run still occupies its environment and is polled.
var activePolled = []string{"dispatching", "queued", "in_progress", "waiting"}

// ActionRun is a migration or seed request and its GitHub workflow run.
type ActionRun struct {
	ID               string          `json:"id"`
	CorrelationID    string          `json:"correlation_id"`
	App              string          `json:"app"`
	Env              string          `json:"env"`
	Tier             string          `json:"tier"`
	AppEnvID         int64           `json:"-"`
	Action           string          `json:"action"`
	Status           string          `json:"status"`
	Conclusion       *string         `json:"conclusion"`
	RequestedBy      int64           `json:"-"`
	RequestedByLogin string          `json:"requested_by"`
	ApprovedByLogin  *string         `json:"approved_by"`
	RejectedByLogin  *string         `json:"rejected_by"`
	GHRunID          *int64          `json:"gh_run_id"`
	GHRunURL         *string         `json:"gh_run_url"`
	Repo             string          `json:"repo"`
	Workflow         string          `json:"workflow"`
	Ref              *string         `json:"ref"`
	Warning          *string         `json:"warning"`
	Error            *string         `json:"error"`
	Jobs             json.RawMessage `json:"jobs"`
	CancelRequested  bool            `json:"cancel_requested"`
	RerunOf          *string         `json:"rerun_of"`
	CreatedAt        time.Time       `json:"created_at"`
	DecidedAt        *time.Time      `json:"decided_at"`
	DispatchedAt     *time.Time      `json:"dispatched_at"`
	FinishedAt       *time.Time      `json:"finished_at"`
	NextPollAt       time.Time       `json:"-"`
}

const runSelect = `
	SELECT r.id::text, r.correlation_id::text, a.id, e.env, e.tier, r.app_env_id, r.action, r.status, r.conclusion,
	       r.requested_by, ru.login, au.login, xu.login, r.gh_run_id, r.gh_run_url,
	       COALESCE(r.repo,''), COALESCE(r.workflow,''), r.ref, r.warning, r.error, r.jobs, r.cancel_requested,
	       r.rerun_of::text, r.created_at, r.decided_at, r.dispatched_at, r.finished_at, r.next_poll_at
	FROM action_runs r
	JOIN app_envs e ON e.id=r.app_env_id JOIN apps a ON a.id=e.app_id
	JOIN users ru ON ru.id=r.requested_by
	LEFT JOIN users au ON au.id=r.approved_by
	LEFT JOIN users xu ON xu.id=r.rejected_by `

func scanRun(row pgx.Row) (ActionRun, error) {
	var r ActionRun
	err := row.Scan(&r.ID, &r.CorrelationID, &r.App, &r.Env, &r.Tier, &r.AppEnvID, &r.Action, &r.Status, &r.Conclusion,
		&r.RequestedBy, &r.RequestedByLogin, &r.ApprovedByLogin, &r.RejectedByLogin, &r.GHRunID, &r.GHRunURL,
		&r.Repo, &r.Workflow, &r.Ref, &r.Warning, &r.Error, &r.Jobs, &r.CancelRequested,
		&r.RerunOf, &r.CreatedAt, &r.DecidedAt, &r.DispatchedAt, &r.FinishedAt, &r.NextPollAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) queryRuns(ctx context.Context, where string, args ...any) ([]ActionRun, error) {
	rows, err := s.Pool.Query(ctx, runSelect+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActionRun{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// NewRun is the input of CreateRun.
type NewRun struct {
	ID, CorrelationID string
	AppEnvID          int64
	Action            string
	Inputs            map[string]string
	Status            string // pending_approval or dispatching
	RequestedBy       int64
	Repo, Workflow    string
	RerunOf           string
}

// CreateRun inserts a run. It returns ErrActiveRun when the env already has one.
func (s *Store) CreateRun(ctx context.Context, n NewRun) error {
	in, _ := json.Marshal(n.Inputs)
	var rerun any
	if n.RerunOf != "" {
		rerun = n.RerunOf
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO action_runs (id, correlation_id, requested_by, app_env_id, action, inputs, status, repo, workflow, rerun_of)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		n.ID, n.CorrelationID, n.RequestedBy, n.AppEnvID, n.Action, in, n.Status, n.Repo, n.Workflow, rerun)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "action_runs_one_active_per_env" {
		return ErrActiveRun
	}
	return err
}

// GetRun loads a run by id.
func (s *Store) GetRun(ctx context.Context, id string) (ActionRun, error) {
	rs, err := s.queryRuns(ctx, ` WHERE r.id::text = $1`, id)
	if err != nil {
		return ActionRun{}, err
	}
	if len(rs) == 0 {
		return ActionRun{}, ErrNotFound
	}
	return rs[0], nil
}

// ListRuns returns the newest runs of an env.
func (s *Store) ListRuns(ctx context.Context, appEnvID int64, limit int) ([]ActionRun, error) {
	return s.queryRuns(ctx, ` WHERE r.app_env_id=$1 ORDER BY r.created_at DESC LIMIT $2`, appEnvID, limit)
}

// PendingApprovals returns every run waiting for an approver (the handler filters by access).
func (s *Store) PendingApprovals(ctx context.Context) ([]ActionRun, error) {
	return s.queryRuns(ctx, ` WHERE r.status='pending_approval' ORDER BY r.created_at`)
}

// RecentRunBy finds a still-active run of the same action on the same env by the same user
// since t (double click dedupe). A rejected or failed run never blocks a fresh request.
func (s *Store) RecentRunBy(ctx context.Context, appEnvID, userID int64, action string, since time.Time) (ActionRun, error) {
	rs, err := s.queryRuns(ctx, ` WHERE r.app_env_id=$1 AND r.requested_by=$2 AND r.action=$3 AND r.created_at >= $4
		AND r.rerun_of IS NULL AND r.status IN ('pending_approval','dispatching','queued','in_progress','waiting') ORDER BY r.created_at DESC LIMIT 1`, appEnvID, userID, action, since)
	if err != nil {
		return ActionRun{}, err
	}
	if len(rs) == 0 {
		return ActionRun{}, ErrNotFound
	}
	return rs[0], nil
}

// DuePolls returns active runs whose next poll time has come.
func (s *Store) DuePolls(ctx context.Context, now time.Time, limit int) ([]ActionRun, error) {
	return s.queryRuns(ctx, ` WHERE r.status = ANY($1) AND r.next_poll_at <= $2 ORDER BY r.next_poll_at LIMIT $3`,
		activePolled, now, limit)
}

// ActiveRunForEnv returns the run occupying an env, if any.
func (s *Store) ActiveRunForEnv(ctx context.Context, appEnvID int64) (ActionRun, error) {
	rs, err := s.queryRuns(ctx, ` WHERE r.app_env_id=$1 AND r.status IN ('pending_approval','dispatching','queued','in_progress','waiting') LIMIT 1`, appEnvID)
	if err != nil {
		return ActionRun{}, err
	}
	if len(rs) == 0 {
		return ActionRun{}, ErrNotFound
	}
	return rs[0], nil
}

// Approve moves pending_approval to dispatching. It reports false when the run was no
// longer pending (someone else decided first).
func (s *Store) Approve(ctx context.Context, id string, approver int64) (bool, error) {
	t, err := s.Pool.Exec(ctx, `UPDATE action_runs SET status='dispatching', approved_by=$2, decided_at=now(), next_poll_at=now()
		WHERE id::text=$1 AND status='pending_approval'`, id, approver)
	return t.RowsAffected() == 1, err
}

// Reject moves pending_approval to rejected.
func (s *Store) Reject(ctx context.Context, id string, by int64, reason string) (bool, error) {
	t, err := s.Pool.Exec(ctx, `UPDATE action_runs SET status='rejected', rejected_by=$2, decided_at=now(), finished_at=now(), error=$3
		WHERE id::text=$1 AND status='pending_approval'`, id, by, nullIfEmpty(reason))
	return t.RowsAffected() == 1, err
}

// MarkDispatched records the ref and dispatch time and schedules correlation polling.
func (s *Store) MarkDispatched(ctx context.Context, id, ref, warning string, at time.Time, next time.Time) error {
	_, err := s.Pool.Exec(ctx, `UPDATE action_runs SET ref=$2, warning=$3, dispatched_at=$4, next_poll_at=$5 WHERE id::text=$1`,
		id, ref, nullIfEmpty(warning), at, next)
	return err
}

// Progress is a polled update of a run.
type Progress struct {
	Status     string
	GHRunID    *int64
	GHRunURL   *string
	Conclusion *string
	Jobs       json.RawMessage
	Error      *string
	Finished   bool
	NextPoll   time.Time
}

// SetProgress writes a poll result. A run already in a final state is never reopened.
func (s *Store) SetProgress(ctx context.Context, id string, p Progress) error {
	jobs := p.Jobs
	if jobs == nil {
		jobs = json.RawMessage("[]")
	}
	_, err := s.Pool.Exec(ctx, `UPDATE action_runs SET status=$2, gh_run_id=COALESCE($3, gh_run_id), gh_run_url=COALESCE($4, gh_run_url),
		conclusion=COALESCE($5, conclusion), jobs=CASE WHEN $6::jsonb = '[]'::jsonb THEN jobs ELSE $6::jsonb END,
		error=COALESCE($7, error), finished_at=CASE WHEN $8 THEN now() ELSE finished_at END, next_poll_at=$9
		WHERE id::text=$1 AND status IN ('dispatching','queued','in_progress','waiting','unknown')`,
		id, p.Status, p.GHRunID, p.GHRunURL, p.Conclusion, jobs, p.Error, p.Finished, p.NextPoll)
	return err
}

// SetNextPoll delays the next poll of a run (used for rate-limit back-off).
func (s *Store) SetNextPoll(ctx context.Context, id string, at time.Time) error {
	_, err := s.Pool.Exec(ctx, `UPDATE action_runs SET next_poll_at=$2 WHERE id::text=$1`, id, at)
	return err
}

// Fail ends a run that never reached GitHub or could not be tracked.
func (s *Store) Fail(ctx context.Context, id, msg string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE action_runs SET status='failed', error=$2, finished_at=now()
		WHERE id::text=$1 AND status IN ('dispatching','pending_approval','queued','in_progress','waiting','unknown')`, id, msg)
	return err
}

// MarkUnknown ends tracking when the run never appeared. The workflow link stays usable.
func (s *Store) MarkUnknown(ctx context.Context, id, msg string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE action_runs SET status='unknown', error=$2, finished_at=now()
		WHERE id::text=$1 AND status='dispatching'`, id, msg)
	return err
}

// RequestCancel flags a run so the poller reports it cancelled.
func (s *Store) RequestCancel(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE action_runs SET cancel_requested=true, next_poll_at=now() WHERE id::text=$1`, id)
	return err
}

// RunLog is the captured log of one job.
type RunLog struct {
	JobID     int64  `json:"job_id"`
	JobName   string `json:"job_name"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

// SaveLogs stores job logs for a finished run (idempotent).
func (s *Store) SaveLogs(ctx context.Context, runID string, logs []RunLog) error {
	for _, l := range logs {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO action_run_logs (run_id, job_id, job_name, content, truncated)
			VALUES ($1::uuid,$2,$3,$4,$5) ON CONFLICT (run_id, job_id) DO UPDATE SET content=EXCLUDED.content, truncated=EXCLUDED.truncated`,
			runID, l.JobID, l.JobName, l.Content, l.Truncated); err != nil {
			return err
		}
	}
	return nil
}

// RunLogs returns the stored job logs of a run.
func (s *Store) RunLogs(ctx context.Context, runID string) ([]RunLog, error) {
	rows, err := s.Pool.Query(ctx, `SELECT job_id, job_name, content, truncated FROM action_run_logs WHERE run_id::text=$1 ORDER BY job_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RunLog{}
	for rows.Next() {
		var l RunLog
		if err := rows.Scan(&l.JobID, &l.JobName, &l.Content, &l.Truncated); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ClaimDispatch lets exactly one caller (the request handler, the approver or the poller)
// perform the GitHub dispatch call for a run.
func (s *Store) ClaimDispatch(ctx context.Context, id string) (bool, error) {
	t, err := s.Pool.Exec(ctx, `UPDATE action_runs SET dispatched_at=now(), next_poll_at=now()
		WHERE id::text=$1 AND status='dispatching' AND dispatched_at IS NULL`, id)
	return t.RowsAffected() == 1, err
}
