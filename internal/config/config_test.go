package config

import "testing"

func lookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestDefaultsAndRequired(t *testing.T) {
	if _, err := FromLookup(lookup(nil)); err == nil {
		t.Fatal("expected error for missing database url")
	}
	c, err := FromLookup(lookup(map[string]string{"IDP_DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8080" || c.DisplayTZ != "Africa/Lagos" || c.GitHubMode != "real" {
		t.Fatalf("bad defaults: %+v", c)
	}
}

func TestBadGitHubMode(t *testing.T) {
	_, err := FromLookup(lookup(map[string]string{"IDP_DATABASE_URL": "x", "IDP_GITHUB_MODE": "nope"}))
	if err == nil {
		t.Fatal("expected error")
	}
}
