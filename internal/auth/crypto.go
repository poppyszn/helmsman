// Package auth implements GitHub OAuth login, server-side sessions and the
// membership refresh job.
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
)

// Encrypt seals plaintext with AES-256-GCM. The nonce is prepended.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt.
func Decrypt(key, sealed []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		key32 := sha256.Sum256(key)
		key = key32[:]
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// RandomToken returns n random bytes, base64url encoded.
func RandomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken returns the SHA-256 of a token, which is what the database stores.
func HashToken(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

// Sign returns value.mac for use in short-lived signed cookies.
func Sign(key []byte, value string) string {
	return value + "." + mac(key, value)
}

// Verify checks a Sign output and returns the value.
func Verify(key []byte, signed string) (string, bool) {
	for i := len(signed) - 1; i >= 0; i-- {
		if signed[i] == '.' {
			v, m := signed[:i], signed[i+1:]
			return v, subtle.ConstantTimeCompare([]byte(m), []byte(mac(key, v))) == 1
		}
	}
	return "", false
}

func mac(key []byte, v string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(v))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// CSRFToken derives the per-session CSRF token from the session hash.
func CSRFToken(key, sessionHash []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte("csrf:"))
	h.Write(sessionHash)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
