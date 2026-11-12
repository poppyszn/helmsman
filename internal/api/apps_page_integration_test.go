package api_test

import (
	"context"
	"strings"
	"testing"
)

func TestAppsCarryTheirOrganizationAndTags(t *testing.T) {
	e := newEnv(t)
	alice := e.client()
	alice.login("alice")
	tagged := strings.Replace(catalogYAML, "    repo: acme-org/billing-api\n", "    repo: acme-org/billing-api\n    tags: [api, payments]\n", 1)
	tagged = strings.Replace(tagged, "        github_environment: production\n", "        github_environment: production\n        tags: [critical]\n", 1)
	e.writeCatalog(tagged)
	if err := e.cat.Reload(context.Background(), "test", true); err != nil {
		t.Fatal(err)
	}
	var apps []struct {
		ID   string   `json:"id"`
		Org  string   `json:"org"`
		Tags []string `json:"tags"`
	}
	if code := alice.json("GET", "/api/apps", nil, &apps); code != 200 {
		t.Fatalf("apps: %d", code)
	}
	for _, a := range apps {
		if a.ID == "billing-api" {
			if a.Org != "acme-org" || strings.Join(a.Tags, ",") != "api,payments,critical" {
				t.Fatalf("org and tags (the app's, then the environment's): %+v", a)
			}
		}
		if a.Tags == nil {
			t.Fatalf("tags is always a list: %+v", a)
		}
	}
}

func TestCustomAppsAreVisibleToEveryoneWhoSeesAppsAndManagedByAdmins(t *testing.T) {
	e := newEnv(t)
	alice, bob, carol := e.client(), e.client(), e.client()
	alice.login("alice")
	bob.login("bob")
	carol.login("carol") // signed in, no role
	e.bind("user", itoa(e.userID("bob")), "viewer", "billing-api", "*")

	for _, c := range []struct{ m, p string }{{"POST", "/api/admin/custom-apps"}, {"PUT", "/api/admin/custom-apps/1"}, {"DELETE", "/api/admin/custom-apps/1"}} {
		if code := bob.json(c.m, c.p, map[string]any{"name": "x", "url": "https://x.example.com"}, nil); code != 403 {
			t.Errorf("%s %s: %d", c.m, c.p, code)
		}
	}
	for name, body := range map[string]map[string]any{
		"no name":     {"name": "", "url": "https://grafana.example.com"},
		"no scheme":   {"name": "Grafana", "url": "grafana.example.com"},
		"javascript":  {"name": "Grafana", "url": "javascript:alert(1)"},
		"credentials": {"name": "Grafana", "url": "https://user:pw@grafana.example.com"},
		"bad tag":     {"name": "Grafana", "url": "https://grafana.example.com", "tags": []string{"Not A Tag"}},
		"long":        {"name": "Grafana", "url": "https://grafana.example.com", "description": strings.Repeat("x", 201)},
	} {
		if code := alice.json("POST", "/api/admin/custom-apps", body, nil); code != 422 {
			t.Errorf("%s must be refused: %d", name, code)
		}
	}
	var made struct {
		ID int64 `json:"id"`
	}
	if code := alice.json("POST", "/api/admin/custom-apps", map[string]any{"name": "Grafana", "url": "https://grafana.example.com/", "description": "Dashboards", "tags": []string{"Monitoring", "prod"}}, &made); code != 201 || made.ID == 0 {
		t.Fatalf("create: %d", code)
	}
	if code := alice.json("POST", "/api/admin/custom-apps", map[string]any{"name": "GRAFANA", "url": "https://other.example.com"}, nil); code != 422 {
		t.Fatalf("names are unique ignoring case: %d", code)
	}
	var list []struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if code := bob.json("GET", "/api/custom-apps", nil, &list); code != 200 || len(list) != 1 || list[0].Name != "Grafana" || strings.Join(list[0].Tags, ",") != "monitoring,prod" {
		t.Fatalf("someone who sees apps sees the tools, tags lowercased: %d %+v", code, list)
	}
	if code := carol.json("GET", "/api/custom-apps", nil, nil); code != 403 {
		t.Fatalf("someone with no access sees nothing: %d", code)
	}
	if code := alice.json("PUT", "/api/admin/custom-apps/"+itoa(made.ID), map[string]any{"name": "Grafana", "url": "https://grafana.example.com/d/1", "tags": []string{}}, nil); code != 200 {
		t.Fatalf("update: %d", code)
	}
	if code := alice.json("PUT", "/api/admin/custom-apps/9999", map[string]any{"name": "Nope", "url": "https://x.example.com"}, nil); code != 404 {
		t.Fatalf("update unknown: %d", code)
	}
	if code := alice.json("DELETE", "/api/admin/custom-apps/"+itoa(made.ID), nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code := bob.json("GET", "/api/custom-apps", nil, &list); code != 200 || len(list) != 0 {
		t.Fatalf("after delete: %d %+v", code, list)
	}
}
