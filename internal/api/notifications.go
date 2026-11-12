package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/notify"
	"github.com/poppyszn/helmsman/internal/store"
)

func (s *Server) notifyOff(w http.ResponseWriter) bool {
	if s.Notify == nil {
		writeErr(w, http.StatusServiceUnavailable, "notifications are not available on this server")
		return true
	}
	return false
}

func (s *Server) auditNotify(r *http.Request, action, target, result string, d map[string]any) {
	// What changed and who did it. Never a password or a signing secret.
	_ = s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: rc(r).User.Login, Action: action,
		Target: target, Result: result, IP: auth.ClientIP(r), Details: d})
}

func (s *Server) notifyErr(w http.ResponseWriter, err error) {
	var inv notify.ErrInvalid
	switch {
	case errors.As(err, &inv):
		writeErr(w, http.StatusUnprocessableEntity, inv.Error())
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		s.Log.Error("notifications", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	if s.notifyOff(w) {
		return
	}
	smtp, err := s.Notify.SMTP(r.Context())
	if err != nil {
		s.notifyErr(w, err)
		return
	}
	chans, err := s.Notify.Channels(r.Context())
	if err != nil {
		s.notifyErr(w, err)
		return
	}
	del, err := s.Notify.Deliveries(r.Context(), 50)
	if err != nil {
		s.notifyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"smtp": smtp, "channels": chans, "deliveries": del, "events": notify.Types})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func (s *Server) handleSMTPSave(w http.ResponseWriter, r *http.Request) {
	if s.notifyOff(w) {
		return
	}
	var in notify.SMTPInput
	if !decodeBody(w, r, &in) {
		return
	}
	if err := s.Notify.SaveSMTP(r.Context(), rc(r).User, in); err != nil {
		s.notifyErr(w, err)
		return
	}
	s.auditNotify(r, "notifications.smtp.saved", "notifications/smtp", "ok", map[string]any{"host": in.Host, "security": in.Security, "password_changed": in.Password != nil})
	st, _ := s.Notify.SMTP(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"smtp": st})
}

func (s *Server) handleSMTPRemove(w http.ResponseWriter, r *http.Request) {
	if s.notifyOff(w) {
		return
	}
	if err := s.Notify.RemoveSMTP(r.Context()); err != nil {
		s.notifyErr(w, err)
		return
	}
	s.auditNotify(r, "notifications.smtp.removed", "notifications/smtp", "ok", nil)
	writeJSON(w, http.StatusOK, map[string]any{"smtp": notify.SMTPStatus{}})
}

func (s *Server) handleSMTPTest(w http.ResponseWriter, r *http.Request) {
	if s.notifyOff(w) {
		return
	}
	var in struct {
		To string `json:"to"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	err := s.Notify.TestSMTP(r.Context(), in.To)
	var inv notify.ErrInvalid
	if errors.As(err, &inv) {
		writeErr(w, http.StatusUnprocessableEntity, inv.Error())
		return
	}
	res := map[bool]string{true: "ok", false: "error"}[err == nil]
	s.auditNotify(r, "notifications.smtp.tested", "notifications/smtp", res, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleChannelCreate(w http.ResponseWriter, r *http.Request) {
	if s.notifyOff(w) {
		return
	}
	var in notify.ChannelInput
	if !decodeBody(w, r, &in) {
		return
	}
	c, secret, err := s.Notify.CreateChannel(r.Context(), rc(r).User, in)
	if err != nil {
		s.notifyErr(w, err)
		return
	}
	s.auditNotify(r, "notifications.channel.created", "notifications/channel/"+strconv.FormatInt(c.ID, 10), "ok",
		map[string]any{"name": c.Name, "kind": c.Kind, "events": c.Events, "tier": c.Tier})
	out := map[string]any{"channel": c}
	if secret != "" {
		out["secret"] = secret // shown once
	}
	writeJSON(w, http.StatusCreated, out)
}

func channelID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return 0, false
	}
	return id, true
}

func (s *Server) handleChannelUpdate(w http.ResponseWriter, r *http.Request) {
	if s.notifyOff(w) {
		return
	}
	id, ok := channelID(w, r)
	if !ok {
		return
	}
	var in notify.ChannelInput
	if !decodeBody(w, r, &in) {
		return
	}
	c, secret, err := s.Notify.UpdateChannel(r.Context(), id, in)
	if err != nil {
		s.notifyErr(w, err)
		return
	}
	s.auditNotify(r, "notifications.channel.updated", "notifications/channel/"+strconv.FormatInt(id, 10), "ok",
		map[string]any{"name": c.Name, "events": c.Events, "tier": c.Tier, "enabled": c.Enabled, "secret_changed": secret != ""})
	writeJSON(w, http.StatusOK, map[string]any{"channel": c})
}

func (s *Server) handleChannelDelete(w http.ResponseWriter, r *http.Request) {
	if s.notifyOff(w) {
		return
	}
	id, ok := channelID(w, r)
	if !ok {
		return
	}
	if err := s.Notify.DeleteChannel(r.Context(), id); err != nil {
		s.notifyErr(w, err)
		return
	}
	s.auditNotify(r, "notifications.channel.deleted", "notifications/channel/"+strconv.FormatInt(id, 10), "ok", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleChannelTest(w http.ResponseWriter, r *http.Request) {
	if s.notifyOff(w) {
		return
	}
	id, ok := channelID(w, r)
	if !ok {
		return
	}
	err := s.Notify.TestChannel(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	res := map[bool]string{true: "ok", false: "error"}[err == nil]
	s.auditNotify(r, "notifications.channel.tested", "notifications/channel/"+strconv.FormatInt(id, 10), res, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
