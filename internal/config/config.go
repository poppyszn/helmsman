// Package config loads server settings from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Config holds server settings. M0 only needs a subset of the full variable list.
type Config struct {
	Listen      string
	BaseURL     string
	DatabaseURL string
	GitHubMode  string // real | fake
	DisplayTZ   string
}

// Load reads configuration from the process environment.
func Load() (Config, error) { return FromLookup(os.LookupEnv) }

// FromLookup reads configuration using the supplied lookup function.
func FromLookup(lookup func(string) (string, bool)) (Config, error) {
	get := func(k, def string) string {
		if v, ok := lookup(k); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	c := Config{
		Listen:      get("IDP_LISTEN", ":8080"),
		BaseURL:     get("IDP_BASE_URL", ""),
		DatabaseURL: get("IDP_DATABASE_URL", ""),
		GitHubMode:  get("IDP_GITHUB_MODE", "real"),
		DisplayTZ:   get("IDP_DISPLAY_TZ", "Africa/Lagos"),
	}
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("IDP_DATABASE_URL is required"))
	}
	if c.GitHubMode != "real" && c.GitHubMode != "fake" {
		errs = append(errs, fmt.Errorf("IDP_GITHUB_MODE must be real or fake, got %q", c.GitHubMode))
	}
	return c, errors.Join(errs...)
}
