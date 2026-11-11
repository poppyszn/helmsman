package agent

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

func cfgFrom(t *testing.T, yaml string, env map[string]string) (Config, error) {
	t.Helper()
	p := t.TempDir() + "/agent.yaml"
	if err := writeFile(p, yaml); err != nil {
		t.Fatal(err)
	}
	return LoadConfig(p, func(k string) string { return env[k] })
}

func TestConfigRefusesPlainWebSocketOutsideDev(t *testing.T) {
	y := "idp_url: ws://idp.example.com/agent/ws\nallow: []\n"
	if _, err := cfgFrom(t, y, nil); err == nil || !strings.Contains(err.Error(), "ws://") {
		t.Fatalf("want ws:// refusal, got %v", err)
	}
	if _, err := cfgFrom(t, y, map[string]string{"IDP_ALLOW_INSECURE": "true"}); err != nil {
		t.Fatalf("dev override: %v", err)
	}
	if _, err := cfgFrom(t, "idp_url: wss://idp.example.com/agent/ws\n", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cfgFrom(t, "idp_url: https://idp.example.com\n", nil); err == nil {
		t.Fatal("https scheme must be rejected for the socket url")
	}
	if _, err := cfgFrom(t, "idp_url: wss://x/agent/ws\nallow:\n  - compose_project: a\n    services: [b]\n    actions: [exec]\n", nil); err == nil {
		t.Fatal("unknown action must be rejected")
	}
}

func TestConfigDefaultsAndJoinURL(t *testing.T) {
	c, err := cfgFrom(t, "idp_url: wss://idp.example.com/agent/ws\nsnapshot_interval: 10m\nlimits: {max_line_bytes: 999999}\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.SnapshotInterval != 30*time.Second {
		t.Fatalf("snapshot interval is capped at 30s (it is the heartbeat), got %v", c.SnapshotInterval)
	}
	if c.Limits.MaxLineBytes != 16384 || c.Limits.MaxStreams != 5 || c.Limits.MaxLinesPerSec != 500 {
		t.Fatalf("limits %+v", c.Limits)
	}
	if c.JoinURL() != "https://idp.example.com/agent/join" {
		t.Fatal(c.JoinURL())
	}
}

func TestAllowlistIsDenyByDefault(t *testing.T) {
	c := Config{Allow: []Allow{
		{ComposeProject: "billing", Services: []string{"api", "worker"}, Actions: []string{"logs"}},
		{ComposeProject: "ledger", Services: []string{"api"}, Actions: []string{"logs", "restart"}, HealthURLs: []string{"http://api:8080/health"}},
	}}
	cases := []struct {
		action, p, s string
		want         bool
	}{
		{"logs", "billing", "api", true},
		{"restart", "billing", "api", false}, // logs only
		{"logs", "billing", "db", false},     // service not listed
		{"restart", "ledger", "api", true},
		{"logs", "other", "api", false},
		{"exec", "ledger", "api", false}, // no such action ever
		{"", "ledger", "api", false},
	}
	for _, tc := range cases {
		if got := c.Allowed(tc.action, tc.p, tc.s); got != tc.want {
			t.Errorf("%s %s/%s: got %v", tc.action, tc.p, tc.s, got)
		}
	}
	if (Config{}).Allowed("logs", "billing", "api") {
		t.Fatal("empty allowlist must allow nothing")
	}
	if !c.HealthAllowed("http://api:8080/health") || c.HealthAllowed("http://169.254.169.254/latest") {
		t.Fatal("health urls must match exactly")
	}
	if !c.Visible("billing", "worker") || c.Visible("billing", "db") {
		t.Fatal("visibility")
	}
}

func TestDigestIgnoresOrder(t *testing.T) {
	a := Config{Allow: []Allow{{ComposeProject: "a", Services: []string{"x", "y"}, Actions: []string{"logs", "restart"}}, {ComposeProject: "b", Services: []string{"z"}}}}
	b := Config{Allow: []Allow{{ComposeProject: "b", Services: []string{"z"}}, {ComposeProject: "a", Services: []string{"y", "x"}, Actions: []string{"restart", "logs"}}}}
	if a.Digest() != b.Digest() {
		t.Fatal("digest must be order independent")
	}
	b.Allow[0].Services = []string{"z", "extra"}
	if a.Digest() == b.Digest() {
		t.Fatal("digest must change with the allowlist")
	}
}

func mux(stream byte, s string) []byte {
	h := make([]byte, 8)
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(s)))
	return append(h, s...)
}

func collect(t *testing.T, raw []byte, tty bool, max int) []RawLine {
	t.Helper()
	var out []RawLine
	if err := demux(bytes.NewReader(raw), tty, max, func(l RawLine) { out = append(out, l) }); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDemuxStreamsTimestampsAndSplitFrames(t *testing.T) {
	var raw []byte
	raw = append(raw, mux(1, "2026-01-02T03:04:05.123456789Z hello\n2026-01-02T03:04:06Z wor")...)
	raw = append(raw, mux(2, "2026-01-02T03:04:07Z oops\n")...)
	raw = append(raw, mux(1, "ld\n")...) // a line split across two frames of the same stream
	got := collect(t, raw, false, 16384)
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if got[0].Text != "hello" || got[0].Stream != "stdout" || got[0].TS.Nanosecond() != 123456789 {
		t.Fatalf("%+v", got[0])
	}
	if got[1].Text != "oops" || got[1].Stream != "stderr" {
		t.Fatalf("%+v", got[1])
	}
	if got[2].Text != "world" {
		t.Fatalf("split line: %+v", got[2])
	}
}

func TestDemuxTruncatesHugeLinesAndKeepsGoing(t *testing.T) {
	huge := strings.Repeat("A", 100000)
	raw := mux(1, "2026-01-02T03:04:05Z "+huge+"\n2026-01-02T03:04:06Z after\n")
	got := collect(t, raw, false, 16384)
	if len(got) != 2 {
		t.Fatalf("want the truncated line and the next one, got %d", len(got))
	}
	if !got[0].Trunc || len(got[0].Text) > 16384 {
		t.Fatalf("not truncated: trunc=%v len=%d", got[0].Trunc, len(got[0].Text))
	}
	if got[1].Text != "after" || got[1].Trunc {
		t.Fatalf("%+v", got[1])
	}
}

func TestDemuxBinaryAndInvalidUTF8DoNotCrash(t *testing.T) {
	bin := "2026-01-02T03:04:05Z \x00\x01\xff\xfe binary \x1b[31mred\x1b[0m\n"
	got := collect(t, mux(1, bin), false, 16384)
	if len(got) != 1 || !strings.Contains(got[0].Text, "binary") {
		t.Fatalf("%+v", got)
	}
}

func TestDemuxTTYHasNoFraming(t *testing.T) {
	got := collect(t, []byte("2026-01-02T03:04:05Z tty line\n"), true, 16384)
	if len(got) != 1 || got[0].Text != "tty line" {
		t.Fatalf("%+v", got)
	}
}

func TestCutUTF8NeverSplitsARune(t *testing.T) {
	s := strings.Repeat("é", 10) // 2 bytes each
	for n := 1; n < len(s); n++ {
		c := cutUTF8(s, n)
		if !strings.HasPrefix(s, c) || len(c) > n {
			t.Fatalf("cut %d gave %q", n, c)
		}
		for _, r := range c {
			if r == '�' {
				t.Fatalf("split rune at %d", n)
			}
		}
	}
}

func writeFile(p, s string) error { return osWriteFile(p, []byte(s)) }
