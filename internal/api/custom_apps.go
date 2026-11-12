package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/store"
)

var customTagRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,29}$`)

// customAppInput is what an admin submits for an external tool.
type customAppInput struct {
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// clean validates the input: a short name, an http(s) address without credentials, a short description, tidy tags.
func (in *customAppInput) clean() string {
	in.Name = strings.TrimSpace(in.Name)
	if n := len([]rune(in.Name)); n < 1 || n > 60 {
		return "give the tool a name of 1 to 60 characters"
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "the address must start with http:// or https://"
	}
	if u.User != nil {
		return "the address cannot contain a user name or password"
	}
	in.URL = u.String()
	in.Description = strings.TrimSpace(in.Description)
	if len([]rune(in.Description)) > 200 {
		return "the description is limited to 200 characters"
	}
	tags := []string{}
	seen := map[string]bool{}
	for _, t := range in.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		if !customTagRE.MatchString(t) {
			return "tag \"" + t + "\" must be 1 to 30 lowercase letters, digits, dots, dashes or underscores"
		}
		seen[t] = true
		tags = append(tags, t)
	}
	if len(tags) > 20 {
		return "at most 20 tags"
	}
	in.Tags = tags
	return ""
}

func (s *Server) handleListCustomApps(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListCustomApps(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) auditCustomApp(r *http.Request, action string, id int64, d map[string]any) {
	_ = s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: rc(r).User.Login, Action: action,
		Target: "custom-app/" + strconv.FormatInt(id, 10), Result: "ok", IP: auth.ClientIP(r), Details: d})
}

func (s *Server) customAppErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrDuplicateName):
		writeErr(w, http.StatusUnprocessableEntity, "a tool with that name already exists")
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

func (s *Server) handleCreateCustomApp(w http.ResponseWriter, r *http.Request) {
	var in customAppInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<15)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if msg := in.clean(); msg != "" {
		writeErr(w, http.StatusUnprocessableEntity, msg)
		return
	}
	c, err := s.Store.CreateCustomApp(r.Context(), store.CustomApp{Name: in.Name, URL: in.URL, Description: in.Description, Tags: in.Tags}, rc(r).User.ID)
	if err != nil {
		s.customAppErr(w, err)
		return
	}
	s.auditCustomApp(r, "custom_app.created", c.ID, map[string]any{"name": c.Name, "url": c.URL})
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateCustomApp(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var in customAppInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<15)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if msg := in.clean(); msg != "" {
		writeErr(w, http.StatusUnprocessableEntity, msg)
		return
	}
	c, err := s.Store.UpdateCustomApp(r.Context(), store.CustomApp{ID: id, Name: in.Name, URL: in.URL, Description: in.Description, Tags: in.Tags})
	if err != nil {
		s.customAppErr(w, err)
		return
	}
	s.auditCustomApp(r, "custom_app.updated", id, map[string]any{"name": c.Name, "url": c.URL})
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteCustomApp(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Store.DeleteCustomApp(r.Context(), id); err != nil {
		s.customAppErr(w, err)
		return
	}
	s.auditCustomApp(r, "custom_app.deleted", id, nil)
	w.WriteHeader(http.StatusNoContent)
}
