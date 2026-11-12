package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// CustomApp is an external tool shown on the Apps page.
type CustomApp struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ListCustomApps returns every tool, by name.
func (s *Store) ListCustomApps(ctx context.Context) ([]CustomApp, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, name, url, description, tags, updated_at FROM custom_apps ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomApp{}
	for rows.Next() {
		var c CustomApp
		if err := rows.Scan(&c.ID, &c.Name, &c.URL, &c.Description, &c.Tags, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ErrDuplicateName means another tool already has the name.
var ErrDuplicateName = errors.New("duplicate name")

func dupName(err error) bool { return err != nil && strings.Contains(err.Error(), "custom_apps_name") }

// CreateCustomApp adds a tool.
func (s *Store) CreateCustomApp(ctx context.Context, c CustomApp, by int64) (CustomApp, error) {
	err := s.Pool.QueryRow(ctx, `INSERT INTO custom_apps (name, url, description, tags, created_by) VALUES ($1,$2,$3,$4,$5)
		RETURNING id, updated_at`, c.Name, c.URL, c.Description, c.Tags, by).Scan(&c.ID, &c.UpdatedAt)
	if dupName(err) {
		return c, ErrDuplicateName
	}
	return c, err
}

// UpdateCustomApp changes a tool.
func (s *Store) UpdateCustomApp(ctx context.Context, c CustomApp) (CustomApp, error) {
	err := s.Pool.QueryRow(ctx, `UPDATE custom_apps SET name=$2, url=$3, description=$4, tags=$5, updated_at=now() WHERE id=$1 RETURNING updated_at`,
		c.ID, c.Name, c.URL, c.Description, c.Tags).Scan(&c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if dupName(err) {
		return c, ErrDuplicateName
	}
	return c, err
}

// DeleteCustomApp removes a tool.
func (s *Store) DeleteCustomApp(ctx context.Context, id int64) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM custom_apps WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
