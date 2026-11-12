package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/config"
	"github.com/poppyszn/helmsman/internal/store"
	"golang.org/x/oauth2"
)

const (
	// SessionCookie holds the opaque session id. Only its SHA-256 is stored.
	SessionCookie = "helmsman_session"
	stateCookie   = "helmsman_oauth"
	// SessionTTL is the session lifetime.
	SessionTTL = 12 * time.Hour
)

// Service implements login, logout and membership checks.
type Service struct {
	bg    breakglassState
	Cfg   config.Config
	Store *store.Store
	Audit *audit.Writer
	GH    *GitHubClient
	oauth oauth2.Config
}

// NewService wires the OAuth config from Cfg.
func NewService(cfg config.Config, st *store.Store, a *audit.Writer) *Service {
	return &Service{
		Cfg: cfg, Store: st, Audit: a,
		GH: &GitHubClient{APIURL: cfg.GitHubAPIURL},
		oauth: oauth2.Config{
			ClientID: cfg.OAuthClientID, ClientSecret: cfg.OAuthClientSecret,
			Endpoint:    oauth2.Endpoint{AuthURL: cfg.GitHubAuthURL, TokenURL: cfg.GitHubTokenURL},
			RedirectURL: cfg.BaseURL + "/auth/github/callback",
			Scopes:      []string{"read:user", "read:org"},
		},
	}
}

type oauthState struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
	Exp      int64  `json:"e"`
}

// HandleLogin starts the flow: state + PKCE verifier go into a short-lived signed cookie.
func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	st := oauthState{State: RandomToken(24), Verifier: oauth2.GenerateVerifier(), Exp: time.Now().Add(10 * time.Minute).Unix()}
	raw, _ := json.Marshal(st)
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: Sign(s.Cfg.SessionKey, string(b64(raw))), Path: "/auth/github",
		MaxAge: 600, HttpOnly: true, Secure: s.Cfg.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(st.Verifier)}
	// "Switch account" asks GitHub to show its account picker instead of reusing the account that is signed in.
	if r.URL.Query().Get("switch") == "1" {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "select_account"))
	}
	http.Redirect(w, r, s.oauth.AuthCodeURL(st.State, opts...), http.StatusFound)
}

// HandleCallback finishes login. Failures redirect to /login?error=<code> with a code the UI maps to text.
func (s *Service) HandleCallback(w http.ResponseWriter, r *http.Request) {
	ip := ClientIP(r)
	fail := func(code, org string, detail map[string]any) {
		if detail == nil {
			detail = map[string]any{}
		}
		detail["code"] = code
		_ = s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorSystem, ActorID: "login", Action: "auth.login",
			Result: "denied", Details: detail, IP: ip})
		q := url.Values{"error": {code}}
		if org != "" {
			q.Set("org", org)
		}
		http.Redirect(w, r, "/login?"+q.Encode(), http.StatusFound)
	}
	clearState := func() {
		http.SetCookie(w, &http.Cookie{Name: stateCookie, Path: "/auth/github", MaxAge: -1})
	}
	defer clearState()

	c, err := r.Cookie(stateCookie)
	if err != nil {
		fail("state", "", nil)
		return
	}
	val, ok := Verify(s.Cfg.SessionKey, c.Value)
	raw, derr := unb64(val)
	var st oauthState
	if !ok || derr != nil || json.Unmarshal(raw, &st) != nil || time.Now().Unix() > st.Exp ||
		r.URL.Query().Get("state") != st.State || st.State == "" {
		fail("state", "", nil)
		return
	}
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		fail("denied", "", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	tok, err := s.oauth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		fail("github_unavailable", "", map[string]any{"stage": "exchange"})
		return
	}
	user, err := s.GH.User(ctx, tok.AccessToken)
	if err != nil {
		fail("github_unavailable", "", map[string]any{"stage": "user"})
		return
	}
	mem, err := s.GH.Membership(ctx, tok.AccessToken)
	if err != nil {
		fail("github_unavailable", "", map[string]any{"stage": "membership"})
		return
	}
	orgTeams := s.allowedOrgTeams(mem)
	if len(orgTeams) == 0 {
		for _, org := range s.Cfg.AllowedOrgs {
			if s.GH.OrgRestricted(ctx, tok.AccessToken, org) {
				fail("org_restricted", org, map[string]any{"login": user.Login})
				return
			}
		}
		fail("not_in_org", "", map[string]any{"login": user.Login})
		return
	}

	enc, err := Encrypt(s.Cfg.EncryptionKey, []byte(tok.AccessToken))
	if err != nil {
		fail("internal", "", nil)
		return
	}
	info := store.LoginInfo{GitHubID: user.ID, Login: user.Login, AvatarURL: user.AvatarURL}
	if user.Name != nil {
		info.Name = *user.Name
	}
	if user.Email != nil {
		info.Email = *user.Email
	}
	u, err := s.Store.UpsertLogin(ctx, info, enc, slices.Contains(s.Cfg.BootstrapIDs, user.ID))
	if err != nil {
		fail("internal", "", nil)
		return
	}
	if u.DisabledAt != nil {
		fail("disabled", "", map[string]any{"login": u.Login})
		return
	}
	if err := s.Store.ReplaceUserOrgs(ctx, u.ID, orgTeams); err != nil {
		fail("internal", "", nil)
		return
	}
	sid := RandomToken(32)
	if err := s.Store.CreateSession(ctx, HashToken(sid), u.ID, SessionTTL, ip, r.UserAgent()); err != nil {
		fail("internal", "", nil)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: sid, Path: "/", MaxAge: int(SessionTTL.Seconds()),
		HttpOnly: true, Secure: s.Cfg.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
	_ = s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: u.Login, Action: "auth.login",
		Target: u.Login, Result: "ok", IP: ip})
	http.Redirect(w, r, "/", http.StatusFound)
}

// allowedOrgTeams keeps only allowed orgs and the teams inside them.
func (s *Service) allowedOrgTeams(m Membership) map[string][]string {
	out := map[string][]string{}
	for _, o := range m.Orgs {
		if slices.ContainsFunc(s.Cfg.AllowedOrgs, func(a string) bool { return strings.EqualFold(a, o) }) {
			out[o] = []string{}
		}
	}
	for _, t := range m.Teams {
		org, _, _ := strings.Cut(t, "/")
		if _, ok := out[org]; ok {
			out[org] = append(out[org], t)
		}
	}
	return out
}

// Logout revokes the session behind the request cookie.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request, sessionHash []byte, login string) {
	_ = s.Store.RevokeSession(r.Context(), sessionHash)
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Cfg.SecureCookies(), SameSite: http.SameSiteLaxMode})
	_ = s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: login, Action: "auth.logout", Result: "ok", IP: ClientIP(r)})
}

// RefreshAll re-checks org membership for every user with a live session. Users no
// longer in an allowed org (or whose token GitHub revoked) lose all sessions. A GitHub
// outage never revokes anything: existing sessions keep working.
func (s *Service) RefreshAll(ctx context.Context) {
	targets, err := s.Store.RefreshTargets(ctx)
	if err != nil {
		return
	}
	for _, t := range targets {
		tok, err := Decrypt(s.Cfg.EncryptionKey, t.TokenEnc)
		if err != nil {
			continue
		}
		mem, err := s.GH.Membership(ctx, string(tok))
		switch {
		case errors.Is(err, ErrTokenInvalid):
			s.revoke(ctx, t, "token_invalid")
		case err != nil:
			continue // GitHub down or other transient error: leave sessions alone
		default:
			orgTeams := s.allowedOrgTeams(mem)
			if len(orgTeams) == 0 {
				s.revoke(ctx, t, "left_allowed_orgs")
				continue
			}
			_ = s.Store.ReplaceUserOrgs(ctx, t.UserID, orgTeams)
		}
	}
}

func (s *Service) revoke(ctx context.Context, t store.RefreshTarget, why string) {
	n, _ := s.Store.RevokeUserSessions(ctx, t.UserID)
	_ = s.Audit.Write(ctx, audit.Event{ActorType: audit.ActorSystem, ActorID: "membership-refresh", Action: "auth.sessions_revoked",
		Target: t.Login, Result: "ok", Details: map[string]any{"reason": why, "sessions": n}})
}

// RunRefresh runs RefreshAll every interval until ctx ends.
func (s *Service) RunRefresh(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.RefreshAll(ctx)
		}
	}
}

// ClientIP returns the peer IP (chi's RealIP middleware has already applied proxy headers).
func ClientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}
