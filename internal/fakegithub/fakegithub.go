// Package fakegithub is an in-memory stand-in for the parts of GitHub that Helmsman
// uses: OAuth login, the user, org and team APIs, and the GitHub App and Actions APIs (installations, workflow dispatch, runs, jobs, logs). It is scriptable at runtime
// through /_fake endpoints and is used by dev compose and by tests.
package fakegithub

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// User is a fake GitHub account.
type User struct {
	ID    int64    `json:"id"`
	Login string   `json:"login"`
	Name  string   `json:"name"`
	Email *string  `json:"email"`
	Orgs  []string `json:"orgs"`
	// Teams are "org/slug".
	Teams []string `json:"teams"`
	// Restricted lists orgs that block third-party OAuth apps: the user belongs
	// to them, but they are hidden from /user/orgs and /user/teams.
	Restricted []string `json:"restricted"`
}

type codeInfo struct {
	login     string
	challenge string
}

// Server is the fake GitHub.
type Server struct {
	mu     sync.Mutex
	users  map[string]*User
	codes  map[string]codeInfo
	tokens map[string]string // token -> login
	down   bool
	act    *actionsState
	mux    *http.ServeMux
}

// DefaultUsers returns the dev and test cast.
func DefaultUsers() []User {
	email := "alice@example.com"
	return []User{
		{ID: 1001, Login: "alice", Name: "Alice Admin", Email: &email, Orgs: []string{"acme-org"}, Teams: []string{"acme-org/platform"}},
		{ID: 1002, Login: "bob", Name: "Bob Developer", Orgs: []string{"acme-org"}, Teams: []string{"acme-org/backend"}},
		{ID: 1003, Login: "carol", Name: "Carol Viewer", Orgs: []string{"acme-org"}},
		{ID: 1004, Login: "dave", Name: "Dave Outsider", Orgs: []string{"other-org"}},
		{ID: 1005, Login: "erin", Name: "Erin Approver", Orgs: []string{"acme-org"}, Teams: []string{"acme-org/leads"}},
		{ID: 1006, Login: "frank", Name: "Frank Restricted", Restricted: []string{"acme-org"}},
		// In the allowed org with no role bound: signs in and waits for an admin to grant access.
		{ID: 1007, Login: "grace", Name: "Grace Newcomer", Orgs: []string{"acme-org"}},
	}
}

// New builds a fake with the default users.
func New() *Server { return NewWithUsers(DefaultUsers()) }

// NewWithUsers builds a fake with the given users.
func NewWithUsers(us []User) *Server {
	s := &Server{users: map[string]*User{}, codes: map[string]codeInfo{}, tokens: map[string]string{}}
	for i := range us {
		u := us[i]
		s.users[u.Login] = &u
	}
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	m.HandleFunc("GET /login/oauth/authorize", s.authorize)
	m.HandleFunc("POST /login/oauth/access_token", s.accessToken)
	m.HandleFunc("GET /user", s.api(s.getUser))
	m.HandleFunc("GET /user/orgs", s.api(s.getOrgs))
	m.HandleFunc("GET /user/teams", s.api(s.getTeams))
	m.HandleFunc("GET /user/memberships/orgs/{org}", s.api(s.getMembership))
	m.HandleFunc("PUT /_fake/users/{login}", s.putUser)
	m.HandleFunc("DELETE /_fake/tokens/{login}", s.revokeTokens)
	m.HandleFunc("PUT /_fake/down", s.setDown)
	s.registerActions(m)
	s.mux = m
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	down := s.down && !strings.HasPrefix(r.URL.Path, "/_fake")
	s.mu.Unlock()
	if down {
		http.Error(w, "fake github is down", http.StatusServiceUnavailable)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func randHex() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// authorize shows a user picker, or finishes at once when ?fake_user= is given (tests).
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	if redirect == "" || q.Get("state") == "" {
		http.Error(w, "redirect_uri and state are required", http.StatusBadRequest)
		return
	}
	pick := q.Get("fake_user")
	if pick == "" {
		s.mu.Lock()
		logins := make([]string, 0, len(s.users))
		for l := range s.users {
			logins = append(logins, l)
		}
		s.mu.Unlock()
		sort.Strings(logins)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>Fake GitHub</title><h1>Fake GitHub: sign in as</h1><ul>")
		for _, l := range logins {
			qq := r.URL.Query()
			qq.Set("fake_user", l)
			fmt.Fprintf(w, `<li><a href="/login/oauth/authorize?%s">%s</a></li>`, html.EscapeString(qq.Encode()), html.EscapeString(l))
		}
		fmt.Fprint(w, "</ul>")
		return
	}
	s.mu.Lock()
	_, ok := s.users[pick]
	code := randHex()
	if ok {
		s.codes[code] = codeInfo{login: pick, challenge: q.Get("code_challenge")}
	}
	s.mu.Unlock()
	if !ok {
		http.Error(w, "unknown user", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(redirect)
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	qq := u.Query()
	qq.Set("code", code)
	qq.Set("state", q.Get("state"))
	u.RawQuery = qq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (s *Server) accessToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	s.mu.Lock()
	ci, ok := s.codes[r.PostForm.Get("code")]
	delete(s.codes, r.PostForm.Get("code"))
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if !ok {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_verification_code"})
		return
	}
	if ci.challenge != "" {
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != ci.challenge {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_verification_code", "error_description": "PKCE failed"})
			return
		}
	}
	tok := "fake_" + randHex()
	s.mu.Lock()
	s.tokens[tok] = ci.login
	s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]string{"access_token": tok, "token_type": "bearer", "scope": "read:user,read:org"})
}

func (s *Server) api(h func(w http.ResponseWriter, r *http.Request, u User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		tok = strings.TrimPrefix(tok, "token ")
		s.mu.Lock()
		login, ok := s.tokens[tok]
		var u User
		if ok {
			u = *s.users[login]
		}
		s.mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
			return
		}
		h(w, r, u)
	}
}

func hidden(u User, org string) bool {
	for _, r := range u.Restricted {
		if r == org {
			return true
		}
	}
	return false
}

func (s *Server) getUser(w http.ResponseWriter, _ *http.Request, u User) {
	writeJSON(w, 200, map[string]any{"id": u.ID, "login": u.Login, "name": u.Name, "email": u.Email,
		"avatar_url": "https://avatars.example.invalid/" + u.Login})
}

func (s *Server) getOrgs(w http.ResponseWriter, _ *http.Request, u User) {
	out := []map[string]any{}
	for _, o := range u.Orgs {
		if !hidden(u, o) {
			out = append(out, map[string]any{"login": o})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) getTeams(w http.ResponseWriter, _ *http.Request, u User) {
	out := []map[string]any{}
	for _, t := range u.Teams {
		org, slug, _ := strings.Cut(t, "/")
		if !hidden(u, org) {
			out = append(out, map[string]any{"slug": slug, "organization": map[string]any{"login": org}})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) getMembership(w http.ResponseWriter, r *http.Request, u User) {
	org := r.PathValue("org")
	if hidden(u, org) {
		writeJSON(w, 403, map[string]string{"message": "Although you appear to have the correct authorization credentials, the `" + org +
			"` organization has enabled OAuth App access restrictions, meaning that data access to third-parties is limited."})
		return
	}
	for _, o := range u.Orgs {
		if o == org {
			writeJSON(w, 200, map[string]string{"state": "active"})
			return
		}
	}
	writeJSON(w, 404, map[string]string{"message": "Not Found"})
}

func (s *Server) putUser(w http.ResponseWriter, r *http.Request) {
	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	u.Login = r.PathValue("login")
	s.mu.Lock()
	s.users[u.Login] = &u
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) revokeTokens(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	for t, l := range s.tokens {
		if l == r.PathValue("login") {
			delete(s.tokens, t)
		}
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setDown(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.down = r.URL.Query().Get("down") == "true"
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ReplaceUser sets a user directly (for tests). Keyed by login.
func (s *Server) ReplaceUser(u User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for l, old := range s.users {
		if old.ID == u.ID {
			delete(s.users, l)
		}
	}
	s.users[u.Login] = &u
	for t, l := range s.tokens {
		if _, ok := s.users[l]; !ok {
			s.tokens[t] = u.Login
		}
	}
}

// SetDown makes every non-/_fake request return 503.
func (s *Server) SetDown(v bool) {
	s.mu.Lock()
	s.down = v
	s.mu.Unlock()
}

// RevokeTokens invalidates all tokens of a user.
func (s *Server) RevokeTokens(login string) {
	s.mu.Lock()
	for t, l := range s.tokens {
		if l == login {
			delete(s.tokens, t)
		}
	}
	s.mu.Unlock()
}
