// Package health derives environment status and runs the health checker.
package health

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"encoding/json"
	"github.com/poppyszn/helmsman/internal/catalog"
	"github.com/poppyszn/helmsman/internal/store"
	"github.com/poppyszn/helmsman/pkg/protocol"
)

// Status values shown in the UI.
const (
	Healthy  = "healthy"
	Degraded = "degraded"
	Down     = "down"
	Unknown  = "unknown"
)

// Probe statuses stored in health_checks.
const (
	ProbeOK   = "ok"
	ProbeSlow = "slow" // answered, but over 2 s
	ProbeFail = "fail"
)

const (
	// SlowAfter is the latency above which a healthy answer counts as degraded.
	SlowAfter = 2 * time.Second
	// Interval between probes and Timeout per probe.
	Interval = 30 * time.Second
	Timeout  = 5 * time.Second
	// fresh is how old a probe may be and still count.
	fresh = 90 * time.Second
)

// Input is everything Derive needs.
type Input struct {
	Now          time.Time
	AgentOnline  bool
	DockerOK     bool // agent can reach Docker (only meaningful when AgentOnline)
	HasContainer bool
	State        string // container state
	DockerHealth string
	Probes       []store.HealthRow // newest first
}

// Derive computes the status. Down needs two consecutive failed probes (no flapping),
// and unknown means we cannot see the environment (agent offline, no fresh probe).
func Derive(in Input) string {
	var latest *store.HealthRow
	if len(in.Probes) > 0 && in.Now.Sub(in.Probes[0].CheckedAt) <= fresh {
		latest = &in.Probes[0]
	}
	containerKnown := in.AgentOnline && in.DockerOK && in.HasContainer
	if latest == nil && !containerKnown {
		return Unknown
	}
	if latest != nil && latest.Status == ProbeFail && len(in.Probes) > 1 && in.Probes[1].Status == ProbeFail {
		return Down
	}
	if containerKnown && in.State != "running" {
		return Down
	}
	if (latest != nil && latest.Status == ProbeSlow) || (containerKnown && in.DockerHealth == "unhealthy") {
		return Degraded
	}
	return Healthy
}

// Prober lets the checker reach agents.
type Prober interface {
	Online(agentID string) (bool, time.Time)
	Probe(ctx context.Context, agentID string, c protocol.HealthCheck) (protocol.HealthResult, error)
}

// Checker probes every environment that has a health URL.
type Checker struct {
	St     *store.Store
	Agents Prober
	Log    *slog.Logger
}

// Classify turns a probe outcome into a stored status.
func Classify(code int, latency time.Duration, errMsg string) string {
	if errMsg != "" || code == 0 || code >= 400 {
		return ProbeFail
	}
	if latency > SlowAfter {
		return ProbeSlow
	}
	return ProbeOK
}

// Run probes every Interval until ctx ends.
func (c *Checker) Run(ctx context.Context) {
	t := time.NewTicker(Interval)
	defer t.Stop()
	c.round(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.round(ctx)
		}
	}
}

func (c *Checker) round(ctx context.Context) {
	apps, err := c.St.ListApps(ctx)
	if err != nil {
		c.Log.Error("health: list apps", "err", err)
		return
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, a := range apps {
		for _, e := range a.Envs {
			var spec catalog.Env
			if json.Unmarshal(e.Spec, &spec) != nil || spec.Health.URL == "" {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				c.probe(ctx, e, spec)
			}()
		}
	}
	wg.Wait()
}

func (c *Checker) probe(ctx context.Context, e store.EnvRow, spec catalog.Env) {
	var code, latencyMS int
	var errMsg string
	if spec.Health.Via == "agent" {
		if on, _ := c.Agents.Online(e.Agent); !on {
			return // nothing to record: status is unknown while the agent is offline
		}
		r, err := c.Agents.Probe(ctx, e.Agent, protocol.HealthCheck{AppEnvID: e.ID, URL: spec.Health.URL,
			TimeoutMS: int(Timeout.Milliseconds()), TLSSkipVerify: spec.Health.TLSSkipVerify})
		if err != nil {
			return
		}
		code, latencyMS, errMsg = r.StatusCode, r.LatencyMS, r.Error
	} else {
		code, latencyMS, errMsg = HTTPProbe(ctx, spec.Health.URL, Timeout, spec.Health.TLSSkipVerify)
	}
	status := Classify(code, time.Duration(latencyMS)*time.Millisecond, errMsg)
	if err := c.St.AddHealth(ctx, e.ID, status, code, latencyMS); err != nil {
		c.Log.Error("health: store", "err", err)
	}
}

// HTTPProbe does one GET without following redirects and returns code, latency and an error.
func HTTPProbe(ctx context.Context, url string, timeout time.Duration, skipTLS bool) (code, latencyMS int, errMsg string) {
	client := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if skipTLS {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // #nosec G402 -- opt in per environment
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, "bad url"
	}
	start := time.Now()
	resp, err := client.Do(req)
	latencyMS = int(time.Since(start).Milliseconds())
	if err != nil {
		return 0, latencyMS, "request failed"
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, latencyMS, ""
}
