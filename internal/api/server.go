package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/poppyszn/helmsman/internal/actions"
	"github.com/poppyszn/helmsman/internal/agenthub"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/catalog"
	"github.com/poppyszn/helmsman/internal/config"
	"github.com/poppyszn/helmsman/internal/store"
)

// Pinger checks a dependency for readiness.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are the server's collaborators. Only DB and UI are needed for the probe routes.
type Deps struct {
	Cfg     config.Config
	DB      Pinger
	Store   *store.Store
	Audit   *audit.Writer
	Auth    *auth.Service
	Catalog *catalog.Manager
	Hub     *agenthub.Hub
	Actions *actions.Service
	Logs    agenthub.LogSource
	UI      fs.FS
	// LogRecheck is how often open log streams re-check access (default 60 s).
	LogRecheck time.Duration
	// ActionsPerMinute caps state-changing requests per user (default 30); LogOpensPerMinute caps
	// log streams opened per user (default 60).
	ActionsPerMinute  int
	LogOpensPerMinute int
}

// Server is the HTTP API.
type Server struct {
	Deps
	mux           chi.Router
	limiter       *rateLimiter
	actionLimiter *rateLimiter
	logLimiter    *rateLimiter
	restarts      inflight
}

// New builds the router. It returns an error if any route lacks a permission.
func New(d Deps) (*Server, error) {
	if d.ActionsPerMinute <= 0 {
		d.ActionsPerMinute = 30
	}
	if d.LogOpensPerMinute <= 0 {
		d.LogOpensPerMinute = 60
	}
	s := &Server{Deps: d, limiter: newRateLimiter(20, time.Minute),
		actionLimiter: newRateLimiter(d.ActionsPerMinute, time.Minute), logLimiter: newRateLimiter(d.LogOpensPerMinute, time.Minute)}
	r := chi.NewRouter()
	r.Use(middleware.RealIP, middleware.Recoverer, timeoutExceptSockets(60*time.Second), securityHeaders)
	for _, rt := range s.Routes() {
		if rt.Permission == "" {
			return nil, fmt.Errorf("route %s %s declares no permission", rt.Method, rt.Path)
		}
		r.Method(rt.Method, rt.Path, s.guard(rt))
	}
	r.NotFound(s.serveUI)
	s.mux = r
	return s, nil
}

// timeoutExceptSockets applies a request timeout to everything but WebSocket endpoints,
// which stay open for as long as the peer does.
func timeoutExceptSockets(d time.Duration) func(http.Handler) http.Handler {
	t := middleware.Timeout(d)
	return func(next http.Handler) http.Handler {
		limited := t(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/ws/") || r.URL.Path == "/agent/ws" {
				next.ServeHTTP(w, r)
				return
			}
			limited.ServeHTTP(w, r)
		})
	}
}

// Handler returns the root handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.DB.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "reason": "database"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// serveUI serves the embedded SPA, falling back to index.html for client routes.
// API and WebSocket paths that are not in the route table stay 404.
func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/ws/") || strings.HasPrefix(p, "/agent/") || strings.HasPrefix(p, "/auth/") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(p, "/")
	if name == "" {
		name = "index.html"
	}
	if f, err := s.UI.Open(name); err == nil {
		_ = f.Close()
		http.ServeFileFS(w, r, s.UI, name)
		return
	}
	if _, err := fs.Stat(s.UI, "index.html"); err != nil {
		http.Error(w, "UI not built. Run the web build.", http.StatusNotFound)
		return
	}
	http.ServeFileFS(w, r, s.UI, "index.html")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
