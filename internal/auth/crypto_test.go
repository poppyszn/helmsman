package auth

import (
	"bytes"
	"testing"
)

func TestEncryptRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	sealed, err := Encrypt(key, []byte("gho_secret"))
	if err != nil || bytes.Contains(sealed, []byte("gho_secret")) {
		t.Fatalf("seal failed: %v", err)
	}
	pt, err := Decrypt(key, sealed)
	if err != nil || string(pt) != "gho_secret" {
		t.Fatalf("round trip: %v %q", err, pt)
	}
	if _, err := Decrypt(bytes.Repeat([]byte{8}, 32), sealed); err == nil {
		t.Fatal("wrong key must fail")
	}
}

func TestSignVerify(t *testing.T) {
	k := []byte("k")
	s := Sign(k, "abc")
	if v, ok := Verify(k, s); !ok || v != "abc" {
		t.Fatal("verify failed")
	}
	if _, ok := Verify(k, s+"x"); ok {
		t.Fatal("tampered cookie accepted")
	}
	if _, ok := Verify([]byte("other"), s); ok {
		t.Fatal("wrong key accepted")
	}
}
