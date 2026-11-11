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
	GitHubMode  string // real | fake
	DisplayTZ   string

	SessionKey    []byte
	EncryptionKey []byte
	AllowedOrgs   []string
	BootstrapIDs  []int64
	CatalogPath   string

	OAuthClientID     string
	OAuthClientSecret string
	// GitHub App used for workflow dispatch (M3). In fake mode an ephemeral key is generated.
	GitHubAppID      string
	GitHubAppKeyPath string
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
		Listen:            get("IDP_LISTEN", ":8080"),
		BaseURL:           strings.TrimRight(get("IDP_BASE_URL", ""), "/"),
		DatabaseURL:       get("IDP_DATABASE_URL", ""),
		GitHubMode:        get("IDP_GITHUB_MODE", "real"),
		DisplayTZ:         get("IDP_DISPLAY_TZ", "Africa/Lagos"),
		CatalogPath:       get("IDP_CATALOG_PATH", "catalog/apps.yaml"),
		OAuthClientID:     get("GITHUB_OAUTH_CLIENT_ID", ""),
		OAuthClientSecret: get("GITHUB_OAUTH_CLIENT_SECRET", ""),
		GitHubAppID:       get("GITHUB_APP_ID", ""),
		GitHubAppKeyPath:  get("GITHUB_APP_PRIVATE_KEY_PATH", ""),
		GitHubAuthURL:     get("IDP_GITHUB_AUTH_URL", "https://github.com/login/oauth/authorize"),
		GitHubTokenURL:    get("IDP_GITHUB_TOKEN_URL", "https://github.com/login/oauth/access_token"),
		GitHubAPIURL:      strings.TrimRight(get("IDP_GITHUB_API_URL", "https://api.github.com"), "/"),
	}
	for _, o := range strings.Split(get("IDP_ALLOWED_ORGS", ""), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.AllowedOrgs = append(c.AllowedOrgs, o)
		}
	}
	var errs []error
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
