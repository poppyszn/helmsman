package auth

import (
	"encoding/base32"
	"testing"
	"time"
)

func TestTOTPMatchesRFC6238Vector(t *testing.T) {
	// RFC 6238 appendix B, SHA-1, secret "12345678901234567890": T=59 gives 94287082 (8 digits),
	// so the 6 digit code is 287082.
	secret := []byte("12345678901234567890")
	if got := totpCode(secret, 59/30); got != "287082" {
		t.Fatalf("got %s", got)
	}
	if _, ok := VerifyTOTP(secret, "287082", time.Unix(59, 0)); !ok {
		t.Fatal("current step must verify")
	}
	if _, ok := VerifyTOTP(secret, "287082", time.Unix(59+30, 0)); !ok {
		t.Fatal("one step of drift is allowed")
	}
	if _, ok := VerifyTOTP(secret, "287082", time.Unix(59+120, 0)); ok {
		t.Fatal("old codes must fail")
	}
	if _, ok := VerifyTOTP(secret, "000000", time.Unix(59, 0)); ok {
		t.Fatal("wrong code")
	}
}

func TestDecodeSecretAcceptsSpacesAndLowercase(t *testing.T) {
	raw := []byte("12345678901234567890")
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	got, err := DecodeTOTPSecret(enc[:8] + " " + enc[8:])
	if err != nil || string(got) != string(raw) {
		t.Fatalf("%v %q", err, got)
	}
}
