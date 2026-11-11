package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/poppyszn/helmsman/pkg/protocol"
)

// logDriverOK lists drivers whose logs the Docker API can read back.
func logDriverOK(d string) bool {
	switch d {
	case "", "json-file", "local", "journald":
		return true
	}
	return false
}

func (s *session) openLogs(m protocol.LogsOpen) {
	closeWith := func(reason, msg string) {
		_ = s.send(protocol.TypeLogsClose, protocol.LogsClose{StreamID: m.StreamID, Reason: reason, Message: msg})
	}
	sel := m.Selector
	if !s.a.cfg.Allowed(ActLogs, sel.ComposeProject, sel.Service) {
		s.a.log.Warn("logs refused by allowlist", "project", sel.ComposeProject, "service", sel.Service)
		closeWith(protocol.CloseNotAllowed, "not allowed by the agent allowlist")
		return
	}
	s.mu.Lock()
	if len(s.streams) >= s.a.cfg.Limits.MaxStreams {
		s.mu.Unlock()
		closeWith(protocol.CloseTooManyStreams, "too many open log streams on this agent")
		return
	}
	if _, dup := s.streams[m.StreamID]; dup {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.streams[m.StreamID] = cancel
	s.mu.Unlock()

	go func() {
		reason, msg := s.streamLogs(ctx, m)
		cancel()
		s.mu.Lock()
		delete(s.streams, m.StreamID)
		s.mu.Unlock()
		if s.ctx.Err() == nil {
			closeWith(reason, msg)
		}
	}()
}

// streamLogs follows a service by compose labels. When the container is replaced (a
// deploy) it reattaches to the new one and inserts a marker line.
func (s *session) streamLogs(ctx context.Context, m protocol.LogsOpen) (reason, msg string) {
	cfg := s.a.cfg
	sink := newSink(ctx, s, m.StreamID, cfg.Limits)
	defer sink.stop()

	tail := m.Tail
	if tail < 0 {
		tail = 0
	}
	if tail > protocol.MaxTail {
		tail = protocol.MaxTail
	}
	var since time.Time
	if m.Since != nil {
		since = *m.Since
		tail = 0
	}
	var lastID string
	var lastTS time.Time
	attached := false
	for ctx.Err() == nil {
		c, in, err := s.a.docker.Find(ctx, cfg, m.Selector.ComposeProject, m.Selector.Service)
		switch {
		case errors.Is(err, ErrDockerDown):
			if !attached {
				return protocol.CloseDockerDown, "the Docker daemon is not reachable on this host"
			}
		case errors.Is(err, errNotFound):
			if !attached {
				return protocol.CloseContainerMissing, "no container found for this service"
			}
		case err != nil:
			return protocol.CloseError, "could not read the container"
		default:
			if !logDriverOK(c.LogDriver) {
				return protocol.CloseLogsUnavailable, fmt.Sprintf("logs are not available through Docker (log driver %q)", c.LogDriver)
			}
			if lastID != "" && lastID != c.ID {
				sink.marker("-- container replaced, log stream reattached --")
				// A new container: show its history from the start (bounded), not from lastTS.
				tail, since = 200, time.Time{}
			} else if !lastTS.IsZero() {
				tail, since = 0, lastTS.Add(time.Nanosecond)
			}
			lastID, attached = c.ID, true
			err := s.a.docker.Logs(ctx, c.ID, in.Config.Tty, LogOptions{Tail: tail, Since: since, Follow: m.Follow},
				cfg.Limits.MaxLineBytes, func(l RawLine) {
					if l.TS.After(lastTS) {
						lastTS = l.TS
					}
					sink.add(l)
				})
			sink.flush()
			if ctx.Err() != nil {
				return protocol.CloseRequested, ""
			}
			if errors.Is(err, errLogsUnavailable) {
				return protocol.CloseLogsUnavailable, "logs are not available through Docker for this container"
			}
			if !m.Follow {
				if err != nil {
					return protocol.CloseError, "could not read the container logs"
				}
				return protocol.CloseEnded, ""
			}
			if err != nil && !errors.Is(err, errNotFound) {
				s.a.log.Warn("log read failed, retrying", "service", m.Selector.Service, "err", err)
				sink.marker("-- reading logs failed, retrying --")
			} else {
				sink.marker("-- container stopped, waiting for it to start again --")
			}
		}
		// Wait for a start event, or poll slowly. Never a tight loop.
		select {
		case <-ctx.Done():
		case <-s.a.events.wait():
		case <-time.After(5 * time.Second):
		}
	}
	return protocol.CloseRequested, ""
}

// sink batches lines, applies the per stream rate cap and sends chunks.
type sink struct {
	ctx      context.Context
	s        *session
	id       string
	maxRate  int
	mu       sync.Mutex
	lines    []protocol.LogLine
	bytes    int
	winStart time.Time
	winCount int
	dropped  int
	done     chan struct{}
}

func newSink(ctx context.Context, s *session, id string, l Limits) *sink {
	k := &sink{ctx: ctx, s: s, id: id, maxRate: l.MaxLinesPerSec, done: make(chan struct{})}
	go func() {
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-k.done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				k.flush()
			}
		}
	}()
	return k
}

func (k *sink) stop() { close(k.done); k.flush() }

func (k *sink) add(l RawLine) {
	k.mu.Lock()
	now := time.Now()
	if now.Sub(k.winStart) >= time.Second {
		if k.dropped > 0 {
			k.appendLocked(protocol.LogLine{TS: now.UTC(), Stream: "stderr", Marker: true,
				Text: fmt.Sprintf("-- %d lines dropped (rate limit of %d lines/s) --", k.dropped, k.maxRate)})
			k.dropped = 0
		}
		k.winStart, k.winCount = now, 0
	}
	if k.winCount >= k.maxRate {
		k.dropped++
		k.mu.Unlock()
		return
	}
	k.winCount++
	text := l.Text
	if l.Trunc {
		text += " …[line truncated]"
	}
	k.appendLocked(protocol.LogLine{TS: l.TS, Stream: l.Stream, Text: text})
	big := k.bytes > 128<<10 || len(k.lines) >= 200
	k.mu.Unlock()
	if big {
		k.flush()
	}
}

func (k *sink) marker(text string) {
	k.mu.Lock()
	k.appendLocked(protocol.LogLine{TS: time.Now().UTC(), Stream: "stdout", Marker: true, Text: text})
	k.mu.Unlock()
	k.flush()
}

func (k *sink) appendLocked(l protocol.LogLine) {
	k.lines = append(k.lines, l)
	k.bytes += len(l.Text) + 48
}

func (k *sink) flush() {
	k.mu.Lock()
	if len(k.lines) == 0 {
		k.mu.Unlock()
		return
	}
	lines := k.lines
	k.lines, k.bytes = nil, 0
	k.mu.Unlock()
	_ = k.s.send(protocol.TypeLogsChunk, protocol.LogsChunk{StreamID: k.id, Lines: lines})
}
