package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Integration is a stored optional integration.
type Integration struct {
	Kind           string
	Enabled        bool
	Config         json.RawMessage
	SecretEnc      []byte
	UpdatedBy      string
	UpdatedAt      time.Time
	LastCheckAt    *time.Time
	LastCheckOK    *bool
	LastCheckError string
}

// GetIntegration returns the stored row, or ErrNotFound.
func (s *Store) GetIntegration(ctx context.Context, kind string) (Integration, error) {
	var i Integration
	var by *string
	var lerr *string
	err := s.Pool.QueryRow(ctx, `
		SELECT i.kind, i.enabled, i.config, i.secret_enc, u.login, i.updated_at, i.last_check_at, i.last_check_ok, i.last_check_error
		FROM integrations i LEFT JOIN users u ON u.id = i.updated_by WHERE i.kind = $1`, kind).
		Scan(&i.Kind, &i.Enabled, &i.Config, &i.SecretEnc, &by, &i.UpdatedAt, &i.LastCheckAt, &i.LastCheckOK, &lerr)
	if errors.Is(err, pgx.ErrNoRows) {
		return i, ErrNotFound
	}
	if by != nil {
		i.UpdatedBy = *by
	}
	if lerr != nil {
		i.LastCheckError = *lerr
	}
	return i, err
}

// SaveIntegration creates or replaces an integration's settings.
func (s *Store) SaveIntegration(ctx context.Context, kind string, enabled bool, config json.RawMessage, secretEnc []byte, userID int64, checkOK *bool, checkErr string) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO integrations (kind, enabled, config, secret_enc, updated_by, updated_at, last_check_at, last_check_ok, last_check_error)
		VALUES ($1, $2, $3, $4, $5, now(), CASE WHEN $6::boolean IS NULL THEN NULL ELSE now() END, $6, NULLIF($7, ''))
		ON CONFLICT (kind) DO UPDATE SET enabled = EXCLUDED.enabled, config = EXCLUDED.config, secret_enc = EXCLUDED.secret_enc,
		  updated_by = EXCLUDED.updated_by, updated_at = now(), last_check_at = EXCLUDED.last_check_at,
		  last_check_ok = EXCLUDED.last_check_ok, last_check_error = EXCLUDED.last_check_error`,
		kind, enabled, config, secretEnc, userID, checkOK, checkErr)
	return err
}

// DeleteIntegration removes an integration's settings.
func (s *Store) DeleteIntegration(ctx context.Context, kind string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM integrations WHERE kind = $1`, kind)
	return err
}
