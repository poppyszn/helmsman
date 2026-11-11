package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GitHubClient calls the GitHub REST API with a user token.
type GitHubClient struct {
	APIURL string
	HTTP   *http.Client
}

// ErrTokenInvalid means GitHub rejected the token (revoked or expired).
var ErrTokenInvalid = errors.New("github token invalid")

// ErrUnavailable means GitHub could not be reached or returned a server error.
var ErrUnavailable = errors.New("github unavailable")

// GHUser is the subset of GET /user we use.
type GHUser struct {
	ID        int64   `json:"id"`
	Login     string  `json:"login"`
	Name      *string `json:"name"`
	Email     *string `json:"email"`
	AvatarURL string  `json:"avatar_url"`
}

func (c *GitHubClient) get(ctx context.Context, token, path string, out any) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIURL+path, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return resp.StatusCode, "", ErrTokenInvalid
	case resp.StatusCode >= 500:
		return resp.StatusCode, "", fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode >= 300:
		var m struct{ Message string }
		_ = json.Unmarshal(body, &m)
		return resp.StatusCode, m.Message, nil
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return resp.StatusCode, "", err
		}
	}
	return resp.StatusCode, "", nil
}

// User returns the authenticated user.
func (c *GitHubClient) User(ctx context.Context, token string) (GHUser, error) {
	var u GHUser
	code, _, err := c.get(ctx, token, "/user", &u)
	if err == nil && code != 200 {
		err = fmt.Errorf("github /user returned %d", code)
	}
	return u, err
}

// Membership is the user's orgs and "org/team" slugs.
type Membership struct {
	Orgs  []string
	Teams []string
}

// Membership returns the orgs and teams visible to the token. Orgs that restrict
// third-party OAuth apps are not listed by GitHub.
func (c *GitHubClient) Membership(ctx context.Context, token string) (Membership, error) {
	var orgs []struct{ Login string }
	if code, _, err := c.get(ctx, token, "/user/orgs?per_page=100", &orgs); err != nil || code != 200 {
		if err == nil {
			err = fmt.Errorf("github /user/orgs returned %d", code)
		}
		return Membership{}, err
	}
	var teams []struct {
		Slug         string
		Organization struct{ Login string }
	}
	if code, _, err := c.get(ctx, token, "/user/teams?per_page=100", &teams); err != nil || code != 200 {
		if err == nil {
			err = fmt.Errorf("github /user/teams returned %d", code)
		}
		return Membership{}, err
	}
	var m Membership
	for _, o := range orgs {
		m.Orgs = append(m.Orgs, o.Login)
	}
	for _, t := range teams {
		m.Teams = append(m.Teams, t.Organization.Login+"/"+t.Slug)
	}
	return m, nil
}

// OrgRestricted reports whether org blocks this OAuth app. GitHub answers
// GET /user/memberships/orgs/{org} with 403 and a message naming the restriction.
func (c *GitHubClient) OrgRestricted(ctx context.Context, token, org string) bool {
	code, msg, err := c.get(ctx, token, "/user/memberships/orgs/"+org, nil)
	return err == nil && code == http.StatusForbidden && strings.Contains(msg, "OAuth App access restrictions")
}
