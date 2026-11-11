package agenthub

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/poppyszn/helmsman/pkg/protocol"
)

// Errors returned to callers.
var (
	ErrOffline        = errors.New("agent is offline")
	ErrTooManyStreams = errors.New("too many open log streams on this agent")
)

// Target names a service on an agent by compose labels.
type Target struct {
	AgentID string
	protocol.Selector
}

// LogOptions mirror logs.open.
type LogOptions struct {
	Tail   int
	Since  *time.Time
	Follow bool
}

// LogEvent is what a log consumer receives: lines, or the end of the stream.
type LogEvent struct {
	Lines   []protocol.LogLine
	Closed  bool
	Reason  string
	Message string
}

// LogSource reads container logs. The agent backed implementation is the Hub; Loki or
// CloudWatch sources can be added per environment later without touching the API layer.
type LogSource interface {
	// Tail opens a stream. Close the returned Stream when done.
	Tail(ctx context.Context, t Target, o LogOptions) (*Stream, error)
	// Range returns lines since a point in time without following.
	Range(ctx context.Context, t Target, since time.Time, limit int) ([]protocol.LogLine, error)
}

var _ LogSource = (*Hub)(nil)

// Stream is one open log stream.
type Stream struct {
	ID string
	C  chan LogEvent

	c       *Conn
	mu      sync.Mutex
	closed  bool
	dropped int
}

// Close tells the agent to stop and releases the stream.
func (s *Stream) Close() {
	s.c.removeStream(s.ID)
	s.mu.Lock()
	already := s.closed
	s.closed = true
	s.mu.Unlock()
	if !already {
		_ = s.c.send(protocol.TypeLogsClose, protocol.LogsClose{StreamID: s.ID, Reason: protocol.CloseRequested})
	}
}

// deliver never blocks the agent read loop: a slow consumer loses chunks and gets a marker.
func (s *Stream) deliver(lines []protocol.LogLine) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if s.dropped > 0 {
		lines = append([]protocol.LogLine{{TS: time.Now().UTC(), Stream: "stderr", Marker: true,
			Text: fmt.Sprintf("-- %d log batches dropped, the viewer was too slow --", s.dropped)}}, lines...)
	}
	select {
	case s.C <- LogEvent{Lines: lines}:
		s.dropped = 0
	default:
		s.dropped++
	}
	s.mu.Unlock()
}

func (s *Stream) finish(reason, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	// The buffer may be full; make room so the close is never lost.
	for {
		select {
		case s.C <- LogEvent{Closed: true, Reason: reason, Message: msg}:
			return
		default:
			select {
			case <-s.C:
			default:
			}
		}
	}
}

func (c *Conn) stream(id string) *Stream {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streams[id]
}

func (c *Conn) removeStream(id string) {
	c.mu.Lock()
	delete(c.streams, id)
	c.mu.Unlock()
}

func (h *Hub) conn(agentID string) (*Conn, error) {
	h.mu.Lock()
	c := h.conns[agentID]
	h.mu.Unlock()
	if c == nil || time.Since(c.lastSeen()) >= h.silence() {
		return nil, ErrOffline
	}
	return c, nil
}

// Tail implements LogSource.
func (h *Hub) Tail(_ context.Context, t Target, o LogOptions) (*Stream, error) {
	c, err := h.conn(t.AgentID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if len(c.streams) >= MaxStreamsPerAgent {
		c.mu.Unlock()
		return nil, ErrTooManyStreams
	}
	id := protocol.NewID()
	s := &Stream{ID: id, C: make(chan LogEvent, 64), c: c}
	c.streams[id] = s
	c.mu.Unlock()
	if o.Tail > protocol.MaxTail {
		o.Tail = protocol.MaxTail
	}
	if err := c.send(protocol.TypeLogsOpen, protocol.LogsOpen{StreamID: id, Selector: t.Selector, Tail: o.Tail, Since: o.Since, Follow: o.Follow}); err != nil {
		c.removeStream(id)
		return nil, ErrOffline
	}
	return s, nil
}

// Range implements LogSource by opening a non-following stream and collecting it.
func (h *Hub) Range(ctx context.Context, t Target, since time.Time, limit int) ([]protocol.LogLine, error) {
	if limit <= 0 || limit > protocol.MaxTail {
		limit = protocol.MaxTail
	}
	s, err := h.Tail(ctx, t, LogOptions{Tail: limit, Since: &since})
	if err != nil {
		return nil, err
	}
	defer s.Close()
	var out []protocol.LogLine
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev := <-s.C:
			out = append(out, ev.Lines...)
			if ev.Closed {
				if ev.Reason != protocol.CloseEnded && ev.Reason != protocol.CloseRequested {
					return out, fmt.Errorf("logs closed: %s", ev.Reason)
				}
				return out, nil
			}
		case <-deadline:
			return out, errors.New("timed out reading logs")
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
}

// Restart asks the agent to restart a service and waits for the result.
func (h *Hub) Restart(ctx context.Context, t Target) (protocol.ActionResult, error) {
	c, err := h.conn(t.AgentID)
	if err != nil {
		return protocol.ActionResult{}, err
	}
	id := protocol.NewID()
	ch := make(chan protocol.ActionResult, 1)
	c.mu.Lock()
	c.actions[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.actions, id); c.mu.Unlock() }()
	if err := c.send(protocol.TypeActionRequest, protocol.ActionRequest{RequestID: id, Action: protocol.ActionRestart, Selector: t.Selector}); err != nil {
		return protocol.ActionResult{}, ErrOffline
	}
	select {
	case r := <-ch:
		return r, nil
	case <-time.After(75 * time.Second):
		return protocol.ActionResult{}, errors.New("the agent did not answer in time")
	case <-ctx.Done():
		return protocol.ActionResult{}, ctx.Err()
	case <-c.ctx.Done():
		return protocol.ActionResult{}, ErrOffline
	}
}

// Probe asks the agent to check a URL only it can reach.
func (h *Hub) Probe(ctx context.Context, agentID string, chk protocol.HealthCheck) (protocol.HealthResult, error) {
	c, err := h.conn(agentID)
	if err != nil {
		return protocol.HealthResult{}, err
	}
	ch := make(chan protocol.HealthResult, 1)
	c.mu.Lock()
	c.health[chk.AppEnvID] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.health, chk.AppEnvID); c.mu.Unlock() }()
	if err := c.send(protocol.TypeHealthCheck, chk); err != nil {
		return protocol.HealthResult{}, ErrOffline
	}
	wait := time.Duration(chk.TimeoutMS)*time.Millisecond + 3*time.Second
	select {
	case r := <-ch:
		return r, nil
	case <-time.After(wait):
		return protocol.HealthResult{}, errors.New("no health result from the agent")
	case <-ctx.Done():
		return protocol.HealthResult{}, ctx.Err()
	}
}
