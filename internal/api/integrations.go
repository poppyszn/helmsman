package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/integrations"
)

// lokiBody is what the admin page sends. Password is a pointer so that leaving it out keeps the
// stored one, while an empty string clears it.
type lokiBody struct {
	URL      string  `json:"url"`
	User     string  `json:"user"`
	Password *string `json:"password"`
	Enabled  bool    `json:"enabled"`
}

func (b lokiBody) input() integrations.Input {
	return integrations.Input{URL: b.URL, User: b.User, Password: b.Password, Enabled: b.Enabled}
}

func (s *Server) integrationsOff(w http.ResponseWriter) bool {
	if s.Integrations == nil {
		writeErr(w, http.StatusServiceUnavailable, "integrations are not available on this server")
		return true
	}
	return false
}

func (s *Server) auditIntegration(r *http.Request, action, result string, d map[string]any) {
	// Never the password, only what changed and who did it.
	_ = s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: rc(r).User.Login, Action: action,
		Target: "integration/loki", Result: result, IP: auth.ClientIP(r), Details: d})
}

func (s *Server) handleIntegrations(w http.ResponseWriter, _ *http.Request) {
	if s.integrationsOff(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"loki": s.Integrations.Status()})
}

func decodeLoki(w http.ResponseWriter, r *http.Request) (lokiBody, bool) {
	var b lokiBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return b, false
	}
	return b, true
}

func (s *Server) handleLokiTest(w http.ResponseWriter, r *http.Request) {
	if s.integrationsOff(w) {
		return
	}
	b, ok := decodeLoki(w, r)
	if !ok {
		return
	}
	res, err := s.Integrations.Test(r.Context(), b.input())
	var inv integrations.ErrInvalid
	if errors.As(err, &inv) {
		writeErr(w, http.StatusBadRequest, inv.Error())
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditIntegration(r, "integration.loki.tested", map[bool]string{true: "ok", false: "error"}[res.Reachable], map[string]any{"reachable": res.Reachable})
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleLokiSave(w http.ResponseWriter, r *http.Request) {
	if s.integrationsOff(w) {
		return
	}
	b, ok := decodeLoki(w, r)
	if !ok {
		return
	}
	res, err := s.Integrations.Save(r.Context(), rc(r).User, b.input())
	var inv integrations.ErrInvalid
	if errors.As(err, &inv) {
		s.auditIntegration(r, "integration.loki.saved", "error", map[string]any{"enabled": b.Enabled, "reason": inv.Error()})
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": inv.Error(), "test": res})
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	action := "integration.loki.enabled"
	if !b.Enabled {
		action = "integration.loki.disabled"
	}
	s.auditIntegration(r, action, "ok", map[string]any{"enabled": b.Enabled, "password_changed": b.Password != nil})
	writeJSON(w, http.StatusOK, map[string]any{"loki": s.Integrations.Status(), "test": res})
}

func (s *Server) handleLokiRemove(w http.ResponseWriter, r *http.Request) {
	if s.integrationsOff(w) {
		return
	}
	if err := s.Integrations.Remove(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditIntegration(r, "integration.loki.removed", "ok", nil)
	writeJSON(w, http.StatusOK, map[string]any{"loki": s.Integrations.Status()})
}
