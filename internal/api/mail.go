package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/integrations"
	"github.com/poppyszn/helmsman/internal/mailpit"
)

var (
	mailIDRE   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	mailPartRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
)

// mailReader returns the connected Mailpit, or writes the answer for "not connected" and returns false.
func (s *Server) mailReader(w http.ResponseWriter) (mailpit.Reader, bool) {
	if s.Mail != nil {
		if r := s.Mail.Current(); r != nil {
			return r, true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": false, "total": 0, "unread": 0, "messages": []any{}})
	return nil, false
}

func (s *Server) mailErr(w http.ResponseWriter, err error) {
	if mailpit.IsNotFound(err) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.Log.Warn("mail request failed", "err", err)
	writeErr(w, http.StatusBadGateway, "could not read from Mailpit. An admin can check the connection on the Integrations page")
}

func (s *Server) auditMail(r *http.Request, action string, d map[string]any) {
	// What was opened, never what it says.
	if err := s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: rc(r).User.Login, Action: action,
		Target: "mail", Result: "ok", IP: auth.ClientIP(r), Details: d}); err != nil {
		s.Log.Error("audit write failed", "action", action, "err", err)
	}
}

// handleMailList lists messages, newest first. q uses Mailpit's search syntax (to:, from:, subject:, or words).
func (s *Server) handleMailList(w http.ResponseWriter, r *http.Request) {
	m, ok := s.mailReader(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	start, _ := strconv.Atoi(q.Get("start"))
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > 100 {
		limit = 50
	}
	if start < 0 {
		start = 0
	}
	query := q.Get("q")
	if len(query) > 200 {
		writeErr(w, http.StatusBadRequest, "the search is too long")
		return
	}
	l, err := m.List(r.Context(), query, start, limit)
	if err != nil {
		s.mailErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "total": l.Total, "unread": l.Unread, "messages": l.Messages})
}

// handleMailMessage returns one message with its HTML, text and attachment list.
func (s *Server) handleMailMessage(w http.ResponseWriter, r *http.Request) {
	m, ok := s.mailReader(w)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if !mailIDRE.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	msg, err := m.Message(r.Context(), id)
	if err != nil {
		s.mailErr(w, err)
		return
	}
	s.auditMail(r, "mail.view", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "message": msg})
}

// handleMailAttachment downloads one attachment. It is always sent as a download, never rendered, so a
// crafted file cannot run in Helmsman's origin.
func (s *Server) handleMailAttachment(w http.ResponseWriter, r *http.Request) {
	m, ok := s.mailReader(w)
	if !ok {
		return
	}
	id, part := chi.URLParam(r, "id"), chi.URLParam(r, "part")
	if !mailIDRE.MatchString(id) || !mailPartRE.MatchString(part) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	msg, err := m.Message(r.Context(), id)
	if err != nil {
		s.mailErr(w, err)
		return
	}
	name := ""
	for _, a := range append(append([]mailpit.Attachment{}, msg.Attachments...), msg.Inline...) {
		if a.PartID == part {
			name = a.FileName
		}
	}
	if name == "" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	body, _, err := m.Attachment(r.Context(), id, part)
	if err != nil {
		s.mailErr(w, err)
		return
	}
	defer body.Close()
	s.auditMail(r, "mail.attachment", map[string]any{"id": id, "part": part})
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	_, _ = io.Copy(w, io.LimitReader(body, 64<<20))
}

// Admin: connect Mailpit from the Integrations page.

func (s *Server) mailAdminOff(w http.ResponseWriter) bool {
	if s.MailAdmin == nil {
		writeErr(w, http.StatusServiceUnavailable, "the mail integration is not available on this server")
		return true
	}
	return false
}

func (s *Server) auditMailAdmin(r *http.Request, action, result string, d map[string]any) {
	// Never the password, only what changed and who did it.
	_ = s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: rc(r).User.Login, Action: action,
		Target: "integration/mailpit", Result: result, IP: auth.ClientIP(r), Details: d})
}

func decodeMailBody(w http.ResponseWriter, r *http.Request) (lokiBody, bool) {
	var b lokiBody // same fields: url, user, password, enabled
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return b, false
	}
	return b, true
}

func (s *Server) handleMailpitTest(w http.ResponseWriter, r *http.Request) {
	if s.mailAdminOff(w) {
		return
	}
	b, ok := decodeMailBody(w, r)
	if !ok {
		return
	}
	res, err := s.MailAdmin.Test(r.Context(), b.input())
	var inv integrations.ErrInvalid
	if errors.As(err, &inv) {
		writeErr(w, http.StatusBadRequest, inv.Error())
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditMailAdmin(r, "integration.mailpit.tested", map[bool]string{true: "ok", false: "error"}[res.Reachable], map[string]any{"reachable": res.Reachable})
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleMailpitSave(w http.ResponseWriter, r *http.Request) {
	if s.mailAdminOff(w) {
		return
	}
	b, ok := decodeMailBody(w, r)
	if !ok {
		return
	}
	res, err := s.MailAdmin.Save(r.Context(), rc(r).User, b.input())
	var inv integrations.ErrInvalid
	if errors.As(err, &inv) {
		s.auditMailAdmin(r, "integration.mailpit.saved", "error", map[string]any{"enabled": b.Enabled, "reason": inv.Error()})
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": inv.Error(), "test": res})
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	action := "integration.mailpit.enabled"
	if !b.Enabled {
		action = "integration.mailpit.disabled"
	}
	s.auditMailAdmin(r, action, "ok", map[string]any{"enabled": b.Enabled, "password_changed": b.Password != nil})
	writeJSON(w, http.StatusOK, map[string]any{"mailpit": s.MailAdmin.Status(), "test": res})
}

func (s *Server) handleMailpitRemove(w http.ResponseWriter, r *http.Request) {
	if s.mailAdminOff(w) {
		return
	}
	if err := s.MailAdmin.Remove(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditMailAdmin(r, "integration.mailpit.removed", "ok", nil)
	writeJSON(w, http.StatusOK, map[string]any{"mailpit": s.MailAdmin.Status()})
}
