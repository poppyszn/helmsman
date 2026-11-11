package config

import (
	"strings"
	"testing"
)

func lookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

var key = strings.Repeat("ab", 32)

func valid() map[string]string {
	return map[string]string{
		"IDP_DATABASE_URL": "postgres://x", "IDP_BASE_URL": "https://idp.example.com/",
		"IDP_SESSION_KEY": key, "IDP_ENCRYPTION_KEY": key, "IDP_ALLOWED_ORGS": "a, b",
		"IDP_BOOTSTRAP_ADMIN_GITHUB_IDS": "1, 2",
	}
}

func TestDefaultsAndParsing(t *testing.T) {
	c, err := FromLookup(lookup(valid()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8080" || c.DisplayTZ != "Africa/Lagos" || c.GitHubMode != "real" {
		t.Fatalf("bad defaults: %+v", c)
	}
	if len(c.AllowedOrgs) != 2 || len(c.BootstrapIDs) != 2 || c.BaseURL != "https://idp.example.com" {
		t.Fatalf("bad parse: %+v", c)
	}
	if !c.SecureCookies() {
		t.Fatal("https must set Secure")
	}
}

func TestRequired(t *testing.T) {
	if _, err := FromLookup(lookup(nil)); err == nil {
		t.Fatal("expected errors")
	}
	m := valid()
	m["IDP_SESSION_KEY"] = "short"
	if _, err := FromLookup(lookup(m)); err == nil {
		t.Fatal("expected key error")
	}
	m = valid()
	m["IDP_GITHUB_MODE"] = "nope"
	if _, err := FromLookup(lookup(m)); err == nil {
		t.Fatal("expected mode error")
	}
}

func TestPlainHTTPDevCookies(t *testing.T) {
	m := valid()
	m["IDP_BASE_URL"] = "http://localhost:5173"
	c, _ := FromLookup(lookup(m))
	if c.SecureCookies() {
		t.Fatal("http dev must not set Secure")
	}
}
