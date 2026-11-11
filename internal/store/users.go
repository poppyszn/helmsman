package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/poppyszn/helmsman/internal/rbac"
)

// User is an IDP account, keyed on github_id.
type User struct {
	ID          int64      `json:"id"`
	GitHubID    int64      `json:"github_id"`
	Login       string     `json:"login"`
	Name        *string    `json:"name"`
	Email       *string    `json:"email"`
	AvatarURL   *string    `json:"avatar_url"`
	IsAdmin     bool       `json:"is_admin"`
	DisabledAt  *time.Time `json:"disabled_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

const userCols = `id, github_id, login, name, email, avatar_url, is_admin, disabled_at, last_login_at`

func scanUser(row pgx.Row, u *User) error {
	return row.Scan(&u.ID, &u.GitHubID, &u.Login, &u.Name, &u.Email, &u.AvatarURL, &u.IsAdmin, &u.DisabledAt, &u.LastLoginAt)
}

// LoginInfo is what GitHub told us at login.
type LoginInfo struct {
	GitHubID  int64
	Login     string
	Name      string
	Email     string // may be empty
	AvatarURL string
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// UpsertLogin creates or updates the user keyed on github_id. Bootstrap admins are
// promoted on their first login only.
func (s *Store) UpsertLogin(ctx context.Context, in LoginInfo, tokenEnc []byte, bootstrapAdmin bool) (User, error) {
	var u User
	err := scanUser(s.Pool.QueryRow(ctx, `
		INSERT INTO users (github_id, login, name, email, avatar_url, is_admin, last_login_at, gh_token_enc)
		VALUES ($1,$2,$3,$4,$5,$6, now(), $7)
		ON CONFLICT (github_id) DO UPDATE SET
		  login = EXCLUDED.login, name = EXCLUDED.name, email = EXCLUDED.email,
		  avatar_url = EXCLUDED.avatar_url, gh_token_enc = EXCLUDED.gh_token_enc,
		  is_admin = users.is_admin OR (EXCLUDED.is_admin AND users.last_login_at IS NULL),
		  last_login_at = now()
		RETURNING `+userCols,
		in.GitHubID, in.Login, nullable(in.Name), nullable(in.Email), nullable(in.AvatarURL), bootstrapAdmin, tokenEnc), &u)
	return u, err
}

// ReplaceUserOrgs stores org membership. teams hold "org/slug" values.
func (s *Store) ReplaceUserOrgs(ctx context.Context, userID int64, orgTeams map[string][]string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM user_orgs WHERE user_id=$1`, userID); err != nil {
		return err
	}
	for org, teams := range orgTeams {
		if teams == nil {
			teams = []string{}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_orgs (user_id, org, teams) VALUES ($1,$2,$3)`, userID, org, teams); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// CreateSession stores a session by hash.
func (s *Store) CreateSession(ctx context.Context, hash []byte, userID int64, ttl time.Duration, ip, ua string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO sessions (id_hash, user_id, expires_at, ip, user_agent) VALUES ($1,$2, now()+$3::interval, $4,$5)`,
		hash, userID, ttl.String(), ip, ua)
	return err
}

// ErrNoSession means the session is missing, expired, revoked, or the user is disabled.
var ErrNoSession = errors.New("no valid session")

// SessionUser resolves a live session to its user.
func (s *Store) SessionUser(ctx context.Context, hash []byte) (User, error) {
	var u User
	err := scanUser(s.Pool.QueryRow(ctx, `
		SELECT u.id, u.github_id, u.login, u.name, u.email, u.avatar_url, u.is_admin, u.disabled_at, u.last_login_at
		FROM sessions x JOIN users u ON u.id = x.user_id
		WHERE x.id_hash=$1 AND x.revoked_at IS NULL AND x.expires_at > now() AND u.disabled_at IS NULL`, hash), &u)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNoSession
	}
	return u, err
}

// RevokeSession revokes one session.
func (s *Store) RevokeSession(ctx context.Context, hash []byte) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id_hash=$1 AND revoked_at IS NULL`, hash)
	return err
}

// RevokeUserSessions revokes every live session of a user and returns how many.
func (s *Store) RevokeUserSessions(ctx context.Context, userID int64) (int64, error) {
	t, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return t.RowsAffected(), err
}

// RefreshTarget is a user with a live session whose membership must be re-checked.
type RefreshTarget struct {
	UserID   int64
	Login    string
	TokenEnc []byte
}

// RefreshTargets lists users with at least one live session and a stored token.
func (s *Store) RefreshTargets(ctx context.Context) ([]RefreshTarget, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT u.id, u.login, u.gh_token_enc FROM users u
		JOIN sessions x ON x.user_id=u.id AND x.revoked_at IS NULL AND x.expires_at > now()
		WHERE u.disabled_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RefreshTarget
	for rows.Next() {
		var t RefreshTarget
		if err := rows.Scan(&t.UserID, &t.Login, &t.TokenEnc); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListUsers returns all users with their org memberships.
func (s *Store) ListUsers(ctx context.Context) ([]UserWithOrgs, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+userCols+`,
		COALESCE((SELECT array_agg(org ORDER BY org) FROM user_orgs o WHERE o.user_id=users.id), '{}')
		FROM users ORDER BY login`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserWithOrgs{}
	for rows.Next() {
		var u UserWithOrgs
		if err := rows.Scan(&u.ID, &u.GitHubID, &u.Login, &u.Name, &u.Email, &u.AvatarURL, &u.IsAdmin, &u.DisabledAt, &u.LastLoginAt, &u.Orgs); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserWithOrgs adds org names to User.
type UserWithOrgs struct {
	User
	Orgs []string `json:"orgs"`
}

// Principal loads the user's bindings: direct ones plus those of their org/team memberships.
func (s *Store) Principal(ctx context.Context, u User) (rbac.Principal, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT r.permissions, b.app_id, b.env FROM role_bindings b JOIN roles r ON r.id=b.role_id
		WHERE (b.subject_type='user' AND b.subject = $1)
		   OR (b.subject_type='team' AND b.subject IN
		        (SELECT unnest(teams) FROM user_orgs WHERE user_id=$2))`, strconv.FormatInt(u.ID, 10), u.ID)
	if err != nil {
		return rbac.Principal{}, err
	}
	defer rows.Close()
	p := rbac.Principal{IsAdmin: u.IsAdmin}
	for rows.Next() {
		var b rbac.Binding
		if err := rows.Scan(&b.Permissions, &b.AppID, &b.Env); err != nil {
			return p, err
		}
		p.Bindings = append(p.Bindings, b)
	}
	return p, rows.Err()
}
