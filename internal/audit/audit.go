// Package audit writes the append-only audit log. Log text from containers is never
// written here.
package audit

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Actor types.
const (
	ActorUser   = "user"
	ActorAgent  = "agent"
	ActorSystem = "system"
)

// Event is one audit record.
type Event struct {
	ActorType string
	ActorID   string
	Action    string
	Target    string
	Details   map[string]any
	Result    string // ok | denied | error
	IP        string
}

// Writer inserts audit events.
type Writer struct{ Pool *pgxpool.Pool }

// Write records an event. Failure to audit is returned so callers can decide
// whether the action may continue.
func (w *Writer) Write(ctx context.Context, e Event) error {
	d := e.Details
	if d == nil {
		d = map[string]any{}
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = w.Pool.Exec(ctx,
		`INSERT INTO audit_events (actor_type, actor_id, action, target, details, result, ip)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		e.ActorType, e.ActorID, e.Action, e.Target, b, e.Result, e.IP)
	return err
}
