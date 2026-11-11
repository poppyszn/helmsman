// Package protocol defines the agent <-> server message envelope.
// Message payloads arrive in M2.
package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// Version is the current protocol version. The server supports Version and Version-1.
const Version = 1

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
