// Package agenthub terminates agent WebSocket connections and brokers requests to them.
package agenthub

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/store"
	"github.com/poppyszn/helmsman/pkg/protocol"
)

const (
	// OfflineAfter is how long an agent may stay silent before it counts as offline.
	OfflineAfter = 60 * time.Second
	// MaxStreamsPerAgent bounds concurrent log streams through one agent.
	MaxStreamsPerAgent = 5
	// JoinTokenTTL is how long a join token stays valid.
	JoinTokenTTL = time.Hour
	// stale is the silence after which a second connection may take over without an alert.
	stale = 30 * time.Second
)

// Hub tracks live agent connections.
type Hub struct {
	St    *store.Store
	Audit *audit.Writer
	Log   *slog.Logger
	// SilenceLimit is how long an agent may stay silent before it counts as offline
	// (default OfflineAfter). Tests shorten it.
	SilenceLimit time.Duration

	mu    sync.Mutex
	conns map[string]*Conn
}

// New builds a hub.
func New(st *store.Store, a *audit.Writer, log *slog.Logger) *Hub {
	return &Hub{St: st, Audit: a, Log: log, conns: map[string]*Conn{}}
}

// Conn is one live agent connection.
type Conn struct {
	h           *Hub
	AgentID     string
	ws          *websocket.Conn
	ctx         context.Context
	cancel      context.CancelFunc
	Hello       protocol.Hello
	ConnectedAt time.Time
	lastMsg     atomic.Int64

	mu      sync.Mutex
	streams map[string]*Stream
	actions map[string]chan protocol.ActionResult
	health  map[int64]chan protocol.HealthResult
}

func (c *Conn) lastSeen() time.Time { return time.Unix(0, c.lastMsg.Load()) }

func (h *Hub) silence() time.Duration {
	if h != nil && h.SilenceLimit > 0 {
		return h.SilenceLimit
	}
	return OfflineAfter
}

// Online reports whether an agent has a live connection that spoke within OfflineAfter.
func (h *Hub) Online(agentID string) (bool, time.Time) {
	if h == nil {
		return false, time.Time{}
	}
	h.mu.Lock()
	c := h.conns[agentID]
	h.mu.Unlock()
	if c == nil {
		return false, time.Time{}
	}
	ls := c.lastSeen()
	return time.Since(ls) < h.silence(), ls
}

// Disconnect closes an agent's connection (used on revoke).
func (h *Hub) Disconnect(agentID string) {
	h.mu.Lock()
	c := h.conns[agentID]
	h.mu.Unlock()
	if c != nil {
		_ = c.ws.Close(websocket.StatusPolicyViolation, "revoked")
		c.cancel()
	}
}

// NewToken returns a random URL safe secret and its hash.
func NewToken() (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, auth.HashToken(token), nil
}

// HandleJoin exchanges a one-time join token for a long-lived credential.
func (h *Hub) HandleJoin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		JoinToken string `json:"join_token"`
		Hostname  string `json:"hostname"`
		Version   string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil || in.JoinToken == "" {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	cred, credHash, err := NewToken()
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	id, err := h.St.ExchangeJoin(r.Context(), auth.HashToken(in.JoinToken), credHash, clip(in.Hostname, 200), clip(in.Version, 50))
	if errors.Is(err, store.ErrNotFound) {
		_ = h.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorSystem, ActorID: "agent-join", Action: "agent.join",
			Result: "denied", IP: auth.ClientIP(r), Details: map[string]any{"reason": "invalid or expired join token"}})
		http.Error(w, `{"error":"join token is invalid or expired"}`, http.StatusUnauthorized)
		return
	} else if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	_ = h.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorAgent, ActorID: id, Action: "agent.join",
		Target: id, Result: "ok", IP: auth.ClientIP(r), Details: map[string]any{"hostname": in.Hostname}})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"agent_id": id, "credential": cred})
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ServeWS accepts an agent connection at /agent/ws.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	agent, err := h.St.AgentByCredential(r.Context(), auth.HashToken(tok))
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(4 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{h: h, AgentID: agent.ID, ws: ws, ctx: ctx, cancel: cancel, ConnectedAt: time.Now(),
		streams: map[string]*Stream{}, actions: map[string]chan protocol.ActionResult{}, health: map[int64]chan protocol.HealthResult{}}
	c.lastMsg.Store(time.Now().UnixNano())
	defer func() {
		cancel()
		_ = ws.CloseNow()
	}()

	// The first message must be hello.
	hctx, hcancel := context.WithTimeout(ctx, 10*time.Second)
	_, b, err := ws.Read(hctx)
	hcancel()
	if err != nil {
		return
	}
	env, err := protocol.Decode(b)
	if err != nil || env.Type != protocol.TypeHello {
		c.sendError(protocol.ErrCodeBadMessage, "expected hello", env.ID)
		_ = ws.Close(websocket.StatusProtocolError, "expected hello")
		return
	}
	if err := protocol.Unmarshal(env, &c.Hello); err != nil {
		_ = ws.Close(websocket.StatusProtocolError, "bad hello")
		return
	}
	if !h.register(c) {
		return
	}
	defer h.unregister(c)
	_ = h.St.AgentHello(ctx, agent.ID, c.Hello)
	h.Log.Info("agent connected", "agent", agent.ID, "version", c.Hello.AgentVersion, "host", c.Hello.Hostname)

	go c.watchdog()
	c.readLoop()
}

// register enforces one live connection per agent. A reconnect from the same process, or a
// takeover of a stale connection, replaces the old one. A second live process with the same
// credential is rejected and raises an admin alert.
func (h *Hub) register(c *Conn) bool {
	h.mu.Lock()
	old := h.conns[c.AgentID]
	if old != nil && old.Hello.InstanceID != c.Hello.InstanceID && time.Since(old.lastSeen()) < stale {
		h.mu.Unlock()
		msg := "a second agent process connected with the same credential and was rejected (cloned image or copied credential?)"
		c.sendError(protocol.ErrCodeDuplicate, "this credential is already in use by another agent", "")
		_ = c.ws.Close(websocket.StatusPolicyViolation, "credential already in use")
		ctx := context.Background()
		_ = h.St.SetAgentAlert(ctx, c.AgentID, msg)
		_ = h.Audit.Write(ctx, audit.Event{ActorType: audit.ActorAgent, ActorID: c.AgentID, Action: "agent.duplicate_credential",
			Target: c.AgentID, Result: "denied", Details: map[string]any{"hostname": c.Hello.Hostname}})
		h.Log.Warn("duplicate agent credential rejected", "agent", c.AgentID, "host", c.Hello.Hostname)
		return false
	}
	h.conns[c.AgentID] = c
	h.mu.Unlock()
	if old != nil {
		old.cancel()
		_ = old.ws.CloseNow()
	}
	return true
}

func (h *Hub) unregister(c *Conn) {
	h.mu.Lock()
	if h.conns[c.AgentID] == c {
		delete(h.conns, c.AgentID)
	}
	h.mu.Unlock()
	c.closeAll()
	_ = h.St.TouchAgent(context.Background(), c.AgentID)
	h.Log.Info("agent disconnected", "agent", c.AgentID)
}

// watchdog pings the agent and closes the connection when it goes quiet.
func (c *Conn) watchdog() {
	t := time.NewTicker(min(20*time.Second, c.h.silence()/2))
	defer t.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-t.C:
			if time.Since(c.lastSeen()) > c.h.silence() {
				c.h.Log.Warn("agent silent, closing", "agent", c.AgentID)
				_ = c.ws.Close(websocket.StatusGoingAway, "silent")
				c.cancel()
				return
			}
		}
	}
}

func (c *Conn) readLoop() {
	for {
		rctx, cancel := context.WithTimeout(c.ctx, c.h.silence())
		_, b, err := c.ws.Read(rctx)
		cancel()
		if err != nil {
			return
		}
		c.lastMsg.Store(time.Now().UnixNano())
		env, err := protocol.Decode(b)
		if err != nil {
			c.sendError(protocol.ErrCodeUnsupported, err.Error(), env.ID)
			continue
		}
		c.handle(env)
	}
}

func (c *Conn) handle(env protocol.Envelope) {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	switch env.Type {
	case protocol.TypeSnapshot:
		var m protocol.Snapshot
		if protocol.Unmarshal(env, &m) == nil {
			if err := c.h.St.SaveSnapshot(ctx, c.AgentID, m); err != nil {
				c.h.Log.Error("save snapshot", "agent", c.AgentID, "err", err)
			}
		}
	case protocol.TypeEvent:
		var m protocol.Event
		if protocol.Unmarshal(env, &m) == nil {
			if err := c.h.St.AddEvent(ctx, c.AgentID, m); err != nil {
				c.h.Log.Error("save event", "agent", c.AgentID, "err", err)
			}
		}
	case protocol.TypeLogsChunk:
		var m protocol.LogsChunk
		if protocol.Unmarshal(env, &m) == nil {
			if s := c.stream(m.StreamID); s != nil {
				s.deliver(m.Lines)
			}
		}
	case protocol.TypeLogsClose:
		var m protocol.LogsClose
		if protocol.Unmarshal(env, &m) == nil {
			if s := c.stream(m.StreamID); s != nil {
				s.finish(m.Reason, m.Message)
				c.removeStream(m.StreamID)
			}
		}
	case protocol.TypeHealthResult:
		var m protocol.HealthResult
		if protocol.Unmarshal(env, &m) == nil {
			c.mu.Lock()
			ch := c.health[m.AppEnvID]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- m:
				default:
				}
			}
		}
	case protocol.TypeActionResult:
		var m protocol.ActionResult
		if protocol.Unmarshal(env, &m) == nil {
			c.mu.Lock()
			ch := c.actions[m.RequestID]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- m:
				default:
				}
			}
		}
	case protocol.TypeError:
		var m protocol.ErrorMsg
		if protocol.Unmarshal(env, &m) == nil {
			c.h.Log.Warn("agent reported an error", "agent", c.AgentID, "code", m.Code, "message", m.Message)
		}
	case protocol.TypeHello:
		// Ignore repeated hello.
	default:
		c.sendError(protocol.ErrCodeUnsupported, "unknown message type", env.ID)
	}
}

func (c *Conn) send(typ string, payload any) error {
	b, err := protocol.Encode(typ, payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, b)
}

func (c *Conn) sendError(code, msg, related string) {
	_ = c.send(protocol.TypeError, protocol.ErrorMsg{Code: code, Message: msg, RelatedID: related})
}

func (c *Conn) closeAll() {
	c.mu.Lock()
	streams := c.streams
	c.streams = map[string]*Stream{}
	c.mu.Unlock()
	for _, s := range streams {
		s.finish("agent_disconnected", "the agent disconnected")
	}
}
