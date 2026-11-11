package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Role is a named permission set.
type Role struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// ListRoles returns all roles.
func (s *Store) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, name, permissions FROM roles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Name, &r.Permissions); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Binding is a role granted to a user or team on an app and env.
type Binding struct {
	ID          int64     `json:"id"`
	SubjectType string    `json:"subject_type"`
	Subject     string    `json:"subject"`
	SubjectName string    `json:"subject_name"`
	RoleID      int64     `json:"role_id"`
	RoleName    string    `json:"role_name"`
	AppID       string    `json:"app_id"`
	Env         string    `json:"env"`
	CreatedBy   *int64    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListBindings returns all bindings, resolving user subjects to logins.
func (s *Store) ListBindings(ctx context.Context) ([]Binding, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT b.id, b.subject_type, b.subject,
		       CASE WHEN b.subject_type='user' THEN COALESCE(u.login, b.subject) ELSE b.subject END,
		       b.role_id, r.name, b.app_id, b.env, b.created_by, b.created_at
		FROM role_bindings b JOIN roles r ON r.id=b.role_id
		LEFT JOIN users u ON b.subject_type='user' AND u.id::text=b.subject
		ORDER BY b.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Binding{}
	for rows.Next() {
		var b Binding
		if err := rows.Scan(&b.ID, &b.SubjectType, &b.Subject, &b.SubjectName, &b.RoleID, &b.RoleName, &b.AppID, &b.Env, &b.CreatedBy, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// CreateBinding inserts a binding. subject for type user is the user's id.
func (s *Store) CreateBinding(ctx context.Context, b Binding, createdBy int64) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO role_bindings (subject_type, subject, role_id, app_id, env, created_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (subject_type, subject, role_id, app_id, env) DO UPDATE SET subject=EXCLUDED.subject
		RETURNING id`, b.SubjectType, b.Subject, b.RoleID, b.AppID, b.Env, createdBy).Scan(&id)
	return id, err
}

// DeleteBinding removes a binding.
func (s *Store) DeleteBinding(ctx context.Context, id int64) error {
	t, err := s.Pool.Exec(ctx, `DELETE FROM role_bindings WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if t.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UserExists reports whether a user id exists.
func (s *Store) UserExists(ctx context.Context, id int64) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, id).Scan(&ok)
	return ok, err
}

// AuditRow is one audit event for display.
type AuditRow struct {
	ID        int64           `json:"id"`
	At        time.Time       `json:"at"`
	ActorType string          `json:"actor_type"`
	ActorID   string          `json:"actor_id"`
	Action    string          `json:"action"`
	Target    *string         `json:"target"`
	Details   json.RawMessage `json:"details"`
	Result    string          `json:"result"`
	IP        *string         `json:"ip"`
}

// ListAudit returns events newest first. before (0 = none) pages by id.
func (s *Store) ListAudit(ctx context.Context, limit int, before int64) ([]AuditRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, at, actor_type, actor_id, action, target, details, result, ip FROM audit_events `
	args := []any{limit}
	if before > 0 {
		q += `WHERE id < $2 `
		args = append(args, before)
	}
	q += `ORDER BY id DESC LIMIT $1`
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditRow{}
	for rows.Next() {
		var a AuditRow
		if err := rows.Scan(&a.ID, &a.At, &a.ActorType, &a.ActorID, &a.Action, &a.Target, &a.Details, &a.Result, &a.IP); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

var _ = pgx.ErrNoRows
