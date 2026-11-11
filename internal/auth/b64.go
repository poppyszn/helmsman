package auth

import "encoding/base64"

func b64(b []byte) []byte {
	out := make([]byte, base64.RawURLEncoding.EncodedLen(len(b)))
	base64.RawURLEncoding.Encode(out, b)
	return out
}

func unb64(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
