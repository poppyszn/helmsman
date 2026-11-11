// Command breakglass-init generates the credentials for the optional break-glass admin: a random
// password, its bcrypt hash and a TOTP secret. Run it on an admin's machine, store the password
// and add the secret to an authenticator app, then put only the two IDP_BREAKGLASS_* values
// printed at the end into the server's environment. The plain password is shown once.
package main

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"math/big"
	"os"

	"golang.org/x/crypto/bcrypt"
)

const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func main() {
	pw := make([]byte, 28)
	for i := range pw {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			fail(err)
		}
		pw[i] = alphabet[n.Int64()]
	}
	hash, err := bcrypt.GenerateFromPassword(pw, 12)
	if err != nil {
		fail(err)
	}
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		fail(err)
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	user := "breakglass"
	if len(os.Args) > 1 {
		user = os.Args[1]
	}
	fmt.Printf("Password (store it in your password manager, it is not kept anywhere else):\n  %s\n\n", pw)
	fmt.Printf("TOTP secret (add to an authenticator app as a time-based key):\n  %s\n  otpauth://totp/Helmsman:%s?secret=%s&issuer=Helmsman\n\n", secret, user, secret)
	fmt.Printf("Server environment (no secrets here, only the hash and the TOTP key):\n")
	fmt.Printf("IDP_BREAKGLASS_ENABLED=true\nIDP_BREAKGLASS_USER=%s\nIDP_BREAKGLASS_PASSWORD_HASH='%s'\nIDP_BREAKGLASS_TOTP_SECRET=%s\n", user, hash, secret)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
