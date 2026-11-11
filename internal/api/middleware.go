package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/rbac"
	"github.com/poppyszn/helmsman/internal/store"
)

type ctxKey struct{}

// reqCtx is what the guard attaches for handlers behind a session.
type reqCtx struct {
	User        store.User
	Principal   rbac.Principal
	SessionHash []byte
}

func rc(r *http.Request) reqCtx { v, _ := r.Context().Value(ctxKey{}).(reqCtx); return v }

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https://avatars.githubusercontent.com; "+
			"style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// guard wraps a route with authentication, CSRF and the permission check its table entry declares.
func (s *Server) guard(rt Route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rt.Limited && !s.limiter.allow(auth.ClientIP(r)) {
			writeErr(w, http.StatusTooManyRequests, "too many attempts, try again shortly")
			return
		}
		if rt.Permission == Public {
			rt.Handler(w, r)
			return
		}
		c, err := r.Cookie(auth.SessionCookie)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "not signed in")
			return
		}
		hash := auth.HashToken(c.Value)
		user, err := s.Store.SessionUser(r.Context(), hash)
		if errors.Is(err, store.ErrNoSession) {
			writeErr(w, http.StatusUnauthorized, "session expired or revoked")
			return
		} else if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			want := auth.CSRFToken(s.Cfg.SessionKey, hash)
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(want)) != 1 {
				writeErr(w, http.StatusForbidden, "missing or invalid CSRF token")
				return
			}
		}
		p, err := s.Store.Principal(r.Context(), user)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		if rt.Permission != Authenticated && !s.permitted(w, r, rt, p) {
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, reqCtx{User: user, Principal: p, SessionHash: hash})
		rt.Handler(w, r.WithContext(ctx))
	})
}

// permitted evaluates rt.Permission against the route's scope and writes the denial.
// A user lacking the permission always gets 403, whether or not the target exists.
func (s *Server) permitted(w http.ResponseWriter, r *http.Request, rt Route, p rbac.Principal) bool {
	deny := func() bool { writeErr(w, http.StatusForbidden, "forbidden"); return false }
	switch rt.Scope {
	case ScopeGlobal:
		if !p.AllowedGlobal(rt.Permission) {
			return deny()
		}
	case ScopeAny:
		if !p.AllowedAnyEnv(rt.Permission, rbac.Wildcard) {
			return deny()
		}
	case ScopeApp:
		app := chi.URLParam(r, "app")
		if !p.AllowedAnyEnv(rt.Permission, app) {
			return deny()
		}
	case ScopeEnv:
		app, env := chi.URLParam(r, "app"), chi.URLParam(r, "env")
		tier, err := s.Store.EnvTier(r.Context(), app, env)
		if errors.Is(err, store.ErrNotFound) {
			if !p.AllowedAnyEnv(rt.Permission, app) {
				return deny()
			}
			writeErr(w, http.StatusNotFound, "not found")
			return false
		} else if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return false
		}
		if !p.Allowed(rt.Permission, rbac.Scope{AppID: app, Env: env, Tier: tier}) {
			return deny()
		}
	default:
		return deny() // a permission route without a scope is a table bug: fail closed
	}
	return true
}

// rateLimiter is a small fixed-window limiter keyed by IP.
type rateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*bucket
}

type bucket struct {
	n     int
	start time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window, hits: map[string]*bucket{}}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b := l.hits[key]
	if b == nil || now.Sub(b.start) > l.window {
		if len(l.hits) > 10000 {
			l.hits = map[string]*bucket{}
		}
		l.hits[key] = &bucket{n: 1, start: now}
		return true
	}
	b.n++
	return b.n <= l.max
}
