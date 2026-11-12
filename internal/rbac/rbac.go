// Package rbac decides what a user may do. GitHub says who they are; bindings in
// the database say what they can do.
package rbac

// Permissions.
const (
	AppView       = "app.view"
	DeployView    = "deploy.view"
	LogsView      = "logs.view"
	ActionRestart = "action.restart"
	ActionMigrate = "action.migrate"
	ActionSeed    = "action.seed"
	ActionApprove = "action.approve"
	AdminAccess   = "admin.access"
	AdminAgents   = "admin.agents"
	AuditView     = "audit.view"
)

// AllPermissions lists every known permission.
var AllPermissions = []string{AppView, DeployView, LogsView, ActionRestart, ActionMigrate,
	ActionSeed, ActionApprove, AdminAccess, AdminAgents, AuditView}

// Tiers.
const (
	TierNonprod = "nonprod"
	TierProd    = "prod"
)

// Wildcard matches any app or env, subject to the prod rule below.
const Wildcard = "*"

// Binding is a role binding already resolved to the role's permissions.
type Binding struct {
	Permissions []string
	AppID       string // app slug or "*"
	Env         string // env name or "*"
}

// Principal is the subject of a check: a user plus all bindings that apply to
// them directly or through their org/team memberships.
type Principal struct {
	IsAdmin  bool
	Bindings []Binding
}

// Scope identifies the target of an action.
type Scope struct {
	AppID string
	Env   string
	Tier  string
}

func has(perms []string, p string) bool {
	for _, x := range perms {
		if x == p {
			return true
		}
	}
	return false
}

// Allowed reports whether the principal holds perm for the exact app and env.
// Rules: any matching binding is enough (union); an env of "*" matches only
// nonprod tiers, so prod must be granted by explicit env name; action.seed is
// never granted on prod to anyone but a site admin. Site admins
// (users.is_admin) hold every permission everywhere, including prod.
func (p Principal) Allowed(perm string, s Scope) bool {
	if p.IsAdmin {
		return true
	}
	// Seeding production is never granted by a role or a binding, whatever it says: only a site admin can,
	// and even then only where the environment's allow_seed lets it (checked by the caller).
	if perm == ActionSeed && s.Tier == TierProd {
		return false
	}
	for _, b := range p.Bindings {
		if !has(b.Permissions, perm) {
			continue
		}
		if b.AppID != Wildcard && b.AppID != s.AppID {
			continue
		}
		if b.Env == s.Env || (b.Env == Wildcard && s.Tier == TierNonprod) {
			return true
		}
	}
	return false
}

// AllowedAnyEnv reports whether perm could apply to at least one env of the app
// ("*" means every app). It is used to gate list endpoints before filtering.
// A wildcard env binding counts here even though it will not match prod.
func (p Principal) AllowedAnyEnv(perm, appID string) bool {
	if p.IsAdmin {
		return true
	}
	for _, b := range p.Bindings {
		if has(b.Permissions, perm) && (b.AppID == Wildcard || appID == Wildcard || b.AppID == appID) {
			return true
		}
	}
	return false
}

// AllowedGlobal checks a permission that is not about one app (admin pages,
// audit log). It requires a binding on every app and every env.
func (p Principal) AllowedGlobal(perm string) bool {
	if p.IsAdmin {
		return true
	}
	for _, b := range p.Bindings {
		if has(b.Permissions, perm) && b.AppID == Wildcard && b.Env == Wildcard {
			return true
		}
	}
	return false
}

// CanApprove applies the approval rules on top of the permission: a requester can
// never approve their own request.
func (p Principal) CanApprove(s Scope, requesterID, approverID int64) bool {
	return requesterID != approverID && p.Allowed(ActionApprove, s)
}

// SeedAllowed enforces that seeds are refused when the env has allow_seed false,
// even for admins.
func SeedAllowed(allowSeed bool) bool { return allowSeed }
