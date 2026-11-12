package api_test

import (
	"strings"
	"testing"
)

type pendingResp []struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Orgs  []struct {
		Org string `json:"org"`
	} `json:"orgs"`
}

func (p pendingResp) has(login string) bool {
	for _, u := range p {
		if u.Login == login {
			return true
		}
	}
	return false
}

func TestSomeoneWithoutAccessIsListedForAnAdminAndDropsOffWhenGranted(t *testing.T) {
	e := newEnv(t)
	alice, bob, erin := e.client(), e.client(), e.client()
	alice.login("alice")
	bob.login("bob") // signs in, has no role

	var me struct {
		HasAccess bool `json:"has_access"`
	}
	if code := bob.json("GET", "/api/me", nil, &me); code != 200 || me.HasAccess {
		t.Fatalf("someone with no role has no access yet: %d %+v", code, me)
	}
	if code := alice.json("GET", "/api/me", nil, &me); code != 200 || !me.HasAccess {
		t.Fatalf("an admin has access: %d %+v", code, me)
	}

	var list pendingResp
	if code := alice.json("GET", "/api/admin/access-requests", nil, &list); code != 200 || !list.has("bob") || list.has("alice") {
		t.Fatalf("bob is waiting, the admin is not: %d %+v", code, list)
	}
	if code := bob.json("GET", "/api/admin/access-requests", nil, nil); code != 403 {
		t.Fatalf("only an admin can see requests: %d", code)
	}

	// Granting is the ordinary binding call, and the request goes away.
	var roles []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	alice.json("GET", "/api/admin/roles", nil, &roles)
	var viewer int64
	for _, r := range roles {
		if r.Name == "viewer" {
			viewer = r.ID
		}
	}
	if code := alice.json("POST", "/api/admin/bindings", map[string]any{"subject_type": "user", "subject": "bob", "role_id": viewer, "app_id": "billing-api", "env": "*"}, nil); code != 201 && code != 200 {
		t.Fatalf("grant: %d", code)
	}
	if code := bob.json("GET", "/api/me", nil, &me); code != 200 || !me.HasAccess {
		t.Fatalf("after the grant bob has access: %d %+v", code, me)
	}
	if code := alice.json("GET", "/api/admin/access-requests", nil, &list); code != 200 || list.has("bob") {
		t.Fatalf("a granted request is no longer listed: %+v", list)
	}

	// Dismiss hides a request until the person signs in again.
	erin.login("erin")
	alice.json("GET", "/api/admin/access-requests", nil, &list)
	var erinID int64
	for _, u := range list {
		if u.Login == "erin" {
			erinID = u.ID
		}
	}
	if erinID == 0 {
		t.Fatalf("erin is waiting: %+v", list)
	}
	if code := bob.json("POST", "/api/admin/access-requests/"+itoa(erinID)+"/dismiss", nil, nil); code != 403 {
		t.Fatalf("dismiss is admin only: %d", code)
	}
	if code := alice.json("POST", "/api/admin/access-requests/"+itoa(erinID)+"/dismiss", nil, nil); code != 204 {
		t.Fatalf("dismiss: %d", code)
	}
	if code := alice.json("POST", "/api/admin/access-requests/999999/dismiss", nil, nil); code != 404 {
		t.Fatalf("unknown request: %d", code)
	}
	alice.json("GET", "/api/admin/access-requests", nil, &list)
	if list.has("erin") {
		t.Fatalf("dismissed: %+v", list)
	}
	erin2 := e.client()
	erin2.login("erin")
	alice.json("GET", "/api/admin/access-requests", nil, &list)
	if !list.has("erin") {
		t.Fatalf("signing in again brings the request back: %+v", list)
	}
}

func TestSwitchAccountAsksGitHubForItsAccountPicker(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	loc := func(path string) string {
		resp := c.do("GET", e.idp.URL+path, nil)
		return resp.Header.Get("Location")
	}
	if l := loc("/auth/github/login"); strings.Contains(l, "prompt=") {
		t.Fatalf("a normal sign-in does not force the picker: %s", l)
	}
	if l := loc("/auth/github/login?switch=1"); !strings.Contains(l, "prompt=select_account") {
		t.Fatalf("switching account asks for the picker: %s", l)
	}
}
