package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The rate cap drops lines past the limit and says how many were dropped.
func TestSinkRateCapEmitsDroppedMarker(t *testing.T) {
	k := &sink{ctx: context.Background(), maxRate: 5, done: make(chan struct{})}
	for i := 0; i < 20; i++ {
		k.add(RawLine{TS: time.Now(), Stream: "stdout", Text: "line"})
	}
	if len(k.lines) != 5 || k.dropped != 15 {
		t.Fatalf("kept %d dropped %d", len(k.lines), k.dropped)
	}
	// A new second begins: the marker is queued before the next line.
	k.winStart = time.Now().Add(-2 * time.Second)
	k.add(RawLine{TS: time.Now(), Stream: "stdout", Text: "next"})
	var marker string
	for _, l := range k.lines {
		if l.Marker {
			marker = l.Text
		}
	}
	if !strings.Contains(marker, "15 lines dropped") {
		t.Fatalf("marker %q", marker)
	}
}
