package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	mrand "math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/poppyszn/helmsman/pkg/protocol"
)

var (
	errUnauthorized = errors.New("server rejected the agent credential")
	errDuplicate    = errors.New("server reports another agent is using this credential")
)

// Agent connects to the server and serves requests from it.
type Agent struct {
	cfg        Config
	log        *slog.Logger
	docker     *Docker
	version    string
	instanceID string
	hostname   string
	events     *notifier
}

// New builds an agent.
func New(cfg Config, version string, log *slog.Logger) *Agent {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	host, _ := os.Hostname()
	return &Agent{cfg: cfg, log: log, docker: NewDocker(cfg.DockerSocket), version: version,
		instanceID: hex.EncodeToString(b), hostname: host, events: newNotifier()}
}

// Run connects, reconnecting with exponential backoff and jitter (1 s to 60 s) until ctx ends.
func (a *Agent) Run(ctx context.Context) error {
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := a.once(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(started) > 30*time.Second {
			backoff = time.Second
		}
		a.log.Warn("disconnected", "err", err, "retry_in", backoff.String())
		// Full jitter in the upper half so that agents do not reconnect in lockstep.
		d := backoff/2 + time.Duration(mrand.Int64N(int64(backoff/2)+1))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(d):
		}
		if backoff *= 2; backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
	}
	return nil
}

func (a *Agent) once(ctx context.Context) error {
	cred, err := a.credential(ctx)
	if err != nil {
		return err
	}
	err = a.session(ctx, cred)
	if errors.Is(err, errUnauthorized) && a.cfg.JoinToken != "" {
		// The credential is stale (database restored, agent re-created). A fresh join token
		// is single use and expires, so trying it again is safe.
		a.log.Warn("credential rejected; will try the join token", "err", err)
		_ = os.Remove(filepath.Join(a.cfg.DataDir, "credential"))
	}
	return err
}

func (a *Agent) credential(ctx context.Context) (string, error) {
	path := filepath.Join(a.cfg.DataDir, "credential")
	if b, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(b)) > 0 {
		return strings.TrimSpace(string(b)), nil
	}
	if a.cfg.JoinToken == "" {
		return "", errors.New("no stored credential and no IDP_JOIN_TOKEN; create the agent in the UI and set the token")
	}
	body, _ := json.Marshal(map[string]string{"join_token": a.cfg.JoinToken, "hostname": a.hostname, "version": a.version})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.JoinURL(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(cctx))
	if err != nil {
		return "", fmt.Errorf("join: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return "", fmt.Errorf("join rejected (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		AgentID    string `json:"agent_id"`
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err != nil || out.Credential == "" {
		return "", errors.New("join: bad response")
	}
	if err := os.MkdirAll(a.cfg.DataDir, 0o700); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(out.Credential), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	a.log.Info("joined", "agent_id", out.AgentID)
	return out.Credential, nil
}

// session is one connection.
type session struct {
	a       *Agent
	conn    *websocket.Conn
	ctx     context.Context
	cancel  context.CancelFunc
	trigger chan struct{}

	mu       sync.Mutex
	streams  map[string]context.CancelFunc
	restarts map[string]bool
}

func (a *Agent) session(parent context.Context, cred string) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	opts := &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + cred}}}
	dctx, dcancel := context.WithTimeout(ctx, 15*time.Second)
	conn, resp, err := websocket.Dial(dctx, a.cfg.IDPURL, opts)
	dcancel()
	if err != nil {
		if resp != nil {
			switch resp.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return errUnauthorized
			case http.StatusConflict:
				return errDuplicate
			}
		}
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	s := &session{a: a, conn: conn, ctx: ctx, cancel: cancel, trigger: make(chan struct{}, 1),
		streams: map[string]context.CancelFunc{}, restarts: map[string]bool{}}

	dv, derr := a.docker.Version(ctx)
	if derr != nil {
		dv = ""
	}
	if err := s.send(protocol.TypeHello, protocol.Hello{
		AgentVersion: a.version, ProtocolVersion: protocol.Version, Hostname: a.hostname,
		DockerVersion: dv, AllowlistDigest: a.cfg.Digest(), InstanceID: a.instanceID,
	}); err != nil {
		return err
	}
	a.log.Info("connected", "docker", dv)

	var wg sync.WaitGroup
	for _, f := range []func(){s.snapshotLoop, s.eventLoop, s.pingLoop} {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	err = s.readLoop()
	cancel()
	_ = conn.Close(websocket.StatusNormalClosure, "bye")
	wg.Wait()
	return err
}

func (s *session) send(typ string, payload any) error {
	b, err := protocol.Encode(typ, payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	if err := s.conn.Write(ctx, websocket.MessageText, b); err != nil {
		s.cancel()
		return err
	}
	return nil
}

func (s *session) readLoop() error {
	for {
		_, b, err := s.conn.Read(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			if websocket.CloseStatus(err) == websocket.StatusPolicyViolation {
				return fmt.Errorf("%w (%v)", errDuplicate, err)
			}
			return err
		}
		env, err := protocol.Decode(b)
		if err != nil {
			_ = s.send(protocol.TypeError, protocol.ErrorMsg{Code: protocol.ErrCodeUnsupported, Message: err.Error()})
			continue
		}
		s.dispatch(env)
	}
}

func (s *session) dispatch(env protocol.Envelope) {
	switch env.Type {
	case protocol.TypeLogsOpen:
		var m protocol.LogsOpen
		if protocol.Unmarshal(env, &m) != nil || m.StreamID == "" {
			_ = s.send(protocol.TypeError, protocol.ErrorMsg{Code: protocol.ErrCodeBadMessage, Message: "bad logs.open", RelatedID: env.ID})
			return
		}
		s.openLogs(m)
	case protocol.TypeLogsClose:
		var m protocol.LogsClose
		if protocol.Unmarshal(env, &m) == nil {
			s.mu.Lock()
			if c, ok := s.streams[m.StreamID]; ok {
				c()
			}
			s.mu.Unlock()
		}
	case protocol.TypeHealthCheck:
		var m protocol.HealthCheck
		if protocol.Unmarshal(env, &m) == nil {
			go s.healthCheck(m)
		}
	case protocol.TypeActionRequest:
		var m protocol.ActionRequest
		if protocol.Unmarshal(env, &m) == nil {
			go s.action(m)
		}
	case protocol.TypeError:
		var m protocol.ErrorMsg
		if protocol.Unmarshal(env, &m) == nil {
			s.a.log.Warn("server reported an error", "code", m.Code, "message", m.Message)
		}
	default:
		// Unknown types from a newer server are ignored, not fatal.
		_ = s.send(protocol.TypeError, protocol.ErrorMsg{Code: protocol.ErrCodeUnsupported, Message: "unknown message type " + env.Type, RelatedID: env.ID})
	}
}

// pingLoop sends a WebSocket ping every 20 s and drops the connection when it is not answered.
func (s *session) pingLoop() {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
			err := s.conn.Ping(ctx)
			cancel()
			if err != nil {
				s.a.log.Warn("ping failed", "err", err)
				s.cancel()
				return
			}
		}
	}
}

func (s *session) kick() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// snapshotLoop sends a snapshot on connect, on the interval, and after Docker events.
// It doubles as the heartbeat. While Docker is down it retries every 5 s.
func (s *session) snapshotLoop() {
	next := time.Duration(0)
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(next):
		case <-s.trigger:
			// Coalesce bursts (a restart emits several events).
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
		snap := s.a.snapshot(s.ctx)
		if err := s.send(protocol.TypeSnapshot, snap); err != nil {
			return
		}
		next = s.a.cfg.SnapshotInterval
		if !snap.DockerOK {
			next = 5 * time.Second
		}
	}
}

func (a *Agent) snapshot(ctx context.Context) protocol.Snapshot {
	cs, _, err := a.docker.Containers(ctx, a.cfg)
	if err != nil {
		return protocol.Snapshot{DockerOK: false, DockerError: "docker_unavailable", Containers: []protocol.Container{}}
	}
	return protocol.Snapshot{DockerOK: true, Containers: cs}
}

// eventLoop follows the Docker event stream and reconnects every 5 s when it breaks.
func (s *session) eventLoop() {
	for s.ctx.Err() == nil {
		err := s.a.docker.Events(s.ctx, s.onDockerEvent)
		if s.ctx.Err() != nil {
			return
		}
		s.a.log.Warn("docker events stream ended", "err", err)
		s.kick()
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (s *session) onDockerEvent(e DockerEvent) {
	project, service := e.Attrs[labelProject], e.Attrs[labelService]
	if !s.a.cfg.Visible(project, service) {
		return
	}
	ev := protocol.Event{ComposeProject: project, Service: service, ContainerID: e.ID, At: e.Time}
	switch {
	case e.Action == "start":
		ev.Kind = protocol.EventStart
	case e.Action == "die":
		ev.Kind = protocol.EventDie
		var code int
		if _, err := fmt.Sscan(e.Attrs["exitCode"], &code); err == nil {
			ev.ExitCode = &code
		}
	case e.Action == "restart":
		ev.Kind = protocol.EventRestart
	case strings.HasPrefix(e.Action, "health_status"):
		ev.Kind = protocol.EventHealth
		ev.Detail = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(e.Action, "health_status"), ":"))
	default:
		return
	}
	_ = s.send(protocol.TypeEvent, ev)
	s.a.events.broadcast()
	s.kick()
}

func (s *session) action(m protocol.ActionRequest) {
	reply := func(status, msg string) {
		_ = s.send(protocol.TypeActionResult, protocol.ActionResult{RequestID: m.RequestID, Status: status, Message: msg})
	}
	// The only action is restart. Anything else is refused whatever the server asks for.
	if m.Action != protocol.ActionRestart {
		reply(protocol.ResultDenied, "unsupported action")
		return
	}
	sel := m.Selector
	if !s.a.cfg.Allowed(ActRestart, sel.ComposeProject, sel.Service) {
		s.a.log.Warn("restart refused by allowlist", "project", sel.ComposeProject, "service", sel.Service)
		reply(protocol.ResultDenied, "not allowed by the agent allowlist")
		return
	}
	key := sel.ComposeProject + "/" + sel.Service
	s.mu.Lock()
	if s.restarts[key] {
		s.mu.Unlock()
		reply(protocol.ResultFailed, "a restart is already in progress")
		return
	}
	s.restarts[key] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.restarts, key); s.mu.Unlock() }()

	c, _, err := s.a.docker.Find(s.ctx, s.a.cfg, sel.ComposeProject, sel.Service)
	if err != nil {
		if errors.Is(err, errNotFound) {
			reply(protocol.ResultFailed, "container not found")
		} else {
			reply(protocol.ResultFailed, "docker unavailable")
		}
		return
	}
	if err := s.a.docker.Restart(s.ctx, c.ID); err != nil {
		s.a.log.Warn("restart failed", "service", key, "err", err)
		reply(protocol.ResultFailed, "docker could not restart the container")
		return
	}
	s.a.log.Info("restarted", "service", key)
	s.kick()
	reply(protocol.ResultOK, "restarted")
}

func (s *session) healthCheck(m protocol.HealthCheck) {
	res := protocol.HealthResult{AppEnvID: m.AppEnvID}
	if !s.a.cfg.HealthAllowed(m.URL) {
		res.Error = "url not allowed by the agent allowlist"
		_ = s.send(protocol.TypeHealthResult, res)
		return
	}
	timeout := time.Duration(m.TimeoutMS) * time.Millisecond
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 5 * time.Second
	}
	client := &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if m.TLSSkipVerify {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // #nosec G402 -- opt in per environment
	}
	start := time.Now()
	resp, err := client.Get(m.URL)
	res.LatencyMS = int(time.Since(start).Milliseconds())
	if err != nil {
		res.Error = "request failed"
	} else {
		res.StatusCode = resp.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
	}
	_ = s.send(protocol.TypeHealthResult, res)
}

// notifier wakes log streams that wait for a container to come back.
type notifier struct {
	mu sync.Mutex
	ch chan struct{}
}

func newNotifier() *notifier { return &notifier{ch: make(chan struct{})} }

func (n *notifier) wait() <-chan struct{} { n.mu.Lock(); defer n.mu.Unlock(); return n.ch }

func (n *notifier) broadcast() {
	n.mu.Lock()
	close(n.ch)
	n.ch = make(chan struct{})
	n.mu.Unlock()
}
