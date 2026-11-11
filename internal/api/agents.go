package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/agenthub"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/store"
	"github.com/poppyszn/helmsman/pkg/protocol"
)

type agentAdminView struct {
	store.AgentRow
	Online   bool `json:"online"`
	Outdated bool `json:"outdated"`
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	as, err := s.Store.ListAgents(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]agentAdminView, 0, len(as))
	for _, a := range as {
		on, _ := s.Hub.Online(a.ID)
		out = append(out, agentAdminView{AgentRow: a, Online: on,
			Outdated: a.ProtocolVersion != nil && *a.ProtocolVersion < protocol.Version})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !slugRE.MatchString(req.ID) {
		writeErr(w, http.StatusUnprocessableEntity, "id must be lowercase letters, digits and dashes")
		return
	}
	if req.Name == "" || len(req.Name) > 100 {
		writeErr(w, http.StatusUnprocessableEntity, "name is required (up to 100 characters)")
		return
	}
	tok, hash, err := agenthub.NewToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	exp := time.Now().Add(agenthub.JoinTokenTTL)
	if err := s.Store.CreateAgent(r.Context(), req.ID, req.Name, hash, exp); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "an agent with this id already exists")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditAgent(r, "agent.create", req.ID, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"id": req.ID, "join_token": tok, "expires_at": exp})
}

func (s *Server) handleAgentJoinToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tok, hash, err := agenthub.NewToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	exp := time.Now().Add(agenthub.JoinTokenTTL)
	if err := s.Store.ReissueJoin(r.Context(), id, hash, exp); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "this agent already joined or was revoked; create a new agent instead")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditAgent(r, "agent.join_token", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "join_token": tok, "expires_at": exp})
}

func (s *Server) handleRevokeAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.Store.RevokeAgent(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found or already revoked")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.Hub.Disconnect(id) // closes the socket now; the credential no longer matches anyway
	s.auditAgent(r, "agent.revoke", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAckAgentAlert(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.Store.SetAgentAlert(r.Context(), id, ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditAgent(r, "agent.alert_ack", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) auditAgent(r *http.Request, action, id string, d map[string]any) {
	_ = s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: rc(r).User.Login, Action: action,
		Target: id, Result: "ok", IP: auth.ClientIP(r), Details: d})
}

func (s *Server) handleAgentWS(w http.ResponseWriter, r *http.Request) { s.Hub.ServeWS(w, r) }
func (s *Server) handleAgentJoin(w http.ResponseWriter, r *http.Request) {
	s.Hub.HandleJoin(w, r)
}
