package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/agenthub"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/rbac"
	"github.com/poppyszn/helmsman/internal/store"
	"github.com/poppyszn/helmsman/pkg/protocol"
)

// inflight blocks a second restart of the same env while one runs (double click, two tabs).
type inflight struct {
	mu sync.Mutex
	m  map[string]bool
}

func (f *inflight) acquire(k string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[string]bool{}
	}
	if f.m[k] {
		return false
	}
	f.m[k] = true
	return true
}

func (f *inflight) release(k string) { f.mu.Lock(); delete(f.m, k); f.mu.Unlock() }

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	app, env := chi.URLParam(r, "app"), chi.URLParam(r, "env")
	e, err := s.Store.EnvByName(r.Context(), app, env)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	spec := parseSpec(e.Spec)
	if online, _ := s.Hub.Online(e.Agent); !online {
		writeErr(w, http.StatusConflict, "the agent is offline; restart is unavailable")
		return
	}
	key := app + "/" + env
	if !s.restarts.acquire(key) {
		writeErr(w, http.StatusConflict, "a restart of this environment is already in progress")
		return
	}
	defer s.restarts.release(key)

	user := rc(r).User.Login
	details := map[string]any{"tier": e.Tier, "service": spec.Container.Service, "compose_project": spec.Container.ComposeProject}
	// Record the request first. If the audit log cannot be written, nothing happens.
	if err := s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: user, Action: "action.restart.requested",
		Target: key, Result: "ok", IP: auth.ClientIP(r), Details: details}); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not write the audit log; restart not performed")
		return
	}
	res, err := s.Hub.Restart(r.Context(), agenthub.Target{AgentID: e.Agent,
		Selector: protocol.Selector{ComposeProject: spec.Container.ComposeProject, Service: spec.Container.Service}})
	result, status, msg := "ok", http.StatusOK, "restarted"
	switch {
	case err != nil:
		result, status, msg = "error", http.StatusBadGateway, "the agent could not be reached"
	case res.Status == protocol.ResultDenied:
		result, status, msg = "denied", http.StatusForbidden, "the agent refused: "+res.Message
	case res.Status != protocol.ResultOK:
		result, status, msg = "error", http.StatusBadGateway, "restart failed: "+res.Message
	}
	details["message"] = msg
	_ = s.Audit.Write(context.WithoutCancel(r.Context()), audit.Event{ActorType: audit.ActorUser, ActorID: user, Action: "action.restart",
		Target: key, Result: result, IP: auth.ClientIP(r), Details: details})
	if status != http.StatusOK {
		writeErr(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": msg})
}

type wsOut struct {
	Type    string             `json:"type"` // lines | closed
	Lines   []protocol.LogLine `json:"lines,omitempty"`
	Reason  string             `json:"reason,omitempty"`
	Message string             `json:"message,omitempty"`
}

// handleLogsWS streams container logs to the browser. Access is re-checked every minute
// so a removed binding or revoked session ends the stream.
func (s *Server) handleLogsWS(w http.ResponseWriter, r *http.Request) {
	app, env := chi.URLParam(r, "app"), chi.URLParam(r, "env")
	e, err := s.Store.EnvByName(r.Context(), app, env)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	spec := parseSpec(e.Spec)
	if online, _ := s.Hub.Online(e.Agent); !online {
		writeErr(w, http.StatusConflict, "the agent is offline; logs are unavailable")
		return
	}
	opts := agenthub.LogOptions{Tail: 200, Follow: r.URL.Query().Get("follow") != "false"}
	if v := r.URL.Query().Get("tail"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, "invalid tail")
			return
		}
		opts.Tail = min(n, protocol.MaxTail)
	}
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid since")
			return
		}
		opts.Since = &t
	}
	c := rc(r)
	// Opening a stream is audited (who looked at which env), never what the logs say.
	if err := s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: c.User.Login, Action: "logs.view",
		Target: app + "/" + env, Result: "ok", IP: auth.ClientIP(r), Details: map[string]any{"tier": e.Tier}}); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not write the audit log")
		return
	}
	stream, err := s.Logs.Tail(r.Context(), agenthub.Target{AgentID: e.Agent,
		Selector: protocol.Selector{ComposeProject: spec.Container.ComposeProject, Service: spec.Container.Service}}, opts)
	if err != nil {
		code, msg := http.StatusBadGateway, "could not open the log stream"
		if errors.Is(err, agenthub.ErrOffline) {
			code, msg = http.StatusConflict, "the agent is offline; logs are unavailable"
		} else if errors.Is(err, agenthub.ErrTooManyStreams) {
			code, msg = http.StatusTooManyRequests, "too many open log streams on this server; close another viewer and retry"
		}
		writeErr(w, code, msg)
		return
	}
	defer stream.Close()

	ws, err := websocket.Accept(w, r, nil) // same-origin check on by default
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = ws.CloseRead(ctx) // cancels when the browser goes away

	send := func(o wsOut) error {
		b, _ := json.Marshal(o)
		wctx, wc := context.WithTimeout(ctx, 10*time.Second)
		defer wc()
		return ws.Write(wctx, websocket.MessageText, b)
	}
	recheck := time.NewTicker(s.recheckEvery())
	defer recheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-recheck.C:
			if !s.stillAllowed(ctx, c, app, env) {
				_ = send(wsOut{Type: "closed", Reason: "access_revoked", Message: "your access to these logs ended"})
				_ = ws.Close(websocket.StatusPolicyViolation, "access revoked")
				return
			}
		case ev := <-stream.C:
			if ev.Closed {
				_ = send(wsOut{Type: "closed", Reason: ev.Reason, Message: ev.Message})
				_ = ws.Close(websocket.StatusNormalClosure, "closed")
				return
			}
			if err := send(wsOut{Type: "lines", Lines: ev.Lines}); err != nil {
				return
			}
		}
	}
}

func (s *Server) recheckEvery() time.Duration {
	if s.LogRecheck > 0 {
		return s.LogRecheck
	}
	return 60 * time.Second
}

// stillAllowed re-reads the session, the principal and the env. Errors fail closed.
func (s *Server) stillAllowed(ctx context.Context, c reqCtx, app, env string) bool {
	if _, err := s.Store.SessionUser(ctx, c.SessionHash); err != nil {
		return false
	}
	p, err := s.Store.Principal(ctx, c.User)
	if err != nil {
		return false
	}
	tier, err := s.Store.EnvTier(ctx, app, env)
	if err != nil {
		return false
	}
	return p.Allowed(rbac.LogsView, rbac.Scope{AppID: app, Env: env, Tier: tier})
}

var _ = store.ErrNotFound
