package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/mailpit"
	"github.com/poppyszn/helmsman/internal/store"
)

// KindMailpit is the integration that provides the mail viewer.
const KindMailpit = "mailpit"

// MailStatus is what the admin page shows for Mailpit. It never contains the password.
type MailStatus struct {
	Connected      bool       `json:"connected"`
	Enabled        bool       `json:"enabled"`
	URL            string     `json:"url"`
	User           string     `json:"user"`
	HasPassword    bool       `json:"has_password"`
	Source         string     `json:"source"` // none | database | environment
	UpdatedBy      string     `json:"updated_by,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
	LastCheckAt    *time.Time `json:"last_check_at,omitempty"`
	LastCheckOK    *bool      `json:"last_check_ok,omitempty"`
	LastCheckError string     `json:"last_check_error,omitempty"`
}

// MailTestResult is the outcome of trying a Mailpit connection.
type MailTestResult struct {
	Reachable bool   `json:"reachable"`
	Version   string `json:"version,omitempty"`
	Total     int    `json:"total"`
	Error     string `json:"error,omitempty"`
}

type mailConfig struct {
	URL  string `json:"url"`
	User string `json:"user"`
}

// MailManager owns the Mailpit connection, the same way Manager owns Loki's.
type MailManager struct {
	St     *store.Store
	EncKey []byte
	Env    EnvDefaults
	// NewClient builds a Mailpit client. Tests replace it.
	NewClient func(baseURL, user, pass string) mailpit.Reader
	Timeout   time.Duration

	mu     sync.RWMutex
	cur    mailpit.Reader
	status MailStatus
}

// NewMail builds a manager.
func NewMail(st *store.Store, encKey []byte, env EnvDefaults) *MailManager {
	return &MailManager{St: st, EncKey: encKey, Env: env, Timeout: 8 * time.Second,
		NewClient: func(u, user, pass string) mailpit.Reader { return mailpit.New(u, user, pass) }}
}

// Load reads the settings at startup and after every change.
func (m *MailManager) Load(ctx context.Context) error {
	row, err := m.St.GetIntegration(ctx, KindMailpit)
	switch {
	case err == nil:
		var c mailConfig
		_ = json.Unmarshal(row.Config, &c)
		pass := ""
		if len(row.SecretEnc) > 0 {
			b, derr := auth.Decrypt(m.EncKey, row.SecretEnc)
			if derr != nil {
				return fmt.Errorf("decrypt mailpit password: %w", derr)
			}
			pass = string(b)
		}
		m.set(row.Enabled, c, pass, "database", row)
	case errors.Is(err, store.ErrNotFound):
		if m.Env.URL != "" {
			m.set(true, mailConfig{URL: m.Env.URL, User: m.Env.User}, m.Env.Password, "environment", store.Integration{})
		} else {
			m.mu.Lock()
			m.cur, m.status = nil, MailStatus{Source: "none"}
			m.mu.Unlock()
		}
	default:
		return err
	}
	return nil
}

func (m *MailManager) set(enabled bool, c mailConfig, pass, source string, row store.Integration) {
	st := MailStatus{Enabled: enabled, Connected: enabled, URL: c.URL, User: c.User, HasPassword: pass != "", Source: source,
		UpdatedBy: row.UpdatedBy, LastCheckOK: row.LastCheckOK, LastCheckError: row.LastCheckError, LastCheckAt: row.LastCheckAt}
	if !row.UpdatedAt.IsZero() {
		t := row.UpdatedAt
		st.UpdatedAt = &t
	}
	var r mailpit.Reader
	if enabled && c.URL != "" {
		r = m.NewClient(c.URL, c.User, pass)
	}
	m.mu.Lock()
	m.cur, m.status = r, st
	m.mu.Unlock()
}

// Current implements mailpit.Source.
func (m *MailManager) Current() mailpit.Reader {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}

// Status returns the connection state for the admin page.
func (m *MailManager) Status() MailStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *MailManager) passwordFor(ctx context.Context, in Input) (string, error) {
	if in.Password != nil {
		return *in.Password, nil
	}
	row, err := m.St.GetIntegration(ctx, KindMailpit)
	if errors.Is(err, store.ErrNotFound) {
		return m.Env.Password, nil
	}
	if err != nil {
		return "", err
	}
	if len(row.SecretEnc) == 0 {
		return "", nil
	}
	b, err := auth.Decrypt(m.EncKey, row.SecretEnc)
	return string(b), err
}

func (m *MailManager) probe(ctx context.Context, r mailpit.Reader) MailTestResult {
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	var out MailTestResult
	v, err := r.Info(ctx)
	if err != nil {
		out.Error = mailFriendly(err)
		return out
	}
	out.Reachable, out.Version = true, v
	if l, err := r.List(ctx, "", 0, 1); err == nil {
		out.Total = l.Total
	}
	return out
}

func mailFriendly(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "401") || strings.Contains(s, "403"):
		return "Mailpit refused the login. Check the username and password."
	case strings.Contains(s, "404"):
		return "That address answered, but it does not look like Mailpit (no /api/v1/info). Check the address and any path prefix."
	case strings.Contains(s, "deadline exceeded") || strings.Contains(s, "timeout"):
		return "Mailpit did not answer in time. Check the address and that it is reachable from the Helmsman server."
	case strings.Contains(s, "no such host"):
		return "That host name could not be found from the Helmsman server."
	case strings.Contains(s, "connection refused"):
		return "Nothing is listening at that address. Is Mailpit running?"
	case strings.Contains(s, "certificate"):
		return "The TLS certificate was not accepted. Use a certificate the server trusts, or an http:// address on a private network."
	}
	return "Could not connect to Mailpit."
}

// Test tries a connection without saving anything.
func (m *MailManager) Test(ctx context.Context, in Input) (MailTestResult, error) {
	u, err := normalizeURL(in.URL, "Mailpit", "http://mailpit:8025")
	if err != nil {
		return MailTestResult{}, err
	}
	pass, err := m.passwordFor(ctx, in)
	if err != nil {
		return MailTestResult{}, err
	}
	return m.probe(ctx, m.NewClient(u, in.User, pass)), nil
}

// Save stores the settings and applies them. Turning the viewer on requires a working connection.
func (m *MailManager) Save(ctx context.Context, actor store.User, in Input) (MailTestResult, error) {
	u, err := normalizeURL(in.URL, "Mailpit", "http://mailpit:8025")
	if err != nil {
		return MailTestResult{}, err
	}
	pass, err := m.passwordFor(ctx, in)
	if err != nil {
		return MailTestResult{}, err
	}
	var res MailTestResult
	var ok *bool
	if in.Enabled {
		res = m.probe(ctx, m.NewClient(u, in.User, pass))
		if !res.Reachable {
			return res, ErrInvalid("Mailpit could not be reached, so it was not turned on: " + res.Error)
		}
		t := true
		ok = &t
	}
	var enc []byte
	if pass != "" {
		if enc, err = auth.Encrypt(m.EncKey, []byte(pass)); err != nil {
			return res, err
		}
	}
	cfg, _ := json.Marshal(mailConfig{URL: u, User: in.User})
	if err := m.St.SaveIntegration(ctx, KindMailpit, in.Enabled, cfg, enc, actor.ID, ok, ""); err != nil {
		return res, err
	}
	return res, m.Load(ctx)
}

// Remove deletes the stored settings. The environment's settings, if any, apply again.
func (m *MailManager) Remove(ctx context.Context) error {
	if err := m.St.DeleteIntegration(ctx, KindMailpit); err != nil {
		return err
	}
	return m.Load(ctx)
}
