// Package gh is a small GitHub REST client for Helmsman's workflow runs: App
// authentication, workflow dispatch, run and job polling. Only the endpoints the
// portal needs are implemented, and nothing here runs a free-form command.
package gh

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TokenSource hands out an API token for a repository ("owner/name").
type TokenSource interface {
	Token(ctx context.Context, repo string) (string, error)
	// Invalidate drops a cached token after GitHub rejected it.
	Invalidate(repo string)
}

// ErrNoInstallation means the GitHub App is not installed on the repository.
var ErrNoInstallation = errors.New("the GitHub App is not installed on this repository")

// Refresh installation tokens at 50 of their 60 minutes.
const tokenRefreshAfter = 50 * time.Minute

// AppAuth signs a short-lived JWT as the GitHub App and exchanges it for installation
// tokens, which it caches per installation.
type AppAuth struct {
	AppID   string
	Key     *rsa.PrivateKey
	APIURL  string
	HTTP    *http.Client
	Now     func() time.Time
	mu      sync.Mutex
	install map[string]int64 // owner/name -> installation id
	tokens  map[int64]cachedToken
}

type cachedToken struct {
	token   string
	fetched time.Time
	expires time.Time
}

// ParseKey reads an RSA private key in PKCS#1 or PKCS#8 PEM form.
func ParseKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	blk, _ := pem.Decode(pemBytes)
	if blk == nil {
		return nil, errors.New("private key is not PEM encoded")
	}
	if k, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return rk, nil
}

func (a *AppAuth) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *AppAuth) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// JWT returns an App JWT (RS256, valid 9 minutes, backdated 60 s for clock skew).
func (a *AppAuth) JWT() (string, error) {
	now := a.now()
	hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iat": now.Add(-60 * time.Second).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": a.AppID})
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(hdr) + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.Key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

func (a *AppAuth) appRequest(ctx context.Context, method, path string, out any) (int, error) {
	jwt, err := a.JWT()
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, a.APIURL+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := a.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return res.StatusCode, fromResponse(res)
	}
	if out != nil {
		return res.StatusCode, json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode, nil
}

// Token returns an installation token for the repository, fetching or refreshing it as needed.
func (a *AppAuth) Token(ctx context.Context, repo string) (string, error) {
	a.mu.Lock()
	id, ok := a.install[repo]
	a.mu.Unlock()
	if !ok {
		var inst struct {
			ID int64 `json:"id"`
		}
		code, err := a.appRequest(ctx, http.MethodGet, "/repos/"+repo+"/installation", &inst)
		if code == http.StatusNotFound {
			return "", ErrNoInstallation
		}
		if err != nil {
			return "", err
		}
		id = inst.ID
		a.mu.Lock()
		if a.install == nil {
			a.install = map[string]int64{}
		}
		a.install[repo] = id
		a.mu.Unlock()
	}
	a.mu.Lock()
	t, ok := a.tokens[id]
	a.mu.Unlock()
	if ok && a.now().Sub(t.fetched) < tokenRefreshAfter && a.now().Before(t.expires) {
		return t.token, nil
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	code, err := a.appRequest(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", id), &out)
	if code == http.StatusNotFound {
		a.Invalidate(repo)
		return "", ErrNoInstallation
	}
	if err != nil {
		return "", err
	}
	nt := cachedToken{token: out.Token, fetched: a.now(), expires: out.ExpiresAt}
	if nt.expires.IsZero() {
		nt.expires = nt.fetched.Add(time.Hour)
	}
	a.mu.Lock()
	if a.tokens == nil {
		a.tokens = map[int64]cachedToken{}
	}
	a.tokens[id] = nt
	a.mu.Unlock()
	return out.Token, nil
}

// Invalidate forgets the cached token and installation for a repository.
func (a *AppAuth) Invalidate(repo string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if id, ok := a.install[repo]; ok {
		delete(a.tokens, id)
	}
	delete(a.install, repo)
}

// StaticToken is a TokenSource for tests and for a personal access token fallback.
type StaticToken string

// Token implements TokenSource.
func (s StaticToken) Token(context.Context, string) (string, error) { return string(s), nil }

// Invalidate implements TokenSource.
func (StaticToken) Invalidate(string) {}

func splitRepo(repo string) (string, string, bool) {
	o, n, ok := strings.Cut(repo, "/")
	return o, n, ok && o != "" && n != "" && !strings.Contains(n, "/")
}
