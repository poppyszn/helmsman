package auth

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/poppyszn/helmsman/internal/audit"
	"golang.org/x/crypto/bcrypt"
)

// BreakglassSessionTTL is deliberately short: this path exists for emergencies.
const BreakglassSessionTTL = time.Hour

const (
	breakglassMaxFailures = 5
	breakglassLockout     = 15 * time.Minute
)

// breakglassState is the process-wide failure counter and replay guard. It is global, not per IP:
// an attacker should not get fresh attempts by changing address.
type breakglassState struct {
	mu          sync.Mutex
	failures    int
	lockedUntil time.Time
	lastCounter int64
}

// BreakglassEnabled reports whether the local admin path is switched on and fully configured.
func (s *Service) BreakglassEnabled() bool {
	c := s.Cfg
	return c.BreakglassEnabled && c.BreakglassPasswordHash != "" && c.BreakglassTOTPSecret != ""
}

// HandleBreakglass signs in the local emergency admin with a bcrypt password and a TOTP code.
// Every attempt is audited, successful or not, and the password and code are never logged.
func (s *Service) HandleBreakglass(w http.ResponseWriter, r *http.Request) {
	if !s.BreakglassEnabled() {
		http.NotFound(w, r)
		return
	}
	ip := ClientIP(r)
	audit := func(result, reason string) {
		_ = s.Audit.Write(r.Context(), auditEvent(s.Cfg.BreakglassUser, "auth.breakglass", ip, result, reason))
	}
	deny := func(code int, msg, reason string) {
		audit("denied", reason)
		writeJSONErr(w, code, msg)
	}
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		deny(http.StatusBadRequest, "invalid request", "bad body")
		return
	}

	st := &s.bg
	st.mu.Lock()
	if time.Now().Before(st.lockedUntil) {
		st.mu.Unlock()
		deny(http.StatusTooManyRequests, "break-glass sign-in is locked after repeated failures; try again later", "locked")
		return
	}
	st.mu.Unlock()

	// Always check both factors so a wrong password and a wrong code look the same.
	pwOK := bcrypt.CompareHashAndPassword([]byte(s.Cfg.BreakglassPasswordHash), []byte(in.Password)) == nil
	secret, err := DecodeTOTPSecret(s.Cfg.BreakglassTOTPSecret)
	if err != nil {
		deny(http.StatusInternalServerError, "break-glass is misconfigured", "bad totp secret")
		return
	}
	counter, codeOK := VerifyTOTP(secret, in.Code, time.Now())

	st.mu.Lock()
	replay := codeOK && counter <= st.lastCounter
	ok := pwOK && codeOK && !replay
	if ok {
		st.failures, st.lastCounter = 0, counter
	} else {
		st.failures++
		if st.failures >= breakglassMaxFailures {
			st.lockedUntil = time.Now().Add(breakglassLockout)
			st.failures = 0
		}
	}
	locked := time.Now().Before(st.lockedUntil)
	st.mu.Unlock()

	if !ok {
		reason := "bad credentials"
		if replay {
			reason = "code already used"
		}
		if locked {
			reason += "; locked"
		}
		deny(http.StatusUnauthorized, "invalid password or code", reason)
		return
	}

	u, err := s.Store.UpsertBreakglass(r.Context(), s.Cfg.BreakglassUser)
	if err != nil {
		deny(http.StatusInternalServerError, "could not sign in", "store error")
		return
	}
	sid := RandomToken(32)
	if err := s.Store.CreateSession(r.Context(), HashToken(sid), u.ID, BreakglassSessionTTL, ip, r.UserAgent()); err != nil {
		deny(http.StatusInternalServerError, "could not sign in", "session error")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: sid, Path: "/", MaxAge: int(BreakglassSessionTTL.Seconds()),
		HttpOnly: true, Secure: s.Cfg.SecureCookies(), SameSite: http.SameSiteLaxMode})
	audit("ok", "")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"signed in"}`))
}

func writeJSONErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func auditEvent(login, action, ip, result, reason string) audit.Event {
	d := map[string]any{}
	if reason != "" {
		d["reason"] = reason
	}
	return audit.Event{ActorType: audit.ActorUser, ActorID: login, Action: action, Target: login, Result: result, IP: ip, Details: d}
}
