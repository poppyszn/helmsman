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
)

// Pinger checks a dependency for readiness.
type Pinger interface{ Ping(ctx context.Context) error }

// Server is the HTTP API.
type Server struct {
	db  Pinger
	ui  fs.FS
	mux chi.Router
}

// New builds the router. It returns an error if any route lacks a permission.
func New(db Pinger, ui fs.FS) (*Server, error) {
	s := &Server{db: db, ui: ui}
	r := chi.NewRouter()
	r.Use(middleware.RealIP, middleware.Recoverer, middleware.Timeout(60*time.Second))
	for _, rt := range s.Routes() {
		if rt.Permission == "" {
			return nil, fmt.Errorf("route %s %s declares no permission", rt.Method, rt.Path)
		}
		r.Method(rt.Method, rt.Path, rt.Handler)
	}
	r.NotFound(s.serveUI)
	s.mux = r
	return s, nil
}

// Handler returns the root handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "reason": "database"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// serveUI serves the embedded SPA, falling back to index.html for client routes.
// API and WebSocket paths that are not in the route table stay 404.
func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/ws/") || strings.HasPrefix(p, "/agent/") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(p, "/")
	if name == "" {
		name = "index.html"
	}
	if f, err := s.ui.Open(name); err == nil {
		_ = f.Close()
		http.ServeFileFS(w, r, s.ui, name)
		return
	}
	if _, err := fs.Stat(s.ui, "index.html"); err != nil {
		http.Error(w, "UI not built. Run the web build.", http.StatusNotFound)
		return
	}
	http.ServeFileFS(w, r, s.ui, "index.html")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
