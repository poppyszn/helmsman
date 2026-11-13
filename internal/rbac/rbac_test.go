package rbac

import "testing"

var (
	stg   = Scope{AppID: "billing", Env: "staging", Tier: TierNonprod}
	prod  = Scope{AppID: "billing", Env: "prod", Tier: TierProd}
	other = Scope{AppID: "ledger", Env: "staging", Tier: TierNonprod}
)

func dev(app, env string) Principal {
	return Principal{Bindings: []Binding{{Permissions: []string{AppView, LogsView, ActionRestart}, AppID: app, Env: env}}}
}

func TestMatrix(t *testing.T) {
	cases := []struct {
		name string
		p    Principal
		perm string
		s    Scope
		want bool
	}{
		{"exact app+env", dev("billing", "staging"), LogsView, stg, true},
		{"wildcard env matches nonprod", dev("billing", "*"), LogsView, stg, true},
		{"wildcard env never matches prod", dev("billing", "*"), LogsView, prod, false},
		{"wildcard app+env never matches prod", dev("*", "*"), LogsView, prod, false},
		{"explicit prod env matches prod", dev("billing", "prod"), LogsView, prod, true},
		{"** matches prod", dev("*", WildcardAllEnvs), LogsView, prod, true},
		{"** matches nonprod", dev("billing", WildcardAllEnvs), LogsView, stg, true},
		{"** still scoped to its app", dev("billing", WildcardAllEnvs), LogsView, other, false},
		{"** never seeds prod", Principal{Bindings: []Binding{{Permissions: []string{ActionSeed}, AppID: "*", Env: WildcardAllEnvs}}}, ActionSeed, prod, false},
		{"other app denied", dev("billing", "*"), LogsView, other, false},
		{"wildcard app matches other app nonprod", dev("*", "*"), LogsView, other, true},
		{"perm not in role", dev("billing", "*"), ActionMigrate, stg, false},
		{"no bindings", Principal{}, AppView, stg, false},
		{"site admin everywhere incl prod", Principal{IsAdmin: true}, ActionRestart, prod, true},
		{"a developer can migrate prod", Principal{Bindings: []Binding{{Permissions: []string{ActionMigrate, ActionSeed}, AppID: "billing", Env: "prod"}}}, ActionMigrate, prod, true},
		{"a developer can never seed prod, even with the permission bound", Principal{Bindings: []Binding{{Permissions: []string{ActionMigrate, ActionSeed}, AppID: "billing", Env: "prod"}}}, ActionSeed, prod, false},
		{"not by wildcard either", Principal{Bindings: []Binding{{Permissions: []string{ActionSeed}, AppID: "*", Env: "*"}}}, ActionSeed, prod, false},
		{"seeding nonprod is unchanged", Principal{Bindings: []Binding{{Permissions: []string{ActionSeed}, AppID: "billing", Env: "*"}}}, ActionSeed, stg, true},
		{"a site admin can seed prod (allow_seed still decides)", Principal{IsAdmin: true}, ActionSeed, prod, true},
		{"union of bindings", Principal{Bindings: []Binding{
			{Permissions: []string{AppView}, AppID: "billing", Env: "staging"},
			{Permissions: []string{LogsView}, AppID: "billing", Env: "staging"}}}, LogsView, stg, true},
	}
	for _, c := range cases {
		if got := c.p.Allowed(c.perm, c.s); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestAnyEnvAndGlobal(t *testing.T) {
	p := dev("billing", "*")
	if !p.AllowedAnyEnv(AppView, "billing") || p.AllowedAnyEnv(AppView, "ledger") || !p.AllowedAnyEnv(AppView, Wildcard) {
		t.Fatal("any-env scoping wrong")
	}
	if p.AllowedGlobal(AppView) {
		t.Fatal("app-scoped binding must not be global")
	}
	g := Principal{Bindings: []Binding{{Permissions: []string{AdminAccess}, AppID: "*", Env: "*"}}}
	if !g.AllowedGlobal(AdminAccess) || g.AllowedGlobal(AuditView) {
		t.Fatal("global scoping wrong")
	}
}

func TestSelfApprovalAndSeed(t *testing.T) {
	approver := Principal{Bindings: []Binding{{Permissions: []string{ActionApprove}, AppID: "billing", Env: "prod"}}}
	if approver.CanApprove(prod, 7, 7) {
		t.Fatal("self approval must be refused")
	}
	if !approver.CanApprove(prod, 7, 8) {
		t.Fatal("second person may approve")
	}
	if SeedAllowed(false) || !SeedAllowed(true) {
		t.Fatal("seed gate wrong")
	}
	admin := Principal{IsAdmin: true}
	if admin.CanApprove(prod, 1, 1) {
		t.Fatal("admins cannot self-approve either")
	}
}

func TestOrgLimit(t *testing.T) {
	p := Principal{
		Bindings: []Binding{{Permissions: []string{AppView}, AppID: Wildcard, Env: Wildcard}},
		Orgs:     map[string]bool{"acme-org": true},
		AppOrg:   map[string]string{"mine": "Acme-Org", "theirs": "other-org"},
	}
	stg := Scope{AppID: "mine", Env: "staging", Tier: TierNonprod}
	if !p.Allowed(AppView, stg) {
		t.Fatal("an app in the person's organization should be allowed")
	}
	if p.Allowed(AppView, Scope{AppID: "theirs", Env: "staging", Tier: TierNonprod}) {
		t.Fatal("an app in another organization must be refused, even with a wildcard binding")
	}
	if p.AllowedAnyEnv(AppView, "theirs") {
		t.Fatal("list gating must apply the organization limit too")
	}
	if !p.AllowedGlobal(AppView) {
		t.Fatal("global permissions are not limited by organization")
	}
	admin := p
	admin.IsAdmin = true
	if !admin.Allowed(AppView, Scope{AppID: "theirs", Env: "staging", Tier: TierNonprod}) {
		t.Fatal("site admins see every organization")
	}
}
