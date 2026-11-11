// Package protocol defines the agent <-> server message envelope and payloads.
//
// Compatibility: the server supports Version and Version-1. After M2, changes must be
// additive (new optional fields, new message types). Never rename or remove fields.
package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Version is the current protocol version. The server supports Version and Version-1.
const Version = 1

// Message types.
const (
	TypeHello         = "hello"          // agent -> server
	TypeSnapshot      = "snapshot"       // agent -> server
	TypeEvent         = "event"          // agent -> server
	TypeLogsOpen      = "logs.open"      // server -> agent
	TypeLogsChunk     = "logs.chunk"     // agent -> server
	TypeLogsClose     = "logs.close"     // either
	TypeHealthCheck   = "health.check"   // server -> agent
	TypeHealthResult  = "health.result"  // agent -> server
	TypeActionRequest = "action.request" // server -> agent
	TypeActionResult  = "action.result"  // agent -> server
	TypeError         = "error"          // either
)

// Actions an agent can perform. There is deliberately no free-form command.
const ActionRestart = "restart"

// Limits shared by both sides.
const (
	MaxTail      = 2000
	MaxLineBytes = 16384
)

// Envelope is the JSON wrapper for every message.
type Envelope struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	TS      time.Time       `json:"ts"`
	Payload json.RawMessage `json:"payload"`
}

// Decode parses an envelope and rejects unsupported versions.
func Decode(b []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		return e, fmt.Errorf("decode envelope: %w", err)
	}
	if e.V != Version && e.V != Version-1 {
		return e, fmt.Errorf("unsupported protocol version %d", e.V)
	}
	return e, nil
}

// NewID returns a random message id.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Encode builds and marshals an envelope.
func Encode(typ string, payload any) ([]byte, error) {
	return EncodeWithID(NewID(), typ, payload)
}

// EncodeWithID is Encode with a caller chosen id.
func EncodeWithID(id, typ string, payload any) ([]byte, error) {
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{V: Version, Type: typ, ID: id, TS: time.Now().UTC(), Payload: p})
}

// Unmarshal decodes an envelope payload.
func Unmarshal(e Envelope, v any) error {
	if len(e.Payload) == 0 {
		return fmt.Errorf("%s: empty payload", e.Type)
	}
	return json.Unmarshal(e.Payload, v)
}

// Hello is the first message an agent sends.
type Hello struct {
	AgentVersion    string `json:"agent_version"`
	ProtocolVersion int    `json:"protocol_version"`
	Hostname        string `json:"hostname"`
	DockerVersion   string `json:"docker_version"`
	AllowlistDigest string `json:"allowlist_digest"`
	// InstanceID is random per agent process. A second live connection with another
	// instance id means the credential is shared.
	InstanceID string `json:"instance_id"`
}

// Container is one compose service container as the agent sees it.
type Container struct {
	ComposeProject string     `json:"compose_project"`
	Service        string     `json:"service"`
	ID             string     `json:"id"`
	Image          string     `json:"image"`
	Revision       string     `json:"revision"` // org.opencontainers.image.revision label
	State          string     `json:"state"`    // running, exited, restarting, ...
	Health         string     `json:"health"`   // healthy, unhealthy, starting, or empty
	StartedAt      *time.Time `json:"started_at,omitempty"`
	RestartCount   int        `json:"restart_count"`
	LogDriver      string     `json:"log_driver,omitempty"`
}

// Snapshot is the full list of allowed containers.
type Snapshot struct {
	DockerOK    bool        `json:"docker_ok"`
	DockerError string      `json:"docker_error,omitempty"`
	Containers  []Container `json:"containers"`
}

// Event kinds.
const (
	EventStart   = "start"
	EventDie     = "die"
	EventRestart = "restart"
	EventHealth  = "health" // Detail holds the new health status
)

// Event is a Docker event for an allowed container.
type Event struct {
	Kind           string    `json:"kind"`
	Detail         string    `json:"detail,omitempty"`
	ComposeProject string    `json:"compose_project"`
	Service        string    `json:"service"`
	ContainerID    string    `json:"container_id"`
	ExitCode       *int      `json:"exit_code,omitempty"`
	At             time.Time `json:"at"` // as reported by the agent clock
}

// Selector picks containers by compose labels, never by ID.
type Selector struct {
	ComposeProject string `json:"compose_project"`
	Service        string `json:"service"`
}

// LogsOpen asks the agent to stream logs.
type LogsOpen struct {
	StreamID string     `json:"stream_id"`
	Selector Selector   `json:"selector"`
	Tail     int        `json:"tail"`
	Since    *time.Time `json:"since,omitempty"`
	Follow   bool       `json:"follow"`
}

// LogLine is one log line. Marker lines are generated by the agent (dropped lines,
// truncation, container replaced) and are not application output.
type LogLine struct {
	TS     time.Time `json:"ts"`
	Stream string    `json:"stream"` // stdout or stderr
	Text   string    `json:"text"`
	Marker bool      `json:"marker,omitempty"`
}

// LogsChunk carries log lines.
type LogsChunk struct {
	StreamID string    `json:"stream_id"`
	Lines    []LogLine `json:"lines"`
}

// Log close reasons.
const (
	CloseEnded            = "ended"
	CloseRequested        = "requested"
	CloseNotAllowed       = "not_allowed"
	CloseTooManyStreams   = "too_many_streams"
	CloseDockerDown       = "docker_unavailable"
	CloseLogsUnavailable  = "logs_unavailable" // non-file logging driver
	CloseContainerMissing = "container_not_found"
	CloseError            = "error"
)

// LogsClose ends a stream, from either side.
type LogsClose struct {
	StreamID string `json:"stream_id"`
	Reason   string `json:"reason"`
	Message  string `json:"message,omitempty"`
}

// HealthCheck asks the agent to probe a URL that only the app server can reach.
type HealthCheck struct {
	AppEnvID      int64  `json:"app_env_id"`
	URL           string `json:"url"`
	TimeoutMS     int    `json:"timeout_ms"`
	TLSSkipVerify bool   `json:"tls_skip_verify,omitempty"`
}

// HealthResult is the probe outcome.
type HealthResult struct {
	AppEnvID   int64  `json:"app_env_id"`
	StatusCode int    `json:"status_code"`
	LatencyMS  int    `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
}

// ActionRequest asks the agent to perform one fixed action.
type ActionRequest struct {
	RequestID string   `json:"request_id"`
	Action    string   `json:"action"` // restart only
	Selector  Selector `json:"selector"`
}

// Action result statuses.
const (
	ResultOK     = "ok"
	ResultFailed = "failed"
	ResultDenied = "denied"
)

// ActionResult reports the outcome.
type ActionResult struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

// Error codes.
const (
	ErrCodeBadMessage  = "bad_message"
	ErrCodeUnsupported = "unsupported"
	ErrCodeDuplicate   = "duplicate_connection"
	ErrCodeInternal    = "internal"
)

// ErrorMsg reports a protocol level problem.
type ErrorMsg struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RelatedID string `json:"related_id,omitempty"`
}
