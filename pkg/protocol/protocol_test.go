package protocol

import "testing"

func TestDecode(t *testing.T) {
	e, err := Decode([]byte(`{"v":1,"type":"hello","id":"1","ts":"2026-01-01T00:00:00Z","payload":{}}`))
	if err != nil || e.Type != "hello" {
		t.Fatalf("%v %+v", err, e)
	}
	if _, err := Decode([]byte(`{"v":9,"type":"x"}`)); err == nil {
		t.Fatal("expected version error")
	}
	if _, err := Decode([]byte(`nope`)); err == nil {
		t.Fatal("expected parse error")
	}
}
