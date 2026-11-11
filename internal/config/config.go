// Package config loads server settings from the environment.
package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds server settings.
type Config struct {
	Listen      string
	BaseURL     string
	DatabaseURL string
	// MigrateDatabaseURL is used only to run migrations (the table owner). It defaults to
	// DatabaseURL. In production DatabaseURL logs in as the least-privilege helmsman_app role.
	MigrateDatabaseURL string
	GitHubMode         string // real | fake
	DisplayTZ          string

	SessionKey    []byte
	EncryptionKey []byte
	AllowedOrgs   []string
	BootstrapIDs  []int64
	CatalogPath   string

	OAuthClientID     string
	OAuthClientSecret string
	// GitHub App used for workflow dispatch (M3). In fake mode an ephemeral key is generated.
	GitHubAppID string
	// Break-glass local admin (off by default): bcrypt password hash and base32 TOTP secret.
	BreakglassEnabled      bool
	BreakglassUser         string
	BreakglassPasswordHash string
	BreakglassTOTPSecret   string
	GitHubAppKeyPath       string
	// GitHubAppAPIURL is the REST root used for the App and Actions calls. It defaults to
	// GitHubAPIURL. Setting it lets the dev stack sign in through fake-github while
	// dispatching to the real GitHub.
	GitHubAppAPIURL string
	// TrustedProxies are the networks allowed to set X-Forwarded-For (comma separated CIDRs).
	// Empty means loopback and private ranges. Set it to the proxy address to be stricter.
	TrustedProxies string
	// LokiURL enables log history. Empty means the history view reports that it is not set up.
	LokiURL      string
	LokiUser     string
	LokiPassword string
	// AuthRatePerMinute caps sign-in and agent-join attempts per client address (default 20).
	// The dev stack raises it so end-to-end tests can sign in many times.
	AuthRatePerMinute int
	// GitHub endpoints. Defaults point at github.com; fake mode points at fake-github.
	GitHubAuthURL  string // browser-facing authorize endpoint
	GitHubTokenURL string // server-side token endpoint
	GitHubAPIURL   string // server-side REST API root
}

// Load reads configuration from the process environment.
func Load() (Config, error) { return FromLookup(os.LookupEnv) }

// SecureCookies reports whether cookies get the Secure attribute. They always do
// when the IDP is served over https; plain-http localhost is allowed for dev only.
func (c Config) SecureCookies() bool {
	u, err := url.Parse(c.BaseURL)
	return err != nil || u.Scheme != "http"
}

// FromLookup reads configuration using the supplied lookup function.
func FromLookup(lookup func(string) (string, bool)) (Config, error) {
	get := func(k, def string) string {
		if v, ok := lookup(k); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	c := Config{
		Listen:                 get("IDP_LISTEN", ":8080"),
		BaseURL:                strings.TrimRight(get("IDP_BASE_URL", ""), "/"),
		DatabaseURL:            get("IDP_DATABASE_URL", ""),
		MigrateDatabaseURL:     get("IDP_MIGRATE_DATABASE_URL", ""),
		GitHubMode:             get("IDP_GITHUB_MODE", "real"),
		DisplayTZ:              get("IDP_DISPLAY_TZ", "Africa/Lagos"),
		CatalogPath:            get("IDP_CATALOG_PATH", "catalog/apps.yaml"),
		OAuthClientID:          get("GITHUB_OAUTH_CLIENT_ID", ""),
		OAuthClientSecret:      get("GITHUB_OAUTH_CLIENT_SECRET", ""),
		GitHubAppID:            get("GITHUB_APP_ID", ""),
		BreakglassEnabled:      get("IDP_BREAKGLASS_ENABLED", "false") == "true",
		BreakglassUser:         get("IDP_BREAKGLASS_USER", "breakglass"),
		BreakglassPasswordHash: get("IDP_BREAKGLASS_PASSWORD_HASH", ""),
		BreakglassTOTPSecret:   get("IDP_BREAKGLASS_TOTP_SECRET", ""),
		GitHubAppKeyPath:       get("GITHUB_APP_PRIVATE_KEY_PATH", ""),
		GitHubAppAPIURL:        strings.TrimRight(get("IDP_GITHUB_APP_API_URL", ""), "/"),
		TrustedProxies:         get("IDP_TRUSTED_PROXIES", ""),
		LokiURL:                strings.TrimRight(get("IDP_LOKI_URL", ""), "/"),
		LokiUser:               get("IDP_LOKI_USER", ""),
		LokiPassword:           get("IDP_LOKI_PASSWORD", ""),
		GitHubAuthURL:          get("IDP_GITHUB_AUTH_URL", "https://github.com/login/oauth/authorize"),
		GitHubTokenURL:         get("IDP_GITHUB_TOKEN_URL", "https://github.com/login/oauth/access_token"),
		GitHubAPIURL:           strings.TrimRight(get("IDP_GITHUB_API_URL", "https://api.github.com"), "/"),
	}
	if c.MigrateDatabaseURL == "" {
		c.MigrateDatabaseURL = c.DatabaseURL
	}
	if c.GitHubAppAPIURL == "" {
		c.GitHubAppAPIURL = c.GitHubAPIURL
	}
	for _, o := range strings.Split(get("IDP_ALLOWED_ORGS", ""), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.AllowedOrgs = append(c.AllowedOrgs, o)
		}
	}
	var errs []error
	c.AuthRatePerMinute = 20
	if v := get("IDP_AUTH_RATE_PER_MINUTE", ""); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("IDP_AUTH_RATE_PER_MINUTE must be a positive number, got %q", v))
		} else {
			c.AuthRatePerMinute = n
		}
	}
	if c.BreakglassEnabled && (c.BreakglassPasswordHash == "" || c.BreakglassTOTPSecret == "") {
		errs = append(errs, fmt.Errorf("IDP_BREAKGLASS_ENABLED needs IDP_BREAKGLASS_PASSWORD_HASH and IDP_BREAKGLASS_TOTP_SECRET (run cmd/breakglass-init)"))
	}
	for _, s := range strings.Split(get("IDP_BOOTSTRAP_ADMIN_GITHUB_IDS", ""), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("IDP_BOOTSTRAP_ADMIN_GITHUB_IDS: %q is not a number", s))
			continue
		}
		c.BootstrapIDs = append(c.BootstrapIDs, id)
	}
	var err error
	if c.SessionKey, err = hexKey(lookup, "IDP_SESSION_KEY", 32); err != nil {
		errs = append(errs, err)
	}
	if c.EncryptionKey, err = hexKey(lookup, "IDP_ENCRYPTION_KEY", 32); err != nil {
		errs = append(errs, err)
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("IDP_DATABASE_URL is required"))
	}
	if c.BaseURL == "" {
		errs = append(errs, errors.New("IDP_BASE_URL is required"))
	}
	if len(c.AllowedOrgs) == 0 {
		errs = append(errs, errors.New("IDP_ALLOWED_ORGS must list at least one org"))
	}
	if c.GitHubMode != "real" && c.GitHubMode != "fake" {
		errs = append(errs, fmt.Errorf("IDP_GITHUB_MODE must be real or fake, got %q", c.GitHubMode))
	}
	return c, errors.Join(errs...)
}

// hexKey reads a hex encoded key of at least min bytes.
func hexKey(lookup func(string) (string, bool), name string, min int) ([]byte, error) {
	v, _ := lookup(name)
	b, err := hex.DecodeString(strings.TrimSpace(v))
	if err != nil || len(b) < min {
		return nil, fmt.Errorf("%s must be at least %d bytes of hex (try: openssl rand -hex 32)", name, min)
	}
	return b, nil
}
