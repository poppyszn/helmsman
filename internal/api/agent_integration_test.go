package api_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/poppyszn/helmsman/internal/agent"
	"github.com/poppyszn/helmsman/internal/agenthub"
	"github.com/poppyszn/helmsman/pkg/protocol"
)

// fakeDocker is a tiny Docker daemon on a unix socket.
type fakeDocker struct {
	mu        sync.Mutex
	id        string
	started   time.Time
	logs      map[string][]string
	subs      map[string][]chan string
	ends      map[string][]chan struct{}
	restarts  []string
	events    chan string
	logDriver string
	srv       *http.Server
}

func newFakeDocker(t *testing.T) (*fakeDocker, string) {
	t.Helper()
	// Unix socket paths are short-limited; use a short temp dir.
	dir, err := os.MkdirTemp("/tmp", "fd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	fd := &fakeDocker{id: "c1", started: time.Now().Add(-time.Hour).UTC(), logs: map[string][]string{"c1": {"hello from docker", "second line"}},
		subs: map[string][]chan string{}, ends: map[string][]chan struct{}{}, events: make(chan string, 16), logDriver: "json-file"}
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"Version":"27.0.1"}`) })
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		fd.mu.Lock()
		defer fd.mu.Unlock()
		fmt.Fprintf(w, `[{"Id":%q,"State":"running","Created":1,"Labels":{"com.docker.compose.project":"billing","com.docker.compose.service":"api"}},
		{"Id":"other","State":"running","Created":1,"Labels":{"com.docker.compose.project":"secret","com.docker.compose.service":"db"}}]`, fd.id)
	})
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/containers/"), "/")
		id, op := parts[0], parts[1]
		switch op {
		case "json":
			fd.mu.Lock()
			defer fd.mu.Unlock()
			fmt.Fprintf(w, `{"Id":%q,"RestartCount":2,"State":{"Status":"running","StartedAt":%q,"Health":{"Status":"healthy"}},
			"Config":{"Image":"ghcr.io/acme/billing:abc123","Tty":false,"Labels":{"org.opencontainers.image.revision":"abc123"}},
			"HostConfig":{"LogConfig":{"Type":%q}}}`, id, fd.started.Format(time.RFC3339Nano), fd.logDriver)
		case "restart":
			fd.mu.Lock()
			fd.restarts = append(fd.restarts, id)
			fd.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case "logs":
			fd.serveLogs(w, r, id)
		}
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case ev := <-fd.events:
				fmt.Fprintln(w, ev)
				w.(http.Flusher).Flush()
			}
		}
	})
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	fd.srv = &http.Server{Handler: mux}
	go fd.srv.Serve(l)
	t.Cleanup(func() { fd.srv.Close() })
	return fd, sock
}

func frame(stream byte, text string) []byte {
	b := make([]byte, 8, 8+len(text))
	b[0] = stream
	binary.BigEndian.PutUint32(b[4:], uint32(len(text)))
	return append(b, text...)
}

func (fd *fakeDocker) serveLogs(w http.ResponseWriter, r *http.Request, id string) {
	fd.mu.Lock()
	lines := append([]string(nil), fd.logs[id]...)
	var sub chan string
	var end chan struct{}
	if r.URL.Query().Get("follow") == "1" {
		sub, end = make(chan string, 16), make(chan struct{})
		fd.subs[id] = append(fd.subs[id], sub)
		fd.ends[id] = append(fd.ends[id], end)
	}
	fd.mu.Unlock()
	w.WriteHeader(200)
	ts := func() string { return time.Now().UTC().Format(time.RFC3339Nano) }
	for _, l := range lines {
		_, _ = w.Write(frame(1, ts()+" "+l+"\n"))
	}
	w.(http.Flusher).Flush()
	if sub == nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-end:
			return
		case l := <-sub:
			_, _ = w.Write(frame(1, ts()+" "+l+"\n"))
			w.(http.Flusher).Flush()
		}
	}
}

func (fd *fakeDocker) emit(id, line string) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	for _, s := range fd.subs[id] {
		s <- line
	}
}

// replace simulates a deploy: a new container id, the old log stream ends, a start event fires.
func (fd *fakeDocker) replace(newID string, lines ...string) {
	fd.mu.Lock()
	old := fd.id
	fd.id = newID
	fd.logs[newID] = lines
	for _, e := range fd.ends[old] {
		close(e)
	}
	fd.ends[old] = nil
	fd.mu.Unlock()
	fd.events <- fmt.Sprintf(`{"Action":"start","Actor":{"ID":%q,"Attributes":{"com.docker.compose.project":"billing","com.docker.compose.service":"api"}},"timeNano":%d}`, newID, time.Now().UnixNano())
}

func (fd *fakeDocker) restartCalls() []string {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	return append([]string(nil), fd.restarts...)
}

type agentRig struct {
	e      *env
	fd     *fakeDocker
	cancel context.CancelFunc
	data   string
	done   chan struct{}
}

// startAgent joins agent-1 (if needed) and runs a real agent against the fake Docker.
func (e *env) startAgent(fd *fakeDocker, sock, data string, allow []agent.Allow) *agentRig {
	e.t.Helper()
	token, hash, _ := agenthub.NewToken()
	if _, err := os.Stat(filepath.Join(data, "credential")); err != nil {
		if err := e.st.ReissueJoin(context.Background(), "agent-1", hash, time.Now().Add(time.Hour)); err != nil {
			e.t.Fatal(err)
		}
	}
	cfg := agent.Config{IDPURL: "ws" + strings.TrimPrefix(e.idp.URL, "http") + "/agent/ws", JoinToken: token, DataDir: data, DockerSocket: sock,
		AllowInsecure: true, Allow: allow, SnapshotInterval: 30 * time.Second}
	cfg.Limits = agent.Limits{MaxStreams: 5, MaxLinesPerSec: 500, MaxLineBytes: 16384}
	ctx, cancel := context.WithCancel(context.Background())
	r := &agentRig{e: e, fd: fd, cancel: cancel, data: data, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		_ = agent.New(cfg, "test", slog.New(slog.NewTextHandler(io.Discard, nil))).Run(ctx)
	}()
	e.t.Cleanup(func() { cancel(); <-r.done })
	return r
}

func (r *agentRig) stop() { r.cancel(); <-r.done }

var billingAllow = []agent.Allow{{ComposeProject: "billing", Services: []string{"api"}, Actions: []string{"logs", "restart"}}}

func waitFor(t *testing.T, what string, d time.Duration, f func() bool) {
	t.Helper()
	dl := time.Now().Add(d)
	for time.Now().Before(dl) {
		if f() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type statusResp struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
	Agent  struct {
		Online   bool `json:"online"`
		Outdated bool `json:"outdated"`
	} `json:"agent"`
	Container *struct {
		State        string `json:"state"`
		Revision     string `json:"revision"`
		RestartCount int    `json:"restart_count"`
	} `json:"container"`
	LastRestart *time.Time `json:"last_restart"`
	CrashLoop   bool       `json:"crash_loop"`
	CanLogs     bool       `json:"can_logs"`
	CanRestart  bool       `json:"can_restart"`
	Blocked     struct{ Logs, Restart string }
	Events      []struct{ Event string } `json:"events"`
}

func (c *client) status(app, envName string) statusResp {
	c.e.t.Helper()
	var s statusResp
	if code := c.json("GET", "/api/apps/"+app+"/envs/"+envName+"/status", nil, &s); code != 200 {
		c.e.t.Fatalf("status %d", code)
	}
	return s
}

// logsWS opens the browser log socket with the client's session cookie.
func (c *client) logsWS(app, envName, query string) (*websocket.Conn, *http.Response, error) {
	u, _ := http.NewRequest("GET", c.e.idp.URL, nil)
	var cookies []string
	for _, ck := range c.http.Jar.Cookies(u.URL) {
		cookies = append(cookies, ck.Name+"="+ck.Value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.e.idp.URL, "http")+"/ws/apps/"+app+"/envs/"+envName+"/logs"+query,
		&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {strings.Join(cookies, "; ")}}})
}

type logMsg struct {
	Type    string `json:"type"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Lines   []struct {
		Text   string `json:"text"`
		Marker bool   `json:"marker"`
	} `json:"lines"`
}

// readUntil reads log messages until pred is true for the accumulated text.
func readUntil(t *testing.T, ws *websocket.Conn, d time.Duration, pred func(all string, last logMsg) bool) (string, logMsg) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	var all strings.Builder
	var last logMsg
	for {
		_, b, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("log socket: %v (got %q)", err, all.String())
		}
		last = logMsg{}
		_ = json.Unmarshal(b, &last)
		for _, l := range last.Lines {
			all.WriteString(l.Text + "\n")
		}
		if pred(all.String(), last) {
			return all.String(), last
		}
	}
}

func TestAgentEndToEnd(t *testing.T) {
	e := newEnv(t)
	fd, sock := newFakeDocker(t)
	alice := e.client()
	alice.login("alice")

	// Before any agent connects, the env is unknown and actions explain why.
	s := alice.status("billing-api", "staging")
	if s.Status != "unknown" || s.Reason != "agent_offline" || s.Agent.Online || s.Blocked.Logs == "" || s.Blocked.Restart == "" {
		t.Fatalf("offline status: %+v", s)
	}
	if code := alice.json("POST", "/api/apps/billing-api/envs/staging/restart", nil, nil); code != 409 {
		t.Fatalf("restart while offline: %d", code)
	}

	rig := e.startAgent(fd, sock, t.TempDir(), billingAllow)
	waitFor(t, "agent online with a snapshot", 10*time.Second, func() bool {
		s := alice.status("billing-api", "staging")
		return s.Agent.Online && s.Container != nil
	})
	s = alice.status("billing-api", "staging")
	if s.Status != "healthy" || s.Container.State != "running" || s.Container.Revision != "abc123" || s.Container.RestartCount != 2 || s.LastRestart == nil || s.Agent.Outdated {
		t.Fatalf("status: %+v", s)
	}
	if !s.CanLogs || !s.CanRestart || s.Blocked.Logs != "" {
		t.Fatalf("alice should be able to tail and restart: %+v", s)
	}
	// The agent's allowlist hides everything else on the host.
	var n int
	_ = e.st.Pool.QueryRow(context.Background(), `SELECT count(*) FROM container_state WHERE compose_project='secret'`).Scan(&n)
	if n != 0 {
		t.Fatal("container outside the allowlist leaked into the snapshot")
	}

	// Logs: history first, then new lines quickly.
	ws, _, err := alice.logsWS("billing-api", "staging", "?tail=10")
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, ws, 5*time.Second, func(all string, _ logMsg) bool {
		return strings.Contains(all, "hello from docker") && strings.Contains(all, "second line")
	})
	start := time.Now()
	fd.emit("c1", "live line 1")
	readUntil(t, ws, 5*time.Second, func(all string, _ logMsg) bool { return strings.Contains(all, "live line 1") })
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("tail latency %v, want under 2s", d)
	}

	// A deploy recreates the container: the stream reattaches and says so.
	fd.replace("c2", "new container boot")
	all, _ := readUntil(t, ws, 10*time.Second, func(all string, _ logMsg) bool { return strings.Contains(all, "new container boot") })
	if !strings.Contains(all, "container replaced") {
		t.Fatalf("no reattach marker in %q", all)
	}
	ws.CloseNow()

	// Restart: audited before and after, performed by the agent on the right container.
	if code := alice.json("POST", "/api/apps/billing-api/envs/staging/restart", nil, nil); code != 200 {
		t.Fatalf("restart %d", code)
	}
	if calls := fd.restartCalls(); len(calls) != 1 || calls[0] != "c2" {
		t.Fatalf("restart calls %v", calls)
	}
	var audits []string
	rows, _ := e.st.Pool.Query(context.Background(), `SELECT action||':'||result FROM audit_events WHERE action LIKE 'action.restart%' ORDER BY id`)
	for rows.Next() {
		var a string
		_ = rows.Scan(&a)
		audits = append(audits, a)
	}
	rows.Close()
	if strings.Join(audits, ",") != "action.restart.requested:ok,action.restart:ok" {
		t.Fatalf("audit trail %v", audits)
	}

	// The agent enforces its own allowlist even when the server asks for something else.
	res, err := e.hub.Restart(context.Background(), agenthub.Target{AgentID: "agent-1", Selector: protocol.Selector{ComposeProject: "secret", Service: "db"}})
	if err != nil || res.Status != protocol.ResultDenied {
		t.Fatalf("agent must refuse outside its allowlist: %+v %v", res, err)
	}
	if len(fd.restartCalls()) != 1 {
		t.Fatal("agent restarted a container outside its allowlist")
	}
	st, err := e.hub.Tail(context.Background(), agenthub.Target{AgentID: "agent-1", Selector: protocol.Selector{ComposeProject: "secret", Service: "db"}}, agenthub.LogOptions{Tail: 5})
	if err != nil {
		t.Fatal(err)
	}
	if ev := <-st.C; !ev.Closed || ev.Reason != protocol.CloseNotAllowed {
		t.Fatalf("logs outside allowlist: %+v", ev)
	}

	// Offline detection: when the agent stops, status flips and actions are refused.
	rig.stop()
	waitFor(t, "agent offline", 5*time.Second, func() bool { return !alice.status("billing-api", "staging").Agent.Online })
	s = alice.status("billing-api", "staging")
	if s.Status != "unknown" || s.Reason != "agent_offline" || s.Blocked.Logs == "" {
		t.Fatalf("offline status: %+v", s)
	}
	if _, _, err := alice.logsWS("billing-api", "staging", ""); err == nil {
		t.Fatal("log socket must refuse while the agent is offline")
	}
}

func TestLogsRequirePermissionAndStopWhenAccessIsRemoved(t *testing.T) {
	e := newEnv(t)
	fd, sock := newFakeDocker(t)
	e.startAgent(fd, sock, t.TempDir(), billingAllow)
	alice, bob := e.client(), e.client()
	alice.login("alice")
	bob.login("bob")
	waitFor(t, "agent", 10*time.Second, func() bool { return alice.status("billing-api", "staging").Container != nil })

	if _, resp, err := bob.logsWS("billing-api", "staging", ""); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("bob without a binding: %v %v", err, resp)
	}
	if code := bob.json("POST", "/api/apps/billing-api/envs/staging/restart", nil, nil); code != 403 {
		t.Fatalf("restart without permission: %d", code)
	}

	e.bind("user", fmt.Sprint(e.userID("bob")), "developer", "billing-api", "staging")
	ws, _, err := bob.logsWS("billing-api", "staging", "?tail=5")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	readUntil(t, ws, 5*time.Second, func(all string, _ logMsg) bool { return strings.Contains(all, "hello from docker") })
	// bob's role is developer on staging only: prod stays invisible.
	if _, resp, err := bob.logsWS("billing-api", "prod", ""); err == nil || resp.StatusCode != 403 {
		t.Fatalf("prod logs for a staging-only developer: %v", err)
	}

	// Remove the binding while the stream is open: it must close.
	if _, err := e.st.Pool.Exec(context.Background(), `DELETE FROM role_bindings WHERE subject_type='user'`); err != nil {
		t.Fatal(err)
	}
	_, last := readUntil(t, ws, 5*time.Second, func(_ string, m logMsg) bool { return m.Type == "closed" })
	if last.Reason != "access_revoked" {
		t.Fatalf("closed with %q", last.Reason)
	}
}

func TestAgentJoinRevokeAndSharedCredential(t *testing.T) {
	e := newEnv(t)
	fd, sock := newFakeDocker(t)
	alice, bob := e.client(), e.client()
	alice.login("alice")
	bob.login("bob")

	// Only admins manage agents.
	if code := bob.json("POST", "/api/admin/agents", map[string]string{"id": "x", "name": "x"}, nil); code != 403 {
		t.Fatalf("non-admin create: %d", code)
	}
	var created struct {
		ID    string `json:"id"`
		Token string `json:"join_token"`
	}
	if code := alice.json("POST", "/api/admin/agents", map[string]string{"id": "edge-1", "name": "Edge 1"}, &created); code != 201 || created.Token == "" {
		t.Fatalf("create: %d %+v", code, created)
	}
	if code := alice.json("POST", "/api/admin/agents", map[string]string{"id": "edge-1", "name": "dup"}, nil); code != 409 {
		t.Fatalf("duplicate id: %d", code)
	}
	if code := alice.json("POST", "/api/admin/agents", map[string]string{"id": "Bad Id", "name": "x"}, nil); code != 422 {
		t.Fatalf("bad id: %d", code)
	}
	// The token is stored hashed.
	var stored []byte
	_ = e.st.Pool.QueryRow(context.Background(), `SELECT join_token_hash FROM agents WHERE id='edge-1'`).Scan(&stored)
	if len(stored) != 32 || strings.Contains(string(stored), created.Token) {
		t.Fatal("join token must be stored as a hash")
	}
	// A wrong token is refused; the real one works exactly once.
	join := func(tok string) int {
		resp, err := http.Post(e.idp.URL+"/agent/join", "application/json", strings.NewReader(`{"join_token":"`+tok+`","hostname":"h","version":"t"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if join("nope") != 401 {
		t.Fatal("bad join token accepted")
	}
	// An expired token is refused too.
	_, _ = e.st.Pool.Exec(context.Background(), `UPDATE agents SET join_expires_at = now() - interval '1 minute' WHERE id='edge-1'`)
	if join(created.Token) != 401 {
		t.Fatal("expired join token accepted")
	}
	if code := alice.json("POST", "/api/admin/agents/edge-1/join-token", nil, &created); code != 200 {
		t.Fatalf("reissue: %d", code)
	}
	if join(created.Token) != 200 || join(created.Token) != 401 {
		t.Fatal("join token must work exactly once")
	}

	// Run agent-1 and clone its credential onto a second host.
	rig := e.startAgent(fd, sock, t.TempDir(), billingAllow)
	waitFor(t, "agent", 10*time.Second, func() bool { return alice.status("billing-api", "staging").Agent.Online })
	cred, err := os.ReadFile(filepath.Join(rig.data, "credential"))
	if err != nil {
		t.Fatal(err)
	}
	cloneDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cloneDir, "credential"), cred, 0o600); err != nil {
		t.Fatal(err)
	}
	e.startAgent(fd, sock, cloneDir, billingAllow)
	waitFor(t, "duplicate alert", 10*time.Second, func() bool {
		var agents []struct {
			ID    string  `json:"id"`
			Alert *string `json:"alert"`
		}
		alice.json("GET", "/api/admin/agents", nil, &agents)
		for _, a := range agents {
			if a.ID == "agent-1" && a.Alert != nil {
				return true
			}
		}
		return false
	})
	if !alice.status("billing-api", "staging").Agent.Online {
		t.Fatal("the original agent must stay connected")
	}

	// Revoking closes the socket and the credential stops working.
	if code := alice.json("POST", "/api/admin/agents/agent-1/revoke", nil, nil); code != 204 {
		t.Fatalf("revoke %d", code)
	}
	waitFor(t, "revoked agent offline", 5*time.Second, func() bool { return !alice.status("billing-api", "staging").Agent.Online })
	time.Sleep(1500 * time.Millisecond) // the agents retry with backoff; neither may get back in
	if alice.status("billing-api", "staging").Agent.Online {
		t.Fatal("revoked credential reconnected")
	}
	var revoked int
	_ = e.st.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action IN ('agent.create','agent.join','agent.revoke','agent.duplicate_credential')`).Scan(&revoked)
	if revoked < 4 {
		t.Fatalf("agent lifecycle must be audited, got %d", revoked)
	}
}

func TestLogsUnavailableForNonFileDriver(t *testing.T) {
	e := newEnv(t)
	fd, sock := newFakeDocker(t)
	fd.logDriver = "awslogs"
	e.startAgent(fd, sock, t.TempDir(), billingAllow)
	alice := e.client()
	alice.login("alice")
	waitFor(t, "agent", 10*time.Second, func() bool { return alice.status("billing-api", "staging").Container != nil })
	ws, _, err := alice.logsWS("billing-api", "staging", "")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	_, last := readUntil(t, ws, 5*time.Second, func(_ string, m logMsg) bool { return m.Type == "closed" })
	if last.Reason != protocol.CloseLogsUnavailable || !strings.Contains(last.Message, "not available through Docker") {
		t.Fatalf("closed with %+v", last)
	}
}

// An agent that connects and then goes silent (dead network, frozen host) is offline
// after the silence limit, without any close frame.
func TestSilentAgentIsMarkedOffline(t *testing.T) {
	e := newEnv(t)
	e.hub.SilenceLimit = 1500 * time.Millisecond
	alice := e.client()
	alice.login("alice")
	cred, credHash, _ := agenthub.NewToken()
	if _, err := e.st.Pool.Exec(context.Background(), `UPDATE agents SET credential_hash=$1 WHERE id='agent-1'`, credHash); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.idp.URL, "http")+"/agent/ws",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + cred}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	hello, _ := protocol.Encode(protocol.TypeHello, protocol.Hello{AgentVersion: "t", ProtocolVersion: 1, InstanceID: "x"})
	if err := ws.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "online", 3*time.Second, func() bool { return alice.status("billing-api", "staging").Agent.Online })
	go func() { _, _, _ = ws.Read(ctx) }() // keep answering control frames, send nothing
	waitFor(t, "offline after silence", 5*time.Second, func() bool { return !alice.status("billing-api", "staging").Agent.Online })
}

func TestLogsWebSocketRejectsOtherOrigins(t *testing.T) {
	e := newEnv(t)
	fd, sock := newFakeDocker(t)
	e.startAgent(fd, sock, t.TempDir(), billingAllow)
	alice := e.client()
	alice.login("alice")
	waitFor(t, "agent", 10*time.Second, func() bool { return alice.status("billing-api", "staging").Container != nil })

	u, _ := http.NewRequest("GET", e.idp.URL, nil)
	var cookies []string
	for _, ck := range alice.http.Jar.Cookies(u.URL) {
		cookies = append(cookies, ck.Name+"="+ck.Value)
	}
	dial := func(origin string) (*http.Response, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hdr := http.Header{"Cookie": {strings.Join(cookies, "; ")}}
		if origin != "" {
			hdr.Set("Origin", origin)
		}
		c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.idp.URL, "http")+"/ws/apps/billing-api/envs/staging/logs?tail=1", &websocket.DialOptions{HTTPHeader: hdr})
		if c != nil {
			_ = c.CloseNow()
		}
		return resp, err
	}
	// A page on another site, holding the user's cookie, must not be able to read logs.
	if resp, err := dial("https://evil.example"); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("cross-origin socket must be refused with 403: %v %v", err, resp)
	}
	if _, err := dial(e.idp.URL); err != nil {
		t.Fatalf("same-origin must work: %v", err)
	}
}
