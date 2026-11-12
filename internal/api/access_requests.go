package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/store"
)

// handleAccessRequests lists people who signed in and have no access yet. Granting is the ordinary
// create-a-binding call; once they have a role they drop off this list.
func (s *Server) handleAccessRequests(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.PendingAccess(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleDismissAccessRequest hides a request until the person signs in again.
func (s *Server) handleDismissAccessRequest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Store.DismissAccessRequest(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: rc(r).User.Login, Action: "access.request.dismissed",
		Target: "user/" + strconv.FormatInt(id, 10), Result: "ok", IP: auth.ClientIP(r)})
	w.WriteHeader(http.StatusNoContent)
}
