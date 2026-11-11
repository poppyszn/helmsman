package catalog

import (
	"strings"
	"testing"
)

const good = `
version: 1
apps:
  - id: billing-api
    name: Billing API
    repo: acme-org/billing-api
    environments:
      - env: staging
        tier: nonprod
        agent: staging-app-01
        container: { compose_project: billing, service: api }
        url: https://staging-billing.example.com
        health: { url: https://staging-billing.example.com/health, via: server }
        workflows: { deploy: deploy.yml, migrate: migrate.yml }
        github_environment: staging
      - env: prod
        tier: prod
        agent: prod-app-01
        container: { compose_project: billing, service: api }
        url: https://billing.example.com
        health: { url: http://localhost:8080/health, via: agent }
        workflows: { deploy: deploy.yml, migrate: migrate.yml }
        github_environment: production
`

func known(ids ...string) func(string) bool {
	return func(id string) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
}

func TestValidCatalog(t *testing.T) {
	c, err := Parse([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if errs := Validate(c, known("staging-app-01", "prod-app-01")); len(errs) != 0 {
		t.Fatal(JoinErrors(errs))
	}
	if !c.Apps[0].Environments[0].SeedAllowed() || c.Apps[0].Environments[1].SeedAllowed() {
		t.Fatal("allow_seed defaults: nonprod true, prod false")
	}
}

func TestBadYAML(t *testing.T) {
	if _, err := Parse([]byte("version: [")); err == nil {
		t.Fatal("expected yaml error")
	}
	if _, err := Parse([]byte("version: 1\napps: []\nbogus: 1\n")); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
}

func TestProblems(t *testing.T) {
	c, _ := Parse([]byte(good))
	dup := c.Apps[0]
	c.Apps = append(c.Apps, dup)
	c.Apps[0].Environments[0].Tier = "weird"
	errs := Validate(c, known("staging-app-01"))
	msg := JoinErrors(errs)
	for _, want := range []string{"duplicate app id", "tier must be", `unknown agent "prod-app-01"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
}

func TestLinks(t *testing.T) {
	ok := strings.Replace(good, "    repo: acme-org/billing-api\n", "    repo: acme-org/billing-api\n    links:\n      - { name: Runbook, url: https://wiki.example.com/billing }\n", 1)
	c, err := Parse([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if errs := Validate(c, known("staging-app-01", "prod-app-01")); len(errs) != 0 {
		t.Fatalf("valid links rejected: %v", JoinErrors(errs))
	}
	for name, links := range map[string]string{
		"no name":   "{ name: '', url: https://x.example.com }",
		"bad url":   "{ name: Docs, url: 'javascript:alert(1)' }",
		"no scheme": "{ name: Docs, url: wiki.example.com }",
	} {
		bad := strings.Replace(good, "    repo: acme-org/billing-api\n", "    repo: acme-org/billing-api\n    links:\n      - "+links+"\n", 1)
		c, err := Parse([]byte(bad))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if errs := Validate(c, known("staging-app-01", "prod-app-01")); len(errs) == 0 {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}
