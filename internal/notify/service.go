package notify

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/poppyszn/helmsman/internal/store"
)

// maxAttempts is how many times a delivery is tried before it is marked failed.
const maxAttempts = 4

var backoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// Service stores events as deliveries and sends them.
type Service struct {
	St      *store.Store
	EncKey  []byte
	BaseURL string // the portal's public address, for links in messages
	Log     *slog.Logger
	// Mail and Hook send one message. Tests replace them.
	Mail MailSender
	Hook WebhookSender
	Now  func() time.Time
}

// New builds a service with the real senders.
func New(st *store.Store, encKey []byte, baseURL string, log *slog.Logger) *Service {
	s := &Service{St: st, EncKey: encKey, BaseURL: baseURL, Log: log}
	s.Mail = &smtpMailer{svc: s}
	s.Hook = &httpWebhook{}
	return s
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Emit stores a delivery for every enabled channel that wants the event. It never blocks on a receiver and
// never fails the caller: a problem is logged.
func (s *Service) Emit(ctx context.Context, ev Event) {
	if s == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = s.now()
	}
	if ev.Key == "" {
		ev.Key = ev.Type + ":" + ev.App + "/" + ev.Env
	}
	ctx = context.WithoutCancel(ctx)
	rows, err := s.St.Pool.Query(ctx, `SELECT id, events, tier, apps, envs FROM notification_channels WHERE enabled`)
	if err != nil {
		s.Log.Error("notify: list channels", "err", err)
		return
	}
	type target struct {
		id     int64
		events []string
		tier   string
		apps   []string
		envs   []string
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.events, &t.tier, &t.apps, &t.envs); err == nil {
			targets = append(targets, t)
		}
	}
	rows.Close()
	payload, _ := json.Marshal(ev)
	for _, t := range targets {
		if !Matches(t.events, Scope{Tier: t.tier, Apps: t.apps, Envs: t.envs}, ev) {
			continue
		}
		// The same thing is never announced twice to a channel, and a flapping environment is held back.
		window := time.Duration(0)
		if flappy(ev.Type) {
			window = cooldown
		}
		var seen bool
		err := s.St.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM notification_deliveries WHERE channel_id = $1 AND dedupe_key = $2
			AND ($3::interval = '0' OR created_at > now() - $3::interval))`, t.id, ev.Key, window.String()).Scan(&seen)
		if err != nil || seen {
			continue
		}
		if _, err := s.St.Pool.Exec(ctx, `INSERT INTO notification_deliveries (channel_id, event_type, dedupe_key, title, payload)
			VALUES ($1,$2,$3,$4,$5)`, t.id, ev.Type, ev.Key, ev.Title, payload); err != nil {
			s.Log.Error("notify: store delivery", "err", err)
		}
	}
}

// Delivery is one row of the delivery log.
type Delivery struct {
	ID        int64      `json:"id"`
	Channel   string     `json:"channel"`
	Event     string     `json:"event"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"last_error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	SentAt    *time.Time `json:"sent_at,omitempty"`
}

// Deliveries returns the most recent deliveries, newest first.
func (s *Service) Deliveries(ctx context.Context, limit int) ([]Delivery, error) {
	rows, err := s.St.Pool.Query(ctx, `SELECT d.id, c.name, d.event_type, d.title, d.status, d.attempts, d.last_error, d.created_at, d.sent_at
		FROM notification_deliveries d JOIN notification_channels c ON c.id = d.channel_id ORDER BY d.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		var le *string
		if err := rows.Scan(&d.ID, &d.Channel, &d.Event, &d.Title, &d.Status, &d.Attempts, &le, &d.CreatedAt, &d.SentAt); err != nil {
			return nil, err
		}
		if le != nil {
			d.LastError = *le
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Run sends due deliveries until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.SendDue(ctx)
		case <-prune.C:
			_, _ = s.St.Pool.Exec(ctx, `DELETE FROM notification_deliveries WHERE created_at < now() - interval '30 days'`)
		}
	}
}

// SendDue claims due deliveries (a lease, so two servers never send the same one) and sends them.
func (s *Service) SendDue(ctx context.Context) {
	rows, err := s.St.Pool.Query(ctx, `
		UPDATE notification_deliveries SET attempts = attempts + 1, next_attempt_at = now() + interval '2 minutes'
		WHERE id IN (SELECT id FROM notification_deliveries WHERE status = 'pending' AND next_attempt_at <= now()
		             ORDER BY id LIMIT 10 FOR UPDATE SKIP LOCKED)
		RETURNING id, channel_id, payload, attempts`)
	if err != nil {
		s.Log.Error("notify: claim deliveries", "err", err)
		return
	}
	type job struct {
		id, channel int64
		payload     []byte
		attempts    int
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.channel, &j.payload, &j.attempts); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	for _, j := range jobs {
		var ev Event
		_ = json.Unmarshal(j.payload, &ev)
		err := s.send(ctx, j.channel, ev)
		s.finish(ctx, j.id, j.attempts, err)
	}
}

func (s *Service) finish(ctx context.Context, id int64, attempts int, sendErr error) {
	if sendErr == nil {
		_, _ = s.St.Pool.Exec(ctx, `UPDATE notification_deliveries SET status = 'sent', sent_at = now(), last_error = NULL WHERE id = $1`, id)
		return
	}
	msg := trim(sendErr.Error(), 300)
	if attempts >= maxAttempts {
		_, _ = s.St.Pool.Exec(ctx, `UPDATE notification_deliveries SET status = 'failed', last_error = $2 WHERE id = $1`, id, msg)
		return
	}
	wait := backoff[min(attempts-1, len(backoff)-1)]
	_, _ = s.St.Pool.Exec(ctx, `UPDATE notification_deliveries SET last_error = $2, next_attempt_at = now() + $3::interval WHERE id = $1`, id, msg, wait.String())
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

var errChannelGone = errors.New("the channel was deleted")

// send delivers one event to one channel.
func (s *Service) send(ctx context.Context, channelID int64, ev Event) error {
	c, err := s.Channel(ctx, channelID)
	if err != nil {
		return errChannelGone
	}
	return s.deliver(ctx, c, ev)
}

func (s *Service) deliver(ctx context.Context, c Channel, ev Event) error {
	switch c.Kind {
	case "email":
		subject, body := Text(ev, s.BaseURL)
		return s.Mail.Send(ctx, c.To, subject, body)
	case "webhook":
		secret, err := s.secretOf(ctx, c.ID)
		if err != nil {
			return err
		}
		return s.Hook.Post(ctx, c.URL, secret, ev.Type, ToPayload(ev, s.BaseURL))
	}
	return errors.New("unknown channel kind")
}

// TestChannel sends a sample event to one channel right now and reports the outcome. Nothing is stored.
func (s *Service) TestChannel(ctx context.Context, id int64) error {
	c, err := s.Channel(ctx, id)
	if err != nil {
		return err
	}
	return s.deliver(ctx, c, Event{Type: "test", Title: "Test notification from Helmsman", Message: "If you can read this, the channel \"" + c.Name + "\" works.", Path: "/admin/notifications", Key: "test", At: s.now()})
}

// State is the small memory the watcher keeps between runs, so a restart announces nothing twice.
func (s *Service) State(ctx context.Context, key string) (string, bool) {
	var v string
	if err := s.St.Pool.QueryRow(ctx, `SELECT value FROM notification_state WHERE key = $1`, key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

// SetState remembers a value.
func (s *Service) SetState(ctx context.Context, key, value string) {
	if _, err := s.St.Pool.Exec(ctx, `INSERT INTO notification_state (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, value); err != nil {
		s.Log.Error("notify: save state", "err", err)
	}
}
