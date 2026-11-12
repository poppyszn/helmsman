// Package catalog loads, validates and syncs catalog/apps.yaml.
package catalog

import (
	"bytes"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

// Catalog is the parsed apps.yaml.
type Catalog struct {
	Version int   `yaml:"version" json:"version"`
	Apps    []App `yaml:"apps" json:"apps"`
}

// App is one application.
type App struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
	Repo string `yaml:"repo" json:"repo"`
	// Links are shown on every environment of the app (dashboards, runbooks, docs).
	Links        []Link `yaml:"links" json:"links"`
	Environments []Env  `yaml:"environments" json:"environments"`
}

// Link is a named shortcut shown on the app page.
type Link struct {
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url" json:"url"`
}

// Env is one deployment of an app.
type Env struct {
	Env               string    `yaml:"env" json:"env"`
	Tier              string    `yaml:"tier" json:"tier"`
	Agent             string    `yaml:"agent" json:"agent"`
	Container         Container `yaml:"container" json:"container"`
	URL               string    `yaml:"url" json:"url"`
	Health            Health    `yaml:"health" json:"health"`
	Workflows         Workflows `yaml:"workflows" json:"workflows"`
	GitHubEnvironment string    `yaml:"github_environment" json:"github_environment"`
	AllowSeed         *bool     `yaml:"allow_seed" json:"allow_seed"`
	// RequireApproval makes a migration or seed wait for a second person with action.approve before it is
	// started. Off unless set to true. A run still needs the permission on that exact environment.
	RequireApproval *bool `yaml:"require_approval" json:"require_approval"`
	Logs            Logs  `yaml:"logs" json:"logs"`
	// Links are shortcuts for this environment only. The app's own links come first.
	Links []Link `yaml:"links" json:"links"`
}

// Logs says where this environment's history lives besides its container: FaroApp is the app name the
// static or single page app gives Grafana Faro, so browser logs and errors show up beside the
// container logs. Its Faro environment must equal this env's name.
type Logs struct {
	FaroApp string `yaml:"faro_app" json:"faro_app"`
}

// Container selects a container by compose labels, never by ID.
type Container struct {
	ComposeProject string `yaml:"compose_project" json:"compose_project"`
	Service        string `yaml:"service" json:"service"`
}

// Health configures the health check.
type Health struct {
	URL           string `yaml:"url" json:"url"`
	Via           string `yaml:"via" json:"via"`
	TLSSkipVerify bool   `yaml:"tls_skip_verify" json:"tls_skip_verify"`
}

// Workflows names workflow files in the app repo.
type Workflows struct {
	Deploy  string `yaml:"deploy" json:"deploy"`
	Migrate string `yaml:"migrate" json:"migrate"`
}

// SeedAllowed applies the default: seeds are off for prod, on for nonprod.
func (e Env) SeedAllowed() bool {
	if e.AllowSeed != nil {
		return *e.AllowSeed
	}
	return e.Tier != "prod"
}

// NeedsApproval reports whether runs wait for a second person. They do not unless the environment says so.
func (e Env) NeedsApproval() bool { return e.RequireApproval != nil && *e.RequireApproval }

// Parse decodes YAML strictly: unknown fields are errors, so typos are caught.
func Parse(data []byte) (*Catalog, error) {
	var c Catalog
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	return &c, nil
}

// LoadFile reads and parses a catalog file.
func LoadFile(path string) (*Catalog, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	c, err := Parse(data)
	return c, data, err
}
