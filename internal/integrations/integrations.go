// Package integrations holds the optional services an admin connects from the UI. Today that is Loki,
// which turns on log history. Settings are stored in the database with the password encrypted, so
// connecting or disconnecting Loki needs no restart and no change to the server's environment.
package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/loki"
	"github.com/poppyszn/helmsman/internal/store"
)

// KindLoki is the integration that provides log history.
const KindLoki = "loki"

// ErrInvalid is returned for input the admin can fix; its message is safe to show.
type ErrInvalid string

func (e ErrInvalid) Error() string { return string(e) }

// EnvDefaults are the settings from the server's environment. They apply only until an admin saves
// settings in the UI, which then take precedence.
type EnvDefaults struct{ URL, User, Password string }

// Manager owns the Loki connection.
type Manager struct {
	St     *store.Store
	EncKey []byte
	Env    EnvDefaults
	// NewClient builds a Loki client. Tests replace it.
	NewClient func(baseURL, user, pass string) loki.Querier
	// Timeout bounds one connection test.
	Timeout time.Duration

	mu     sync.RWMutex
	cur    loki.Querier
	status LokiStatus
}

// New builds a manager.
func New(st *store.Store, encKey []byte, env EnvDefaults) *Manager {
	return &Manager{St: st, EncKey: encKey, Env: env, Timeout: 8 * time.Second,
		NewClient: func(u, user, pass string) loki.Querier { return loki.New(u, user, pass) }}
}

// LokiStatus is what the admin page shows. It never contains the password.
type LokiStatus struct {
	Connected      bool       `json:"connected"` // history is on
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

type lokiConfig struct {
	URL  string `json:"url"`
	User string `json:"user"`
}

// Load reads the settings at startup.
func (m *Manager) Load(ctx context.Context) error {
	row, err := m.St.GetIntegration(ctx, KindLoki)
	switch {
	case err == nil:
		var c lokiConfig
		_ = json.Unmarshal(row.Config, &c)
		pass := ""
		if len(row.SecretEnc) > 0 {
			b, derr := auth.Decrypt(m.EncKey, row.SecretEnc)
			if derr != nil {
				return fmt.Errorf("decrypt loki password: %w", derr)
			}
			pass = string(b)
		}
		m.set(row.Enabled, c, pass, "database", row)
	case errors.Is(err, store.ErrNotFound):
		if m.Env.URL != "" {
			m.set(true, lokiConfig{URL: m.Env.URL, User: m.Env.User}, m.Env.Password, "environment", store.Integration{})
		} else {
			m.mu.Lock()
			m.cur, m.status = nil, LokiStatus{Source: "none"}
			m.mu.Unlock()
		}
	default:
		return err
	}
	return nil
}

func (m *Manager) set(enabled bool, c lokiConfig, pass, source string, row store.Integration) {
	st := LokiStatus{Enabled: enabled, Connected: enabled, URL: c.URL, User: c.User, HasPassword: pass != "", Source: source,
		UpdatedBy: row.UpdatedBy, LastCheckOK: row.LastCheckOK, LastCheckError: row.LastCheckError, LastCheckAt: row.LastCheckAt}
	if !row.UpdatedAt.IsZero() {
		t := row.UpdatedAt
		st.UpdatedAt = &t
	}
	var q loki.Querier
	if enabled && c.URL != "" {
		q = m.NewClient(c.URL, c.User, pass)
	}
	m.mu.Lock()
	m.cur, m.status = q, st
	m.mu.Unlock()
}

// Current implements loki.Source: the Loki to query, or nil when history is not connected.
func (m *Manager) Current() loki.Querier {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}

// Status returns the connection state for the admin page.
func (m *Manager) Status() LokiStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// Input is what an admin submits. A nil Password keeps the stored one; an empty string clears it.
type Input struct {
	URL      string
	User     string
	Password *string
	Enabled  bool
}

// TestResult is the outcome of trying a connection.
type TestResult struct {
	Reachable bool     `json:"reachable"`
	Error     string   `json:"error,omitempty"`
	Labels    []string `json:"labels"`
	// HasLogs is true when Loki already holds Helmsman's labels, meaning Alloy is sending.
	HasLogs bool     `json:"has_logs"`
	Missing []string `json:"missing_labels,omitempty"`
}

// NormalizeURL checks a Loki address. It must be http or https with a host, carry no credentials
// (those have their own fields), and must not point at a cloud metadata endpoint. Private addresses are
// allowed, because Loki normally runs on the internal network.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalid("enter the address of Loki, for example http://loki:3100")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ErrInvalid("the address must start with http:// or https://, for example http://loki:3100")
	}
	if u.User != nil {
		return "", ErrInvalid("put the username and password in their own fields, not in the address")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", ErrInvalid("the address must be the base of Loki, without a query or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if host == "metadata.google.internal" || host == "metadata" {
		return "", ErrInvalid("that address is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
		return "", ErrInvalid("that address is not allowed")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// passwordFor resolves the password to test with: the submitted one, or the stored one when omitted.
func (m *Manager) passwordFor(ctx context.Context, in Input) (string, error) {
	if in.Password != nil {
		return *in.Password, nil
	}
	row, err := m.St.GetIntegration(ctx, KindLoki)
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

// Test tries a connection without saving anything.
func (m *Manager) Test(ctx context.Context, in Input) (TestResult, error) {
	u, err := NormalizeURL(in.URL)
	if err != nil {
		return TestResult{}, err
	}
	pass, err := m.passwordFor(ctx, in)
	if err != nil {
		return TestResult{}, err
	}
	return m.probe(ctx, m.NewClient(u, in.User, pass)), nil
}

var wantLabels = []string{"source", "kind", "level", "compose_service"}

func (m *Manager) probe(ctx context.Context, q loki.Querier) TestResult {
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	var r TestResult
	if err := q.Ready(ctx); err != nil {
		r.Error = friendly(err)
		return r
	}
	r.Reachable = true
	labels, err := q.Labels(ctx)
	if err != nil {
		r.Error = friendly(err)
		r.Reachable = false
		return r
	}
	r.Labels = labels
	have := map[string]bool{}
	for _, l := range labels {
		have[l] = true
	}
	for _, w := range wantLabels {
		if !have[w] {
			r.Missing = append(r.Missing, w)
		}
	}
	r.HasLogs = len(r.Missing) == 0
	return r
}

// friendly turns a connection error into something an admin can act on, without echoing a response body.
func friendly(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "401") || strings.Contains(s, "403"):
		return "Loki refused the login. Check the username and password."
	case strings.Contains(s, "404"):
		return "That address answered, but it does not look like Loki (no /ready). Check the address and any path prefix."
	case strings.Contains(s, "deadline exceeded") || strings.Contains(s, "timeout"):
		return "Loki did not answer in time. Check the address and that it is reachable from the Helmsman server."
	case strings.Contains(s, "no such host"):
		return "That host name could not be found from the Helmsman server."
	case strings.Contains(s, "connection refused"):
		return "Nothing is listening at that address. Is Loki running?"
	case strings.Contains(s, "certificate"):
		return "The TLS certificate was not accepted. Use a certificate the server trusts, or an http:// address on a private network."
	}
	return "Could not connect to Loki."
}

// Save stores the settings and applies them. Turning history on requires a working connection, so a
// typo cannot switch on a view that then fails for everyone.
func (m *Manager) Save(ctx context.Context, actor store.User, in Input) (TestResult, error) {
	u, err := NormalizeURL(in.URL)
	if err != nil {
		return TestResult{}, err
	}
	pass, err := m.passwordFor(ctx, in)
	if err != nil {
		return TestResult{}, err
	}
	var res TestResult
	var ok *bool
	checkErr := ""
	if in.Enabled {
		res = m.probe(ctx, m.NewClient(u, in.User, pass))
		if !res.Reachable {
			return res, ErrInvalid("Loki could not be reached, so it was not turned on: " + res.Error)
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
	cfg, _ := json.Marshal(lokiConfig{URL: u, User: in.User})
	if err := m.St.SaveIntegration(ctx, KindLoki, in.Enabled, cfg, enc, actor.ID, ok, checkErr); err != nil {
		return res, err
	}
	return res, m.Load(ctx)
}

// Remove deletes the stored settings. The environment's settings, if any, apply again.
func (m *Manager) Remove(ctx context.Context) error {
	if err := m.St.DeleteIntegration(ctx, KindLoki); err != nil {
		return err
	}
	return m.Load(ctx)
}
