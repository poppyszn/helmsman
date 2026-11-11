package auth

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP is defined over HMAC-SHA1 and authenticator apps expect it
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

const totpStep = 30 * time.Second

// totpCode returns the 6 digit code for a counter (RFC 4226 / 6238).
func totpCode(secret []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1_000_000
	return fmt.Sprintf("%06d", v)
}

// DecodeTOTPSecret reads a base32 secret as authenticator apps show it.
func DecodeTOTPSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(s, "="))
}

// VerifyTOTP accepts the code for the current step or one step either side (clock drift).
// It returns the counter that matched, so the caller can refuse to accept it twice.
func VerifyTOTP(secret []byte, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	cur := now.Unix() / int64(totpStep.Seconds())
	var matched int64 = -1
	for d := int64(-1); d <= 1; d++ {
		// Compare every candidate in constant time so timing does not show which step matched.
		if hmac.Equal([]byte(totpCode(secret, cur+d)), []byte(code)) {
			matched = cur + d
		}
	}
	return matched, matched >= 0
}

// TOTPCodeAt returns the code an authenticator would show at time t (used by tests and tooling).
func TOTPCodeAt(secret []byte, t time.Time) string {
	return totpCode(secret, t.Unix()/int64(totpStep.Seconds()))
}
