package notify

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/store"
)

// ErrInvalid is returned for input the admin can fix; its message is safe to show.
type ErrInvalid string

func (e ErrInvalid) Error() string { return string(e) }

// Channel is a place events go. The webhook secret is never returned after it is created.
type Channel struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // email | webhook
	To        []string  `json:"to,omitempty"`
	URL       string    `json:"url,omitempty"`
	HasSecret bool      `json:"has_secret"`
	Events    []string  `json:"events"`
	Tier      string    `json:"tier"` // all | prod | nonprod
	Apps      []string  `json:"apps"` // limited to these apps; empty means all
	Envs      []string  `json:"envs"` // limited to these environment names; empty means all
	Enabled   bool      `json:"enabled"`
	CreatedBy string    `json:"created_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ChannelInput is what an admin submits. Secret is for webhooks: empty keeps the stored one on an update
// and generates one on create.
type ChannelInput struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	To      []string `json:"to"`
	URL     string   `json:"url"`
	Secret  string   `json:"secret"`
	Events  []string `json:"events"`
	Tier    string   `json:"tier"`
	Apps    []string `json:"apps"`
	Envs    []string `json:"envs"`
	Enabled bool     `json:"enabled"`
}

type channelConfig struct {
	To  []string `json:"to,omitempty"`
	URL string   `json:"url,omitempty"`
}

const maxRecipients = 20

// ValidateWebhookURL accepts http and https addresses with a host and no credentials, and refuses cloud
// metadata and link-local addresses. Private networks are allowed, since a webhook often points inside
// the company. The connection itself is checked again when it is made (see safeDialer).
func ValidateWebhookURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ErrInvalid("the webhook address must start with https:// (or http:// on a private network)")
	}
	if u.User != nil {
		return "", ErrInvalid("put secrets in the signing secret or the path, not as user:password in the address")
	}
	if u.Fragment != "" {
		return "", ErrInvalid("the webhook address cannot have a fragment")
	}
	host := strings.ToLower(u.Hostname())
	if host == "metadata.google.internal" || host == "metadata" {
		return "", ErrInvalid("that address is not allowed")
	}
	if blockedIP(host) {
		return "", ErrInvalid("that address is not allowed")
	}
	return u.String(), nil
}

func (in *ChannelInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	if n := len([]rune(in.Name)); n < 1 || n > 60 {
		return ErrInvalid("give the channel a name of 1 to 60 characters")
	}
	if in.Kind != "email" && in.Kind != "webhook" {
		return ErrInvalid("the channel must be an email or a webhook")
	}
	if in.Tier == "" {
		in.Tier = "all"
	}
	if in.Tier != "all" && in.Tier != "prod" && in.Tier != "nonprod" {
		return ErrInvalid("the scope must be all, prod or nonprod environments")
	}
	var err error
	if in.Apps, err = cleanSlugs(in.Apps, "app"); err != nil {
		return err
	}
	if in.Envs, err = cleanSlugs(in.Envs, "environment"); err != nil {
		return err
	}
	seen := map[string]bool{}
	events := []string{}
	for _, e := range in.Events {
		if !ValidType(e) {
			return ErrInvalid("unknown event " + e)
		}
		if !seen[e] {
			seen[e] = true
			events = append(events, e)
		}
	}
	if len(events) == 0 {
		return ErrInvalid("choose at least one event")
	}
	in.Events = events
	switch in.Kind {
	case "email":
		to := []string{}
		for _, r := range in.To {
			r = strings.TrimSpace(r)
			if r == "" {
				continue
			}
			a, err := mail.ParseAddress(r)
			if err != nil || a.Name != "" || strings.ContainsAny(r, "\r\n,;<>") {
				return ErrInvalid("\"" + r + "\" is not a plain email address")
			}
			to = append(to, a.Address)
		}
		if len(to) == 0 {
			return ErrInvalid("add at least one recipient")
		}
		if len(to) > maxRecipients {
			return ErrInvalid(fmt.Sprintf("at most %d recipients per channel", maxRecipients))
		}
		in.To, in.URL, in.Secret = to, "", ""
	case "webhook":
		u, err := ValidateWebhookURL(in.URL)
		if err != nil {
			return err
		}
		in.URL, in.To = u, nil
		if len(in.Secret) > 200 {
			return ErrInvalid("the signing secret is too long")
		}
	}
	return nil
}

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// cleanSlugs checks a list of app ids or environment names against the catalog's slug rule.
func cleanSlugs(in []string, what string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		if !slugRE.MatchString(v) {
			return nil, ErrInvalid("\"" + v + "\" is not a valid " + what + " name")
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) > 200 {
		return nil, ErrInvalid("too many " + what + "s selected")
	}
	return out, nil
}

func newSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

const channelCols = `c.id, c.name, c.kind, c.config, c.secret_enc IS NOT NULL, c.events, c.tier, c.apps, c.envs, c.enabled, u.login, c.updated_at`

func scanChannel(r pgx.Row) (Channel, error) {
	var c Channel
	var cfg []byte
	var by *string
	if err := r.Scan(&c.ID, &c.Name, &c.Kind, &cfg, &c.HasSecret, &c.Events, &c.Tier, &c.Apps, &c.Envs, &c.Enabled, &by, &c.UpdatedAt); err != nil {
		return c, err
	}
	var cc channelConfig
	_ = json.Unmarshal(cfg, &cc)
	c.To, c.URL = cc.To, cc.URL
	if by != nil {
		c.CreatedBy = *by
	}
	return c, nil
}

// Channels lists every channel.
func (s *Service) Channels(ctx context.Context) ([]Channel, error) {
	rows, err := s.St.Pool.Query(ctx, `SELECT `+channelCols+` FROM notification_channels c LEFT JOIN users u ON u.id = c.created_by ORDER BY lower(c.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Channel returns one channel, or store.ErrNotFound.
func (s *Service) Channel(ctx context.Context, id int64) (Channel, error) {
	c, err := scanChannel(s.St.Pool.QueryRow(ctx, `SELECT `+channelCols+` FROM notification_channels c LEFT JOIN users u ON u.id = c.created_by WHERE c.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, store.ErrNotFound
	}
	return c, err
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "notification_channels_name")
}

// CreateChannel stores a channel. The webhook signing secret is returned once, here.
func (s *Service) CreateChannel(ctx context.Context, actor store.User, in ChannelInput) (Channel, string, error) {
	if err := in.normalize(); err != nil {
		return Channel{}, "", err
	}
	secret := ""
	var enc []byte
	if in.Kind == "webhook" {
		secret = in.Secret
		if secret == "" {
			secret = newSecret()
		}
		var err error
		if enc, err = auth.Encrypt(s.EncKey, []byte(secret)); err != nil {
			return Channel{}, "", err
		}
	}
	cfg, _ := json.Marshal(channelConfig{To: in.To, URL: in.URL})
	var id int64
	err := s.St.Pool.QueryRow(ctx, `INSERT INTO notification_channels (name, kind, config, secret_enc, events, tier, apps, envs, enabled, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, in.Name, in.Kind, cfg, enc, in.Events, in.Tier, in.Apps, in.Envs, in.Enabled, actor.ID).Scan(&id)
	if isUnique(err) {
		return Channel{}, "", ErrInvalid("a channel with that name already exists")
	} else if err != nil {
		return Channel{}, "", err
	}
	c, err := s.Channel(ctx, id)
	return c, secret, err
}

// UpdateChannel changes a channel. An empty secret keeps the stored one; the kind cannot change.
func (s *Service) UpdateChannel(ctx context.Context, id int64, in ChannelInput) (Channel, string, error) {
	cur, err := s.Channel(ctx, id)
	if err != nil {
		return Channel{}, "", err
	}
	in.Kind = cur.Kind
	if err := in.normalize(); err != nil {
		return Channel{}, "", err
	}
	secret := ""
	var enc []byte
	keep := true
	if in.Kind == "webhook" && in.Secret != "" {
		secret, keep = in.Secret, false
		if enc, err = auth.Encrypt(s.EncKey, []byte(secret)); err != nil {
			return Channel{}, "", err
		}
	}
	cfg, _ := json.Marshal(channelConfig{To: in.To, URL: in.URL})
	_, err = s.St.Pool.Exec(ctx, `UPDATE notification_channels SET name=$2, config=$3, events=$4, tier=$5, apps=$6, envs=$7, enabled=$8, updated_at=now(),
		secret_enc = CASE WHEN $9 THEN secret_enc ELSE $10 END WHERE id=$1`, id, in.Name, cfg, in.Events, in.Tier, in.Apps, in.Envs, in.Enabled, keep, enc)
	if isUnique(err) {
		return Channel{}, "", ErrInvalid("a channel with that name already exists")
	} else if err != nil {
		return Channel{}, "", err
	}
	c, err := s.Channel(ctx, id)
	return c, secret, err
}

// DeleteChannel removes a channel and its delivery history.
func (s *Service) DeleteChannel(ctx context.Context, id int64) error {
	tag, err := s.St.Pool.Exec(ctx, `DELETE FROM notification_channels WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return err
}

func (s *Service) secretOf(ctx context.Context, id int64) (string, error) {
	var enc []byte
	if err := s.St.Pool.QueryRow(ctx, `SELECT secret_enc FROM notification_channels WHERE id = $1`, id).Scan(&enc); err != nil {
		return "", err
	}
	if len(enc) == 0 {
		return "", nil
	}
	b, err := auth.Decrypt(s.EncKey, enc)
	return string(b), err
}
