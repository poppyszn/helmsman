// Package api holds HTTP handlers and the central route table.
package api

import "net/http"

// Special permission values. Every route must declare one of these or a real
// permission from package rbac. There is no default: an empty value fails to mount.
const (
	// Public routes need no session (probes, the login flow).
	Public = "public"
	// Authenticated routes need a valid session but no permission (own profile, logout).
	Authenticated = "authenticated"
)

// ScopeKind says what a permission applies to.
type ScopeKind int

const (
	// ScopeNone is for Public and Authenticated routes.
	ScopeNone ScopeKind = iota
	// ScopeGlobal needs the permission on every app and env (admin pages, audit).
	ScopeGlobal
	// ScopeAny needs the permission on at least one app or env; the handler filters results.
	ScopeAny
	// ScopeApp needs the permission on at least one env of {app}; the handler filters envs.
	ScopeApp
	// ScopeEnv needs the permission on {app} + {env}.
	ScopeEnv
)

// Route is one entry of the central route table.
type Route struct {
	Method     string
	Path       string
	Permission string
	Scope      ScopeKind
	Handler    http.HandlerFunc
	// Limited applies the login rate limit (unauthenticated endpoints that start auth).
	Limited bool
}

// Routes returns the route table. Deny by default: anything not listed is a 404,
// and a route without a Permission refuses to mount.
func (s *Server) Routes() []Route {
	const (
		get  = http.MethodGet
		post = http.MethodPost
		del  = http.MethodDelete
	)
	return []Route{
		{get, "/healthz", Public, ScopeNone, s.handleHealthz, false},
		{get, "/readyz", Public, ScopeNone, s.handleReadyz, false},

		{get, "/auth/github/login", Public, ScopeNone, s.handleLogin, true},
		{get, "/auth/github/callback", Public, ScopeNone, s.handleCallback, true},
		{post, "/auth/logout", Authenticated, ScopeNone, s.handleLogout, false},

		{get, "/api/me", Authenticated, ScopeNone, s.handleMe, false},
		{get, "/api/apps", "app.view", ScopeAny, s.handleListApps, false},
		{get, "/api/apps/{app}", "app.view", ScopeApp, s.handleGetApp, false},

		{get, "/api/admin/users", "admin.access", ScopeGlobal, s.handleListUsers, false},
		{get, "/api/admin/roles", "admin.access", ScopeGlobal, s.handleListRoles, false},
		{get, "/api/admin/bindings", "admin.access", ScopeGlobal, s.handleListBindings, false},
		{post, "/api/admin/bindings", "admin.access", ScopeGlobal, s.handleCreateBinding, false},
		{del, "/api/admin/bindings/{id}", "admin.access", ScopeGlobal, s.handleDeleteBinding, false},
		{get, "/api/admin/catalog", "admin.access", ScopeGlobal, s.handleCatalogStatus, false},
		{post, "/api/admin/catalog/reload", "admin.access", ScopeGlobal, s.handleCatalogReload, false},
		{get, "/api/admin/audit", "audit.view", ScopeGlobal, s.handleAudit, false},
	}
}
