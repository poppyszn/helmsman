package notify

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/store"
)

// KindSMTP is the integrations row that holds the mail server settings.
const KindSMTP = "smtp"

// SMTPStatus is what the admin page shows. It never contains the password.
type SMTPStatus struct {
	Configured  bool       `json:"configured"`
	Host        string     `json:"host"`
	Port        int        `json:"port"`
	Security    string     `json:"security"` // starttls | tls | none
	User        string     `json:"user"`
	From        string     `json:"from"`
	HasPassword bool       `json:"has_password"`
	UpdatedBy   string     `json:"updated_by,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

// SMTPInput is what an admin submits. A nil Password keeps the stored one; an empty string clears it.
type SMTPInput struct {
	Host     string  `json:"host"`
	Port     int     `json:"port"`
	Security string  `json:"security"`
	User     string  `json:"user"`
	Password *string `json:"password"`
	From     string  `json:"from"`
}

type smtpConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
	User     string `json:"user"`
	From     string `json:"from"`
}

func (in *SMTPInput) normalize() error {
	in.Host = strings.TrimSpace(in.Host)
	if in.Host == "" || strings.ContainsAny(in.Host, " /\\@:?#") && net.ParseIP(strings.Trim(in.Host, "[]")) == nil {
		return ErrInvalid("enter the mail server's host name, for example smtp.example.com (no https:// and no port)")
	}
	if blockedIP(strings.Trim(in.Host, "[]")) {
		return ErrInvalid("that address is not allowed")
	}
	if in.Port == 0 {
		in.Port = map[string]int{"tls": 465, "starttls": 587, "none": 25}[in.Security]
	}
	if in.Port < 1 || in.Port > 65535 {
		return ErrInvalid("the port must be between 1 and 65535")
	}
	switch in.Security {
	case "starttls", "tls", "none":
	default:
		return ErrInvalid("choose STARTTLS, TLS or none for the connection")
	}
	a, err := mail.ParseAddress(strings.TrimSpace(in.From))
	if err != nil || strings.ContainsAny(in.From, "\r\n") {
		return ErrInvalid("enter the sender address, for example helmsman@example.com")
	}
	in.From = a.String()
	in.User = strings.TrimSpace(in.User)
	return nil
}

// SMTP returns the saved settings.
func (s *Service) SMTP(ctx context.Context) (SMTPStatus, error) {
	cfg, _, row, err := s.smtpSettings(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return SMTPStatus{}, nil
	}
	if err != nil {
		return SMTPStatus{}, err
	}
	st := SMTPStatus{Configured: cfg.Host != "", Host: cfg.Host, Port: cfg.Port, Security: cfg.Security, User: cfg.User, From: cfg.From,
		HasPassword: len(row.SecretEnc) > 0, UpdatedBy: row.UpdatedBy}
	if !row.UpdatedAt.IsZero() {
		t := row.UpdatedAt
		st.UpdatedAt = &t
	}
	return st, nil
}

func (s *Service) smtpSettings(ctx context.Context) (smtpConfig, string, store.Integration, error) {
	row, err := s.St.GetIntegration(ctx, KindSMTP)
	if err != nil {
		return smtpConfig{}, "", row, err
	}
	var c smtpConfig
	_ = json.Unmarshal(row.Config, &c)
	pass := ""
	if len(row.SecretEnc) > 0 {
		b, derr := auth.Decrypt(s.EncKey, row.SecretEnc)
		if derr != nil {
			return c, "", row, fmt.Errorf("decrypt smtp password: %w", derr)
		}
		pass = string(b)
	}
	return c, pass, row, nil
}

// SaveSMTP stores the mail server settings.
func (s *Service) SaveSMTP(ctx context.Context, actor store.User, in SMTPInput) error {
	if err := in.normalize(); err != nil {
		return err
	}
	pass := ""
	if in.Password != nil {
		pass = *in.Password
	} else if _, p, _, err := s.smtpSettings(ctx); err == nil {
		pass = p
	}
	var enc []byte
	if pass != "" {
		var err error
		if enc, err = auth.Encrypt(s.EncKey, []byte(pass)); err != nil {
			return err
		}
	}
	cfg, _ := json.Marshal(smtpConfig{Host: in.Host, Port: in.Port, Security: in.Security, User: in.User, From: in.From})
	return s.St.SaveIntegration(ctx, KindSMTP, true, cfg, enc, actor.ID, nil, "")
}

// RemoveSMTP forgets the mail server settings.
func (s *Service) RemoveSMTP(ctx context.Context) error { return s.St.DeleteIntegration(ctx, KindSMTP) }

// TestSMTP sends a test email through the saved settings.
func (s *Service) TestSMTP(ctx context.Context, to string) error {
	a, err := mail.ParseAddress(strings.TrimSpace(to))
	if err != nil || a.Name != "" || strings.ContainsAny(to, "\r\n,;") {
		return ErrInvalid("enter one plain email address to send the test to")
	}
	return s.Mail.Send(ctx, []string{a.Address}, "[Helmsman] Test email", "If you can read this, Helmsman can send email through the mail server you saved.\n")
}

type smtpMailer struct{ svc *Service }

// friendlySMTP turns a protocol error into something an admin can act on, without echoing server banners.
func friendlySMTP(err error) error {
	s := err.Error()
	switch {
	case strings.Contains(s, "no such host"):
		return errors.New("the mail server's host name could not be found")
	case strings.Contains(s, "connection refused"):
		return errors.New("nothing is listening on that host and port")
	case strings.Contains(s, "i/o timeout"), strings.Contains(s, "deadline exceeded"):
		return errors.New("the mail server did not answer in time")
	case strings.Contains(s, "certificate"):
		return errors.New("the mail server's TLS certificate was not accepted")
	case strings.Contains(s, "535"), strings.Contains(s, "534"), strings.Contains(s, "authentication"):
		return errors.New("the mail server refused the username or password")
	case strings.Contains(s, "unencrypted connection"):
		return errors.New("the mail server needs an encrypted connection to accept a password: use STARTTLS or TLS")
	case strings.Contains(s, "STARTTLS"):
		return errors.New("the mail server does not offer STARTTLS on this port: try TLS (port 465) or another port")
	}
	return fmt.Errorf("sending failed: %s", trim(s, 160))
}

func header(v string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(v) }

func buildMessage(from string, to []string, subject, body string) []byte {
	id := make([]byte, 12)
	_, _ = rand.Read(id)
	host := "helmsman"
	if a, err := mail.ParseAddress(from); err == nil {
		if i := strings.LastIndex(a.Address, "@"); i >= 0 {
			host = a.Address[i+1:]
		}
	}
	var b strings.Builder
	b.WriteString("From: " + header(from) + "\r\n")
	b.WriteString("To: " + header(strings.Join(to, ", ")) + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", header(subject)) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + hex.EncodeToString(id) + "@" + header(host) + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\nAuto-Submitted: auto-generated\r\n\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
	return []byte(b.String())
}

// Send implements MailSender.
func (m *smtpMailer) Send(ctx context.Context, to []string, subject, body string) error {
	cfg, pass, _, err := m.svc.smtpSettings(ctx)
	if errors.Is(err, store.ErrNotFound) || cfg.Host == "" {
		return errors.New("no mail server is set up yet: save the SMTP settings first")
	}
	if err != nil {
		return err
	}
	if len(to) == 0 {
		return errors.New("no recipients")
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	d := &net.Dialer{Timeout: 10 * time.Second, Control: safeControl}
	deadline := time.Now().Add(30 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	var conn net.Conn
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
	if cfg.Security == "tls" {
		conn, err = tls.DialWithDialer(d, "tcp", addr, tlsCfg)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return friendlySMTP(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return friendlySMTP(err)
	}
	defer c.Close()
	if cfg.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return friendlySMTP(errors.New("the server does not support STARTTLS"))
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return friendlySMTP(err)
		}
	}
	if cfg.User != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg.User, pass, cfg.Host)); err != nil {
			return friendlySMTP(err)
		}
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return errors.New("the sender address in the settings is not valid")
	}
	if err := c.Mail(from.Address); err != nil {
		return friendlySMTP(err)
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return friendlySMTP(err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return friendlySMTP(err)
	}
	if _, err := w.Write(buildMessage(cfg.From, to, subject, body)); err != nil {
		return friendlySMTP(err)
	}
	if err := w.Close(); err != nil {
		return friendlySMTP(err)
	}
	return c.Quit()
}
