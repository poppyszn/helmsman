// Command loadtest drives a running Helmsman with simulated agents and log viewers.
//
//	go run ./tools/loadtest -url http://localhost:8080 -agents 20 -viewers 10 -duration 60s
//
// It signs in through fake-github (dev stack only), creates one agent and one app per simulated
// agent, writes a catalog for them, and then:
//   - connects every agent over the real WebSocket protocol, sending snapshots and answering health checks
//   - serves log streams from the agents at about 20 lines per second each
//   - opens one browser-style log viewer per app for -viewers apps
//   - halfway through, drops every agent connection at once and measures how long the fleet
//     takes to reconnect (the "IDP restart" case)
//
// It needs the dev stack started with deploy/compose.loadtest.yml so the server reads the catalog
// this tool writes. See docs/runbook.md for the full procedure and results.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/poppyszn/helmsman/pkg/protocol"
)

type cfg struct {
	base, user, catalogPath string
	agents, viewers         int
	dur                     time.Duration
}

type stats struct {
	connects, reconnects, snapshots, healthAnswered, logLinesServed atomic.Int64
	agentErrors                                                     atomic.Int64
	viewerLines, viewerErrors, viewerClosedEarly                    atomic.Int64
	mu                                                              sync.Mutex
	firstLine                                                       []time.Duration
	storm                                                           time.Duration
}

func main() {
	var c cfg
	flag.StringVar(&c.base, "url", "http://localhost:8080", "server base URL")
	flag.StringVar(&c.user, "user", "alice", "fake-github admin user to sign in as")
	flag.StringVar(&c.catalogPath, "catalog", "deploy/loadtest-catalog.yaml", "catalog file the server reads (see compose.loadtest.yml)")
	flag.IntVar(&c.agents, "agents", 20, "simulated agents")
	flag.IntVar(&c.viewers, "viewers", 10, "concurrent log viewers")
	flag.DurationVar(&c.dur, "duration", 60*time.Second, "steady-state duration")
	flag.Parse()
	if c.viewers > c.agents {
		c.viewers = c.agents
	}
	if err := run(c); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
}

type admin struct {
	base string
	hc   *http.Client
	csrf string
}

func login(base, user string) (*admin, error) {
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	next := func(u string) (string, error) {
		res, err := hc.Get(u)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		return res.Header.Get("Location"), nil
	}
	l, err := next(base + "/auth/github/login")
	if err != nil || l == "" {
		return nil, fmt.Errorf("login start: %v", err)
	}
	l, err = next(l + "&fake_user=" + url.QueryEscape(user))
	if err != nil || l == "" {
		return nil, fmt.Errorf("fake authorize (is the dev stack in fake mode?): %v", err)
	}
	if _, err = next(l); err != nil {
		return nil, err
	}
	a := &admin{base: base, hc: hc}
	var me struct {
		CSRF string `json:"csrf_token"`
	}
	if code, err := a.call("GET", "/api/me", nil, &me); err != nil || code != 200 {
		return nil, fmt.Errorf("sign-in failed: %d %v", code, err)
	}
	a.csrf = me.CSRF
	return a, nil
}

func (a *admin) call(method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, a.base+path, body)
	req.Header.Set("Content-Type", "application/json")
	if method != "GET" {
		req.Header.Set("X-CSRF-Token", a.csrf)
	}
	res, err := a.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode, nil
}

type simAgent struct {
	n          int
	id, cred   string
	project    string
	instance   string
	conn       atomic.Pointer[websocket.Conn]
	connectedC chan struct{}
}

func run(c cfg) error {
	a, err := login(c.base, c.user)
	if err != nil {
		return err
	}
	fmt.Printf("signed in as %s; creating %d agents\n", c.user, c.agents)
	agents := make([]*simAgent, c.agents)
	for i := range agents {
		ag := &simAgent{n: i + 1, project: "lt", instance: fmt.Sprintf("lt-%d-%d", i, time.Now().UnixNano()), connectedC: make(chan struct{}, 8)}
		ag.id = fmt.Sprintf("lt-agent-%02d", ag.n)
		agents[i] = ag
		var created struct {
			JoinToken string `json:"join_token"`
		}
		code, err := a.call("POST", "/api/admin/agents", map[string]string{"id": ag.id, "name": "Load test " + ag.id}, &created)
		if code == 409 {
			// A previous run left this agent behind. Issue a fresh join token for it if it never joined.
			return fmt.Errorf("agent %s already exists; reset the dev database (docker compose -f deploy/compose.dev.yml down -v) and retry", ag.id)
		}
		if err != nil || code >= 300 {
			return fmt.Errorf("create agent %s: %d %v", ag.id, code, err)
		}
		if err := ag.join(c.base, created.JoinToken); err != nil {
			return err
		}
		time.Sleep(60 * time.Millisecond) // stay under the per-user action limit
	}

	if err := writeCatalog(c, agents); err != nil {
		return err
	}
	if code, err := a.call("POST", "/api/admin/catalog/reload", map[string]string{}, nil); err != nil || code >= 300 {
		return fmt.Errorf("catalog reload: %d %v (is the server using %s? start it with deploy/compose.loadtest.yml)", code, err, c.catalogPath)
	}

	var st stats
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for _, ag := range agents {
		wg.Add(1)
		go func() { defer wg.Done(); ag.loop(ctx, c.base, &st) }()
	}
	if err := waitConnected(agents, 30*time.Second); err != nil {
		return err
	}
	fmt.Printf("all %d agents connected\n", c.agents)
	time.Sleep(3 * time.Second) // let the first snapshots land so the envs are online

	start := time.Now()
	for i := 0; i < c.viewers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); viewer(ctx, c, a, fmt.Sprintf("lt-app-%02d", i+1), &st) }()
	}
	fmt.Printf("%d viewers started; running for %s\n", c.viewers, c.dur)

	time.Sleep(c.dur / 2)
	// Fleet-wide drop, as when the server restarts.
	fmt.Println("dropping every agent connection at once")
	t0 := time.Now()
	for _, ag := range agents {
		for len(ag.connectedC) > 0 {
			<-ag.connectedC
		}
		if cn := ag.conn.Load(); cn != nil {
			_ = cn.CloseNow()
		}
	}
	if err := waitConnected(agents, 60*time.Second); err != nil {
		return fmt.Errorf("fleet did not reconnect: %w", err)
	}
	st.storm = time.Since(t0)
	fmt.Printf("fleet reconnected in %s\n", st.storm.Round(time.Millisecond))

	if rest := c.dur - time.Since(start); rest > 0 {
		time.Sleep(rest)
	}
	cancel()
	wg.Wait()
	report(c, &st, time.Since(start))
	return nil
}

func waitConnected(agents []*simAgent, d time.Duration) error {
	deadline := time.After(d)
	for _, ag := range agents {
		select {
		case <-ag.connectedC:
		case <-deadline:
			return fmt.Errorf("agent %s did not connect in %s", ag.id, d)
		}
	}
	return nil
}

func (ag *simAgent) join(base, token string) error {
	for try := 0; try < 40; try++ {
		b, _ := json.Marshal(map[string]string{"join_token": token, "hostname": ag.id, "version": "loadtest"})
		res, err := http.Post(base+"/agent/join", "application/json", bytes.NewReader(b))
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == http.StatusTooManyRequests {
			time.Sleep(3 * time.Second) // the join endpoint has its own per-IP limit
			continue
		}
		if res.StatusCode != 200 {
			return fmt.Errorf("join %s: %d %s", ag.id, res.StatusCode, body)
		}
		var out struct {
			Credential string `json:"credential"`
		}
		_ = json.Unmarshal(body, &out)
		ag.cred = out.Credential
		return nil
	}
	return fmt.Errorf("join %s: kept hitting the rate limit", ag.id)
}

func writeCatalog(c cfg, agents []*simAgent) error {
	var b strings.Builder
	b.WriteString("version: 1\napps:\n")
	for _, ag := range agents {
		fmt.Fprintf(&b, `  - id: lt-app-%02d
    name: Load test app %02d
    repo: acme-org/demo-app
    environments:
      - env: staging
        tier: nonprod
        agent: %s
        container: { compose_project: %s, service: app }
        url: http://localhost:9
        health: { url: "http://app:8080/health", via: agent }
        workflows: { migrate: migrate.yml }
        github_environment: staging
`, ag.n, ag.n, ag.id, ag.project)
	}
	return os.WriteFile(c.catalogPath, []byte(b.String()), 0o644)
}

// loop keeps one simulated agent connected, reconnecting with jittered backoff like the real one.
func (ag *simAgent) loop(ctx context.Context, base string, st *stats) {
	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/agent/ws"
	back := time.Second
	first := true
	for ctx.Err() == nil {
		err := ag.session(ctx, wsURL, st, first)
		first = false
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			st.agentErrors.Add(1)
		}
		time.Sleep(time.Duration(rand.Int64N(int64(back))) + 100*time.Millisecond)
		back = min(back*2, 8*time.Second)
	}
}

func (ag *simAgent) session(ctx context.Context, wsURL string, st *stats, first bool) error {
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + ag.cred}}})
	if err != nil {
		return err
	}
	conn.SetReadLimit(1 << 20)
	ag.conn.Store(conn)
	defer conn.CloseNow()
	var wmu sync.Mutex
	send := func(id, typ string, v any) error {
		var b []byte
		var err error
		if id == "" {
			b, err = protocol.Encode(typ, v)
		} else {
			b, err = protocol.EncodeWithID(id, typ, v)
		}
		if err != nil {
			return err
		}
		wmu.Lock()
		defer wmu.Unlock()
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return conn.Write(wctx, websocket.MessageText, b)
	}
	if err := send("", protocol.TypeHello, protocol.Hello{AgentVersion: "loadtest", ProtocolVersion: protocol.Version,
		Hostname: ag.id, DockerVersion: "sim", InstanceID: ag.instance}); err != nil {
		return err
	}
	snap := func() error {
		started := time.Now().Add(-time.Hour)
		st.snapshots.Add(1)
		return send("", protocol.TypeSnapshot, protocol.Snapshot{DockerOK: true, Containers: []protocol.Container{{
			ComposeProject: ag.project, Service: "app", ID: "sim-" + ag.id, Image: "sim:1", State: "running", StartedAt: &started}}})
	}
	if err := snap(); err != nil {
		return err
	}
	if first {
		st.connects.Add(1)
	} else {
		st.reconnects.Add(1)
	}
	ag.connectedC <- struct{}{}

	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { // snapshots double as the heartbeat
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-t.C:
				if snap() != nil {
					return
				}
			}
		}
	}()
	streams := sync.Map{}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		env, err := protocol.Decode(data)
		if err != nil {
			continue
		}
		switch env.Type {
		case protocol.TypeHealthCheck:
			var hc protocol.HealthCheck
			if protocol.Unmarshal(env, &hc) == nil {
				st.healthAnswered.Add(1)
				_ = send(env.ID, protocol.TypeHealthResult, protocol.HealthResult{AppEnvID: hc.AppEnvID, StatusCode: 200, LatencyMS: 4})
			}
		case protocol.TypeLogsOpen:
			var lo protocol.LogsOpen
			if protocol.Unmarshal(env, &lo) != nil {
				continue
			}
			lctx, lcancel := context.WithCancel(sctx)
			streams.Store(lo.StreamID, lcancel)
			go func() {
				defer lcancel()
				t := time.NewTicker(250 * time.Millisecond) // 4 chunks a second, 5 lines each: 20 lines per second
				defer t.Stop()
				n := 0
				for {
					select {
					case <-lctx.Done():
						return
					case <-t.C:
						lines := make([]protocol.LogLine, 5)
						for i := range lines {
							n++
							lines[i] = protocol.LogLine{TS: time.Now().UTC(), Stream: "stdout", Text: fmt.Sprintf("%s request %d handled in %dms", ag.id, n, rand.IntN(90))}
						}
						st.logLinesServed.Add(5)
						if send("", protocol.TypeLogsChunk, protocol.LogsChunk{StreamID: lo.StreamID, Lines: lines}) != nil {
							return
						}
					}
				}
			}()
		case protocol.TypeLogsClose:
			var lc protocol.LogsClose
			if protocol.Unmarshal(env, &lc) == nil {
				if f, ok := streams.LoadAndDelete(lc.StreamID); ok {
					f.(context.CancelFunc)()
				}
			}
		}
	}
}

func viewer(ctx context.Context, c cfg, a *admin, app string, st *stats) {
	u, _ := url.Parse(c.base)
	wsURL := "ws" + strings.TrimPrefix(c.base, "http") + "/ws/apps/" + app + "/envs/staging/logs?tail=20&follow=true"
	var cookies []string
	for _, ck := range a.hc.Jar.Cookies(u) {
		cookies = append(cookies, ck.Name+"="+ck.Value)
	}
	opened := time.Now()
	conn, res, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {strings.Join(cookies, "; ")}, "Origin": {c.base}}})
	if err != nil {
		code := 0
		if res != nil {
			code = res.StatusCode
		}
		fmt.Printf("viewer %s: open failed (%d): %v\n", app, code, err)
		st.viewerErrors.Add(1)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	gotFirst := false
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() == nil {
				st.viewerClosedEarly.Add(1)
			}
			return
		}
		var m struct {
			Type  string            `json:"type"`
			Lines []json.RawMessage `json:"lines"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.Type == "lines" {
			if !gotFirst && len(m.Lines) > 0 {
				gotFirst = true
				st.mu.Lock()
				st.firstLine = append(st.firstLine, time.Since(opened))
				st.mu.Unlock()
			}
			st.viewerLines.Add(int64(len(m.Lines)))
		}
	}
}

func pct(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[min(len(d)-1, int(float64(len(d))*p))]
}

func report(c cfg, st *stats, elapsed time.Duration) {
	fmt.Println("\n--- load test result ---")
	fmt.Printf("agents:            %d connected, %d reconnected after the drop, %d agent errors\n", st.connects.Load(), st.reconnects.Load(), st.agentErrors.Load())
	fmt.Printf("fleet reconnect:   %s for %d agents\n", st.storm.Round(time.Millisecond), c.agents)
	fmt.Printf("snapshots sent:    %d (%.1f per second)\n", st.snapshots.Load(), float64(st.snapshots.Load())/elapsed.Seconds())
	fmt.Printf("health answered:   %d\n", st.healthAnswered.Load())
	fmt.Printf("viewers:           %d, %d open failures, %d closed early\n", c.viewers, st.viewerErrors.Load(), st.viewerClosedEarly.Load())
	fmt.Printf("log lines:         %d served by agents, %d delivered to viewers (%.0f per second per viewer)\n",
		st.logLinesServed.Load(), st.viewerLines.Load(), float64(st.viewerLines.Load())/elapsed.Seconds()/float64(max(1, c.viewers)))
	fmt.Printf("first line:        p50 %s, p95 %s\n", pct(st.firstLine, 0.5).Round(time.Millisecond), pct(st.firstLine, 0.95).Round(time.Millisecond))
}
