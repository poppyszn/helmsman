// Package agent is the Helmsman app-server agent. It dials out to the server and reads
// the local Docker socket. It enforces its own allowlist and never runs arbitrary commands.
package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Action names in agent.yaml.
const (
	ActLogs    = "logs"
	ActRestart = "restart"
)

// Allow permits actions on listed services of one compose project.
type Allow struct {
	ComposeProject string   `yaml:"compose_project"`
	Services       []string `yaml:"services"`
	Actions        []string `yaml:"actions"`
	// HealthURLs lists the exact URLs the server may ask this agent to probe.
	HealthURLs []string `yaml:"health_urls"`
}

// Limits protect the host and the server.
type Limits struct {
	MaxStreams     int `yaml:"max_streams"`
	MaxLinesPerSec int `yaml:"max_lines_per_sec"`
	MaxLineBytes   int `yaml:"max_line_bytes"`
}

// Config is agent.yaml plus environment overrides.
type Config struct {
	IDPURL           string        `yaml:"idp_url"`
	Allow            []Allow       `yaml:"allow"`
	Limits           Limits        `yaml:"limits"`
	SnapshotInterval time.Duration `yaml:"snapshot_interval"`

	// From the environment only.
	JoinToken     string `yaml:"-"`
	DataDir       string `yaml:"-"`
	DockerSocket  string `yaml:"-"`
	AllowInsecure bool   `yaml:"-"` // permit ws:// (development only)
}

// LoadConfig reads the YAML file (if present) and applies environment overrides.
func LoadConfig(path string, env func(string) string) (Config, error) {
	var c Config
	if b, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("parse %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return c, err
	}
	if v := env("IDP_URL"); v != "" {
		c.IDPURL = v
	}
	c.JoinToken = env("IDP_JOIN_TOKEN")
	c.DataDir = orDefault(env("IDP_AGENT_DATA"), "/data")
	c.DockerSocket = orDefault(env("DOCKER_SOCKET"), "/var/run/docker.sock")
	c.AllowInsecure = env("IDP_ALLOW_INSECURE") == "true"
	if c.Limits.MaxStreams == 0 {
		c.Limits.MaxStreams = 5
	}
	if c.Limits.MaxLinesPerSec == 0 {
		c.Limits.MaxLinesPerSec = 500
	}
	if c.Limits.MaxLineBytes == 0 || c.Limits.MaxLineBytes > 16384 {
		c.Limits.MaxLineBytes = 16384
	}
	if c.SnapshotInterval == 0 {
		c.SnapshotInterval = 30 * time.Second
	}
	// The server treats 60 s of silence as offline, so snapshots double as the heartbeat.
	if c.SnapshotInterval > 30*time.Second {
		c.SnapshotInterval = 30 * time.Second
	}
	if c.SnapshotInterval < 5*time.Second {
		c.SnapshotInterval = 5 * time.Second
	}
	return c, c.validate()
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func (c Config) validate() error {
	u, err := url.Parse(c.IDPURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("idp_url %q is not a valid URL", c.IDPURL)
	}
	switch u.Scheme {
	case "wss":
	case "ws":
		if !c.AllowInsecure {
			return fmt.Errorf("idp_url uses ws://; refusing plain text outside development (set IDP_ALLOW_INSECURE=true only for local dev)")
		}
	default:
		return fmt.Errorf("idp_url must start with wss://")
	}
	for _, a := range c.Allow {
		if a.ComposeProject == "" {
			return fmt.Errorf("allow entry without compose_project")
		}
		for _, act := range a.Actions {
			if act != ActLogs && act != ActRestart {
				return fmt.Errorf("unknown action %q in allow (only logs, restart)", act)
			}
		}
	}
	return nil
}

// JoinURL is the HTTP(S) endpoint that exchanges a join token.
func (c Config) JoinURL() string {
	u, _ := url.Parse(c.IDPURL)
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	u.Path = "/agent/join"
	return u.String()
}

// Allowed reports whether an action on project/service is permitted by the local allowlist.
func (c Config) Allowed(action, project, service string) bool {
	for _, a := range c.Allow {
		if a.ComposeProject != project || !contains(a.Services, service) {
			continue
		}
		if contains(a.Actions, action) {
			return true
		}
	}
	return false
}

// Visible reports whether the service appears in snapshots at all.
func (c Config) Visible(project, service string) bool {
	for _, a := range c.Allow {
		if a.ComposeProject == project && contains(a.Services, service) {
			return true
		}
	}
	return false
}

// HealthAllowed reports whether the server may ask for a probe of this exact URL.
func (c Config) HealthAllowed(u string) bool {
	for _, a := range c.Allow {
		if contains(a.HealthURLs, u) {
			return true
		}
	}
	return false
}

// Digest is a stable fingerprint of the allowlist, sent in hello.
func (c Config) Digest() string {
	var parts []string
	for _, a := range c.Allow {
		s := append([]string(nil), a.Services...)
		ac := append([]string(nil), a.Actions...)
		h := append([]string(nil), a.HealthURLs...)
		sort.Strings(s)
		sort.Strings(ac)
		sort.Strings(h)
		parts = append(parts, a.ComposeProject+"|"+strings.Join(s, ",")+"|"+strings.Join(ac, ",")+"|"+strings.Join(h, ","))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:8])
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}
