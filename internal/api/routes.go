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
		put  = http.MethodPut
	)
	return []Route{
		{get, "/healthz", Public, ScopeNone, s.handleHealthz, false},
		{get, "/readyz", Public, ScopeNone, s.handleReadyz, false},

		{get, "/auth/github/login", Public, ScopeNone, s.handleLogin, true},
		{get, "/auth/github/callback", Public, ScopeNone, s.handleCallback, true},
		{get, "/auth/options", Public, ScopeNone, s.handleAuthOptions, false},
		{post, "/auth/breakglass", Public, ScopeNone, s.handleBreakglass, true},
		{post, "/auth/logout", Authenticated, ScopeNone, s.handleLogout, false},

		{get, "/api/me", Authenticated, ScopeNone, s.handleMe, false},
		{get, "/api/apps", "app.view", ScopeAny, s.handleListApps, false},
		{get, "/api/apps/{app}", "app.view", ScopeApp, s.handleGetApp, false},
		{get, "/api/apps/{app}/envs/{env}/status", "app.view", ScopeEnv, s.handleEnvStatus, false},
		{post, "/api/apps/{app}/envs/{env}/restart", "action.restart", ScopeEnv, s.handleRestart, false},
		{get, "/ws/apps/{app}/envs/{env}/logs", "logs.view", ScopeEnv, s.handleLogsWS, false},
		{get, "/api/apps/{app}/envs/{env}/logs/history", "logs.view", ScopeEnv, s.handleLogHistory, false},

		// M3: migrations, seeds and deployments through GitHub Actions. Reject, cancel and rerun
		// need only deploy.view here; the service then checks the permission of the run's action
		// (or approver status, or that the caller is the requester withdrawing their own request).
		{get, "/api/apps/{app}/envs/{env}/runs", "deploy.view", ScopeEnv, s.handleListRuns, false},
		{get, "/api/apps/{app}/envs/{env}/runs/{id}", "deploy.view", ScopeEnv, s.handleGetRun, false},
		{get, "/api/apps/{app}/envs/{env}/runs/{id}/logs", "deploy.view", ScopeEnv, s.handleRunLogs, false},
		{post, "/api/apps/{app}/envs/{env}/actions/migrate", "action.migrate", ScopeEnv, s.startRun("migrate"), false},
		{post, "/api/apps/{app}/envs/{env}/actions/seed", "action.seed", ScopeEnv, s.startRun("seed"), false},
		{post, "/api/apps/{app}/envs/{env}/runs/{id}/approve", "action.approve", ScopeEnv, s.handleApprove(), false},
		{post, "/api/apps/{app}/envs/{env}/runs/{id}/reject", "deploy.view", ScopeEnv, s.handleReject(), false},
		{post, "/api/apps/{app}/envs/{env}/runs/{id}/cancel", "deploy.view", ScopeEnv, s.handleCancel(), false},
		{post, "/api/apps/{app}/envs/{env}/runs/{id}/rerun", "deploy.view", ScopeEnv, s.handleRerun, false},
		{get, "/api/apps/{app}/envs/{env}/deployments", "deploy.view", ScopeEnv, s.handleDeployments, false},
		{get, "/api/apps/{app}/envs/{env}/deployments/{run}/jobs", "deploy.view", ScopeEnv, s.handleDeploymentJobs, false},
		{get, "/api/apps/{app}/envs/{env}/deployments/{run}/jobs/{job}/logs", "deploy.view", ScopeEnv, s.handleDeploymentJobLog, false},
		{get, "/api/approvals", "action.approve", ScopeAny, s.handleApprovals, false},

		{get, "/api/admin/users", "admin.access", ScopeGlobal, s.handleListUsers, false},
		{get, "/api/admin/roles", "admin.access", ScopeGlobal, s.handleListRoles, false},
		{get, "/api/admin/bindings", "admin.access", ScopeGlobal, s.handleListBindings, false},
		{post, "/api/admin/bindings", "admin.access", ScopeGlobal, s.handleCreateBinding, false},
		{del, "/api/admin/bindings/{id}", "admin.access", ScopeGlobal, s.handleDeleteBinding, false},
		{get, "/api/admin/catalog", "admin.access", ScopeGlobal, s.handleCatalogStatus, false},
		{post, "/api/admin/catalog/reload", "admin.access", ScopeGlobal, s.handleCatalogReload, false},
		{get, "/api/admin/github", "admin.access", ScopeGlobal, s.handleGitHubStatus, false},
		{post, "/api/admin/github/check", "admin.access", ScopeGlobal, s.handleGitHubCheck, false},
		{get, "/api/admin/integrations", "admin.access", ScopeGlobal, s.handleIntegrations, false},
		{post, "/api/admin/integrations/loki/test", "admin.access", ScopeGlobal, s.handleLokiTest, false},
		{put, "/api/admin/integrations/loki", "admin.access", ScopeGlobal, s.handleLokiSave, false},
		{del, "/api/admin/integrations/loki", "admin.access", ScopeGlobal, s.handleLokiRemove, false},
		{post, "/api/admin/integrations/mailpit/test", "admin.access", ScopeGlobal, s.handleMailpitTest, false},
		{put, "/api/admin/integrations/mailpit", "admin.access", ScopeGlobal, s.handleMailpitSave, false},
		{del, "/api/admin/integrations/mailpit", "admin.access", ScopeGlobal, s.handleMailpitRemove, false},
		// The Mail page: messages the development environments sent, read from the connected Mailpit.
		{get, "/api/mail/messages", "mail.view", ScopeGlobal, s.handleMailList, false},
		{get, "/api/mail/messages/{id}", "mail.view", ScopeGlobal, s.handleMailMessage, false},
		{get, "/api/mail/messages/{id}/attachments/{part}", "mail.view", ScopeGlobal, s.handleMailAttachment, false},
		{get, "/api/admin/audit", "audit.view", ScopeGlobal, s.handleAudit, false},

		{get, "/api/admin/agents", "admin.agents", ScopeGlobal, s.handleListAgents, false},
		{post, "/api/admin/agents", "admin.agents", ScopeGlobal, s.handleCreateAgent, false},
		{post, "/api/admin/agents/{id}/join-token", "admin.agents", ScopeGlobal, s.handleAgentJoinToken, false},
		{post, "/api/admin/agents/{id}/revoke", "admin.agents", ScopeGlobal, s.handleRevokeAgent, false},
		{post, "/api/admin/agents/{id}/ack-alert", "admin.agents", ScopeGlobal, s.handleAckAgentAlert, false},

		// Agents authenticate with their own credential (bearer token / one-time join token),
		// not a user session, so these two are the only Public routes besides login and probes.
		{get, "/agent/ws", Public, ScopeNone, s.handleAgentWS, false},
		{post, "/agent/join", Public, ScopeNone, s.handleAgentJoin, true},
	}
}
