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
