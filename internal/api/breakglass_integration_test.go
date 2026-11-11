package api_test

import (
	"context"
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"github.com/poppyszn/helmsman/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

const bgPassword = "correct horse battery staple 42"

var bgSecretRaw = []byte("12345678901234567890")

func bgEnv(t *testing.T) *env {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(bgPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return newEnvWith(t, map[string]string{
		"IDP_BREAKGLASS_ENABLED": "true", "IDP_BREAKGLASS_PASSWORD_HASH": string(hash),
		"IDP_BREAKGLASS_TOTP_SECRET": base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bgSecretRaw),
	})
}

func code(offset time.Duration) string { return auth.TOTPCodeAt(bgSecretRaw, time.Now().Add(offset)) }

func TestBreakglassIsOffByDefault(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	if code := c.json("POST", "/auth/breakglass", map[string]string{"password": "x", "code": "000000"}, nil); code != 404 {
		t.Fatalf("disabled break-glass must look absent: %d", code)
	}
	var o struct{ Breakglass bool }
	c.json("GET", "/auth/options", nil, &o)
	if o.Breakglass {
		t.Fatal("must not be offered")
	}
}

func TestBreakglassNeedsBothFactorsAndIsAudited(t *testing.T) {
	e := bgEnv(t)
	c := e.client()
	try := func(pw, cd string) int {
		return c.json("POST", "/auth/breakglass", map[string]string{"password": pw, "code": cd}, nil)
	}
	if try("wrong", code(0)) != 401 || try(bgPassword, "000000") != 401 {
		t.Fatal("either wrong factor must be refused")
	}
	if got := try(bgPassword, code(0)); got != 200 {
		t.Fatalf("both factors: %d", got)
	}
	var me struct {
		User struct {
			Login   string `json:"login"`
			IsAdmin bool   `json:"is_admin"`
		}
		Can struct{ Admin bool }
	}
	c.csrf = ""
	if st := c.json("GET", "/api/me", nil, &me); st != 200 || !me.User.IsAdmin || !me.Can.Admin || me.User.Login != "breakglass" {
		t.Fatalf("session: %d %+v", st, me)
	}
	// The membership refresh must not revoke a user who has no GitHub token.
	e.auth.RefreshAll(context.Background())
	if st := c.json("GET", "/api/me", nil, nil); st != 200 {
		t.Fatalf("refresh revoked the break-glass session: %d", st)
	}
	// The same code cannot be used twice.
	if got := e.client().json("POST", "/auth/breakglass", map[string]string{"password": bgPassword, "code": code(0)}, nil); got != 401 {
		t.Fatalf("replayed code: %d", got)
	}
	rows, _ := e.st.Pool.Query(context.Background(), `SELECT result || ':' || COALESCE(details->>'reason','') FROM audit_events WHERE action='auth.breakglass' ORDER BY id`)
	var got []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		got = append(got, s)
	}
	all := strings.Join(got, "|")
	for _, want := range []string{"denied:bad credentials", "ok:", "denied:code already used"} {
		if !strings.Contains(all, want) {
			t.Fatalf("audit missing %q in %s", want, all)
		}
	}
	if strings.Contains(all, bgPassword) {
		t.Fatal("the password must never reach the audit log")
	}
}

func TestBreakglassLocksAfterRepeatedFailures(t *testing.T) {
	e := bgEnv(t)
	c := e.client()
	for i := 0; i < 5; i++ {
		c.json("POST", "/auth/breakglass", map[string]string{"password": "nope", "code": "000000"}, nil)
	}
	if got := c.json("POST", "/auth/breakglass", map[string]string{"password": bgPassword, "code": code(0)}, nil); got != 429 {
		t.Fatalf("correct credentials must still be refused while locked: %d", got)
	}
}
