package agent

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/poppyszn/helmsman/pkg/protocol"
)

const (
	labelProject  = "com.docker.compose.project"
	labelService  = "com.docker.compose.service"
	labelRevision = "org.opencontainers.image.revision"
)

// ErrDockerDown means the socket is missing or the daemon does not answer.
var ErrDockerDown = errors.New("docker unavailable")

// Docker is a deliberately tiny client. It exposes only the calls the agent needs:
// list, inspect, logs, restart and events. There is no exec, create or generic passthrough.
type Docker struct {
	hc *http.Client
}

// NewDocker connects to a unix socket.
func NewDocker(socket string) *Docker {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		MaxIdleConns: 2, IdleConnTimeout: 30 * time.Second,
	}
	return &Docker{hc: &http.Client{Transport: tr}}
}

func (d *Docker) do(ctx context.Context, method, path string, q url.Values) (*http.Response, error) {
	u := "http://docker" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", ErrDockerDown, err)
	}
	return resp, nil
}

func (d *Docker) getJSON(ctx context.Context, path string, q url.Values, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := d.do(ctx, http.MethodGet, path, q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker %s: %d %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Version returns the daemon version, which doubles as a liveness probe.
func (d *Docker) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"Version"`
	}
	if err := d.getJSON(ctx, "/version", nil, &v); err != nil {
		return "", err
	}
	return v.Version, nil
}

type inspect struct {
	ID           string `json:"Id"`
	RestartCount int
	State        struct {
		Status    string
		StartedAt string
		Health    *struct{ Status string }
	}
	Config struct {
		Image  string
		Labels map[string]string
		Tty    bool
	}
	HostConfig struct {
		LogConfig struct{ Type string }
	}
}

// Containers returns inspected containers that carry compose labels and are allowed by cfg.
// When several containers share a service (an old one exited, a new one runs), the running
// one wins, else the most recently started.
func (d *Docker) Containers(ctx context.Context, cfg Config) ([]protocol.Container, map[string]inspect, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {labelProject}})
	var list []struct {
		ID      string `json:"Id"`
		State   string
		Labels  map[string]string
		Created int64
	}
	if err := d.getJSON(ctx, "/containers/json", url.Values{"all": {"1"}, "filters": {string(filters)}}, &list); err != nil {
		return nil, nil, err
	}
	best := map[string]protocol.Container{}
	raw := map[string]inspect{}
	for _, c := range list {
		p, s := c.Labels[labelProject], c.Labels[labelService]
		if !cfg.Visible(p, s) {
			continue
		}
		var in inspect
		if err := d.getJSON(ctx, "/containers/"+url.PathEscape(c.ID)+"/json", nil, &in); err != nil {
			continue // removed between list and inspect
		}
		pc := toContainer(p, s, in)
		key := p + "/" + s
		if cur, ok := best[key]; ok && !better(pc, cur) {
			continue
		}
		best[key], raw[key] = pc, in
	}
	out := make([]protocol.Container, 0, len(best))
	for _, c := range best {
		out = append(out, c)
	}
	return out, raw, nil
}

// Discovered lists the compose services on this host that cfg does not expose: names, image and state
// only. Used when discovery is on, so an admin can see what to add to the allowlist.
func (d *Docker) Discovered(ctx context.Context, cfg Config) ([]protocol.Discovered, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {labelProject}})
	var list []struct {
		State  string
		Image  string
		Labels map[string]string
	}
	if err := d.getJSON(ctx, "/containers/json", url.Values{"all": {"1"}, "filters": {string(filters)}}, &list); err != nil {
		return nil, err
	}
	best := map[string]protocol.Discovered{}
	for _, c := range list {
		p, s := c.Labels[labelProject], c.Labels[labelService]
		if p == "" || s == "" || cfg.Visible(p, s) {
			continue
		}
		key := p + "/" + s
		if cur, ok := best[key]; ok && (cur.State == "running" || c.State != "running") {
			continue
		}
		best[key] = protocol.Discovered{ComposeProject: p, Service: s, Image: c.Image, State: c.State}
	}
	out := make([]protocol.Discovered, 0, len(best))
	for _, v := range best {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ComposeProject != out[j].ComposeProject {
			return out[i].ComposeProject < out[j].ComposeProject
		}
		return out[i].Service < out[j].Service
	})
	if len(out) > protocol.MaxDiscovered {
		out = out[:protocol.MaxDiscovered]
	}
	return out, nil
}

func better(a, b protocol.Container) bool {
	if (a.State == "running") != (b.State == "running") {
		return a.State == "running"
	}
	if a.StartedAt != nil && b.StartedAt != nil {
		return a.StartedAt.After(*b.StartedAt)
	}
	return a.StartedAt != nil
}

func toContainer(project, service string, in inspect) protocol.Container {
	c := protocol.Container{
		ComposeProject: project, Service: service, ID: in.ID, Image: in.Config.Image,
		Revision: in.Config.Labels[labelRevision], State: in.State.Status,
		RestartCount: in.RestartCount, LogDriver: in.HostConfig.LogConfig.Type,
	}
	if in.State.Health != nil {
		c.Health = in.State.Health.Status
	}
	if t, err := time.Parse(time.RFC3339Nano, in.State.StartedAt); err == nil && !t.IsZero() && t.Year() > 1 {
		c.StartedAt = &t
	}
	return c
}

// Find resolves a selector to the current container.
func (d *Docker) Find(ctx context.Context, cfg Config, project, service string) (protocol.Container, inspect, error) {
	cs, raw, err := d.Containers(ctx, cfg)
	if err != nil {
		return protocol.Container{}, inspect{}, err
	}
	for _, c := range cs {
		if c.ComposeProject == project && c.Service == service {
			return c, raw[project+"/"+service], nil
		}
	}
	return protocol.Container{}, inspect{}, errNotFound
}

var errNotFound = errors.New("container not found")

// Restart restarts a container by ID (resolved from labels by the caller).
func (d *Docker) Restart(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := d.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/restart", url.Values{"t": {"10"}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("restart: %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// DockerEvent is a raw container event.
type DockerEvent struct {
	Action string
	ID     string
	Attrs  map[string]string
	Time   time.Time
}

// Events streams container events until ctx ends or the stream breaks.
func (d *Docker) Events(ctx context.Context, fn func(DockerEvent)) error {
	filters, _ := json.Marshal(map[string][]string{
		"type": {"container"}, "label": {labelProject},
		"event": {"start", "die", "restart", "health_status"},
	})
	resp, err := d.do(ctx, http.MethodGet, "/events", url.Values{"filters": {string(filters)}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("events: %d", resp.StatusCode)
	}
	dec := json.NewDecoder(resp.Body)
	for {
		var m struct {
			Action string
			Actor  struct {
				ID         string
				Attributes map[string]string
			}
			TimeNano int64 `json:"timeNano"`
			Time     int64 `json:"time"`
		}
		if err := dec.Decode(&m); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%w: events: %v", ErrDockerDown, err)
		}
		t := time.Unix(m.Time, 0)
		if m.TimeNano != 0 {
			t = time.Unix(0, m.TimeNano)
		}
		fn(DockerEvent{Action: m.Action, ID: m.Actor.ID, Attrs: m.Actor.Attributes, Time: t.UTC()})
	}
}

// LogReader opens a log stream for a container and yields complete lines.
type LogOptions struct {
	Tail   int
	Since  time.Time
	Follow bool
}

// RawLine is one decoded log line before rate limiting.
type RawLine struct {
	TS     time.Time
	Stream string
	Text   string
	Trunc  bool
}

// Logs streams lines to fn until the stream ends or ctx is done.
func (d *Docker) Logs(ctx context.Context, id string, tty bool, o LogOptions, maxLine int, fn func(RawLine)) error {
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}}
	if o.Follow {
		q.Set("follow", "1")
	}
	if o.Tail > 0 || o.Since.IsZero() {
		q.Set("tail", fmt.Sprint(o.Tail))
	}
	if !o.Since.IsZero() {
		q.Set("since", fmt.Sprintf("%d.%09d", o.Since.Unix(), o.Since.Nanosecond()))
	}
	resp, err := d.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/logs", q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return errNotFound
	case http.StatusNotImplemented:
		// Docker answers 501 when the logging driver cannot be read back (awslogs, none, ...).
		return errLogsUnavailable
	default:
		// Anything else (409 while a container is being replaced, 5xx) is transient.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker logs: %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return demux(resp.Body, tty, maxLine, fn)
}

var errLogsUnavailable = errors.New("logs unavailable")

// lineSplitter assembles frames of one stream into lines, truncating over-long lines.
type lineSplitter struct {
	stream   string
	max      int
	buf      []byte
	skipping bool
	fn       func(RawLine)
}

func (s *lineSplitter) write(p []byte) {
	for len(p) > 0 {
		i := indexByte(p, '\n')
		if i < 0 {
			s.add(p)
			return
		}
		s.add(p[:i])
		if s.skipping {
			s.skipping = false
			s.buf = s.buf[:0]
		} else {
			s.flush(false)
		}
		p = p[i+1:]
	}
}

func (s *lineSplitter) add(p []byte) {
	if s.skipping {
		return
	}
	// Leave room for the timestamp prefix (about 36 bytes).
	if len(s.buf)+len(p) > s.max+64 {
		room := s.max + 64 - len(s.buf)
		if room > 0 {
			s.buf = append(s.buf, p[:room]...)
		}
		s.flush(true)
		s.skipping = true
		return
	}
	s.buf = append(s.buf, p...)
}

func (s *lineSplitter) flush(trunc bool) {
	line := strings.TrimSuffix(string(s.buf), "\r")
	s.buf = s.buf[:0]
	ts, text := splitTimestamp(line)
	if len(text) > s.max {
		text, trunc = cutUTF8(text, s.max), true
	}
	s.fn(RawLine{TS: ts, Stream: s.stream, Text: text, Trunc: trunc})
}

func indexByte(p []byte, c byte) int {
	for i, b := range p {
		if b == c {
			return i
		}
	}
	return -1
}

func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

func splitTimestamp(line string) (time.Time, string) {
	if i := strings.IndexByte(line, ' '); i > 0 {
		if t, err := time.Parse(time.RFC3339Nano, line[:i]); err == nil {
			return t.UTC(), line[i+1:]
		}
	}
	return time.Now().UTC(), line
}

// demux reads Docker's multiplexed stream (8 byte header: type, 3 zero bytes, big endian size).
// TTY containers send a raw stream with no framing.
func demux(r io.Reader, tty bool, maxLine int, fn func(RawLine)) error {
	out := &lineSplitter{stream: "stdout", max: maxLine, fn: fn}
	errs := &lineSplitter{stream: "stderr", max: maxLine, fn: fn}
	br := bufio.NewReaderSize(r, 32*1024)
	if tty {
		buf := make([]byte, 32*1024)
		for {
			n, err := br.Read(buf)
			out.write(buf[:n])
			if err != nil {
				return eofOK(err)
			}
		}
	}
	var hdr [8]byte
	buf := make([]byte, 32*1024)
	for {
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return eofOK(err)
		}
		size := int(binary.BigEndian.Uint32(hdr[4:]))
		dst := out
		if hdr[0] == 2 {
			dst = errs
		}
		for size > 0 {
			n := size
			if n > len(buf) {
				n = len(buf)
			}
			if _, err := io.ReadFull(br, buf[:n]); err != nil {
				return eofOK(err)
			}
			dst.write(buf[:n])
			size -= n
		}
	}
}

func eofOK(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	return err
}
