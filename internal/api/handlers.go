package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/rbac"
	"github.com/poppyszn/helmsman/internal/store"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request)    { s.Auth.HandleLogin(w, r) }
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) { s.Auth.HandleCallback(w, r) }

func (s *Server) handleBreakglass(w http.ResponseWriter, r *http.Request) {
	s.Auth.HandleBreakglass(w, r)
}

// handleAuthOptions tells the login page whether to offer the emergency sign-in.
func (s *Server) handleAuthOptions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"breakglass": s.Auth.BreakglassEnabled()})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	c := rc(r)
	s.Auth.Logout(w, r, c.SessionHash, c.User.Login)
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed out"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	c := rc(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":       c.User,
		"csrf_token": auth.CSRFToken(s.Cfg.SessionKey, c.SessionHash),
		"can": map[string]bool{
			"admin":   c.Principal.AllowedGlobal(rbac.AdminAccess),
			"audit":   c.Principal.AllowedGlobal(rbac.AuditView),
			"agents":  c.Principal.AllowedGlobal(rbac.AdminAgents),
			"approve": c.Principal.AllowedAnyEnv(rbac.ActionApprove, rbac.Wildcard),
			"mail":    c.Principal.AllowedGlobal(rbac.MailView),
		},
		"display_tz": s.Cfg.DisplayTZ,
	})
}

// envView is what app viewers see of an environment. Agent and container details stay server side.
type envView struct {
	Env       string `json:"env"`
	Tier      string `json:"tier"`
	URL       string `json:"url"`
	AllowSeed bool   `json:"allow_seed"`
	Status    string `json:"status"`
}

type appView struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Repo         string    `json:"repo"`
	Environments []envView `json:"environments"`
}

// visibleApp keeps only the envs the principal may see. ok is false when none remain.
func visibleApp(p rbac.Principal, a store.AppRow) (appView, bool) {
	v := appView{ID: a.ID, Name: a.Name, Repo: a.Repo, Environments: []envView{}}
	for _, e := range a.Envs {
		if !p.Allowed(rbac.AppView, rbac.Scope{AppID: a.ID, Env: e.Env, Tier: e.Tier}) {
			continue
		}
		var spec struct {
			URL       string `json:"url"`
			AllowSeed *bool  `json:"allow_seed"`
		}
		_ = json.Unmarshal(e.Spec, &spec)
		v.Environments = append(v.Environments, envView{Env: e.Env, Tier: e.Tier, URL: spec.URL,
			AllowSeed: spec.AllowSeed != nil && *spec.AllowSeed})
	}
	return v, len(v.Environments) > 0
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.Store.ListApps(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := []appView{}
	for _, a := range apps {
		if v, ok := visibleApp(rc(r).Principal, a); ok {
			s.fillStatus(r.Context(), &v, a)
			out = append(out, v)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.GetApp(r.Context(), chi.URLParam(r, "app"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	v, ok := visibleApp(rc(r).Principal, a)
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.fillStatus(r.Context(), &v, a)
	writeJSON(w, http.StatusOK, v)
}

// fillStatus adds the derived status to each visible env.
func (s *Server) fillStatus(ctx context.Context, v *appView, a store.AppRow) {
	rows := map[string]store.EnvRow{}
	for _, e := range a.Envs {
		rows[e.Env] = e
	}
	for i := range v.Environments {
		v.Environments[i].Status = s.statusLabel(ctx, a.ID, rows[v.Environments[i].Env])
	}
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	us, err := s.Store.ListUsers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, us)
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	rs, err := s.Store.ListRoles(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

func (s *Server) handleListBindings(w http.ResponseWriter, r *http.Request) {
	bs, err := s.Store.ListBindings(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, bs)
}

var (
	teamRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

type bindingReq struct {
	SubjectType string `json:"subject_type"` // user | team
	Subject     string `json:"subject"`      // GitHub login for user, org/team for team
	RoleID      int64  `json:"role_id"`
	AppID       string `json:"app_id"`
	Env         string `json:"env"`
}

func (s *Server) handleCreateBinding(w http.ResponseWriter, r *http.Request) {
	var req bindingReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.AppID == "" {
		req.AppID = rbac.Wildcard
	}
	if req.Env == "" {
		req.Env = rbac.Wildcard
	}
	ctx := r.Context()
	b := store.Binding{SubjectType: req.SubjectType, RoleID: req.RoleID, AppID: req.AppID, Env: req.Env}

	switch req.SubjectType {
	case "user":
		users, err := s.Store.ListUsers(ctx)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, u := range users {
			if strings.EqualFold(u.Login, req.Subject) {
				b.Subject = strconv.FormatInt(u.ID, 10)
			}
		}
		if b.Subject == "" {
			writeErr(w, http.StatusUnprocessableEntity, "unknown user: they must have signed in once")
			return
		}
	case "team":
		org, _, _ := strings.Cut(req.Subject, "/")
		if !teamRE.MatchString(req.Subject) || !containsFold(s.Cfg.AllowedOrgs, org) {
			writeErr(w, http.StatusUnprocessableEntity, "team must be org/team in an allowed org")
			return
		}
		b.Subject = req.Subject
	default:
		writeErr(w, http.StatusUnprocessableEntity, "subject_type must be user or team")
		return
	}
	roles, err := s.Store.ListRoles(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	var role string
	for _, ro := range roles {
		if ro.ID == req.RoleID {
			role = ro.Name
		}
	}
	if role == "" {
		writeErr(w, http.StatusUnprocessableEntity, "unknown role")
		return
	}
	if req.AppID != rbac.Wildcard {
		app, err := s.Store.GetApp(ctx, req.AppID)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "unknown app")
			return
		}
		if req.Env != rbac.Wildcard {
			found := false
			for _, e := range app.Envs {
				found = found || e.Env == req.Env
			}
			if !found {
				writeErr(w, http.StatusUnprocessableEntity, "unknown env for app")
				return
			}
		}
	} else if req.Env != rbac.Wildcard && !slugRE.MatchString(req.Env) {
		writeErr(w, http.StatusUnprocessableEntity, "invalid env")
		return
	}

	id, err := s.Store.CreateBinding(ctx, b, rc(r).User.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = s.Audit.Write(ctx, audit.Event{ActorType: audit.ActorUser, ActorID: rc(r).User.Login, Action: "binding.create",
		Target: req.Subject, Result: "ok", IP: auth.ClientIP(r),
		Details: map[string]any{"binding_id": id, "subject_type": req.SubjectType, "role": role, "app": req.AppID, "env": req.Env}})
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) handleDeleteBinding(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.Store.DeleteBinding(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: rc(r).User.Login, Action: "binding.delete",
		Target: strconv.FormatInt(id, 10), Result: "ok", IP: auth.ClientIP(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCatalogStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Catalog.Status())
}

func (s *Server) handleCatalogReload(w http.ResponseWriter, r *http.Request) {
	if err := s.Catalog.Reload(r.Context(), rc(r).User.Login, true); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "catalog rejected; last good catalog stays active", "status": s.Catalog.Status()})
		return
	}
	writeJSON(w, http.StatusOK, s.Catalog.Status())
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	rows, err := s.Store.ListAudit(r.Context(), limit, before)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func containsFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}
