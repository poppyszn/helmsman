package gh

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Run is a workflow run.
type Run struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Title      string    `json:"display_title"`
	Status     string    `json:"status"`     // queued | in_progress | waiting | completed ...
	Conclusion string    `json:"conclusion"` // success | failure | cancelled ...
	HeadSHA    string    `json:"head_sha"`
	HTMLURL    string    `json:"html_url"`
	Event      string    `json:"event"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Actor      struct {
		Login string `json:"login"`
	} `json:"actor"`
}

// Step is one step of a job.
type Step struct {
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	Number      int        `json:"number"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

// Job is one job of a run.
type Job struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	HTMLURL     string     `json:"html_url"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Steps       []Step     `json:"steps"`
}

// APIError is a non-success response from GitHub.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("github: %d %s", e.Status, e.Message) }

// RateLimitError means GitHub asked us to back off.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github rate limit reached, retry in %s", e.RetryAfter.Round(time.Second))
}

// IsNotFound and IsUnprocessable classify API errors.
func IsNotFound(err error) bool {
	var a *APIError
	return errors.As(err, &a) && a.Status == http.StatusNotFound
}

// IsUnprocessable is true for 422, for example a dispatch with an unknown input or ref.
func IsUnprocessable(err error) bool {
	var a *APIError
	return errors.As(err, &a) && a.Status == http.StatusUnprocessableEntity
}

func fromResponse(res *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
	var m struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(b, &m)
	if m.Message == "" {
		m.Message = strings.TrimSpace(string(b))
	}
	limited := res.StatusCode == http.StatusTooManyRequests ||
		(res.StatusCode == http.StatusForbidden && (res.Header.Get("Retry-After") != "" ||
			res.Header.Get("X-RateLimit-Remaining") == "0" || strings.Contains(strings.ToLower(m.Message), "rate limit")))
	if limited {
		wait := time.Minute
		if v := res.Header.Get("Retry-After"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				wait = time.Duration(n) * time.Second
			}
		} else if v := res.Header.Get("X-RateLimit-Reset"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				if d := time.Until(time.Unix(n, 0)); d > 0 {
					wait = d
				}
			}
		}
		return &RateLimitError{RetryAfter: wait}
	}
	return &APIError{Status: res.StatusCode, Message: m.Message}
}

// API is what the actions service needs from GitHub. Tests use the fake server through Client.
type API interface {
	Dispatch(ctx context.Context, repo, workflow, ref string, inputs map[string]string) error
	ListRuns(ctx context.Context, repo, workflow string, perPage int, cacheTTL time.Duration) ([]Run, error)
	GetRun(ctx context.Context, repo string, id int64) (Run, error)
	ListJobs(ctx context.Context, repo string, runID int64) ([]Job, error)
	JobLogs(ctx context.Context, repo string, jobID int64, maxBytes int) (string, bool, error)
	Cancel(ctx context.Context, repo string, runID int64) error
	DefaultBranch(ctx context.Context, repo string) (string, error)
	TagForCommit(ctx context.Context, repo, sha string) (string, bool, error)
	// WorkflowFile returns the workflow file's text from the default branch.
	WorkflowFile(ctx context.Context, repo, workflow string) (string, error)
	// CheckAccess verifies that the App is installed and that a token can be minted.
	CheckAccess(ctx context.Context, repo string) error
}

type cacheEntry struct {
	etag string
	body []byte
	at   time.Time
}

// Client is the HTTP implementation of API.
type Client struct {
	BaseURL string
	Tokens  TokenSource
	HTTP    *http.Client
	Now     func() time.Time

	mu           sync.Mutex
	cache        map[string]cacheEntry
	blockedUntil time.Time
}

// NewClient builds a client.
func NewClient(baseURL string, tokens TokenSource) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Tokens: tokens,
		HTTP: &http.Client{Timeout: 20 * time.Second}, cache: map[string]cacheEntry{}}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// do sends one request. A 401 drops the cached token and retries once with a fresh one.
// While GitHub has told us to back off, calls fail fast with a RateLimitError.
func (c *Client) do(ctx context.Context, repo, method, path string, body any, hdr http.Header) (*http.Response, error) {
	c.mu.Lock()
	if d := c.blockedUntil.Sub(c.now()); d > 0 {
		c.mu.Unlock()
		return nil, &RateLimitError{RetryAfter: d}
	}
	c.mu.Unlock()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	for attempt := 0; ; attempt++ {
		tok, err := c.Tokens.Token(ctx, repo)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		for k, v := range hdr {
			req.Header[k] = v
		}
		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode == http.StatusUnauthorized && attempt == 0 {
			_ = res.Body.Close()
			c.Tokens.Invalidate(repo)
			continue
		}
		return res, nil
	}
}

func (c *Client) checked(res *http.Response) error {
	if res.StatusCode < 300 {
		return nil
	}
	err := fromResponse(res)
	var rl *RateLimitError
	if errors.As(err, &rl) {
		c.mu.Lock()
		c.blockedUntil = c.now().Add(rl.RetryAfter)
		c.mu.Unlock()
	}
	return err
}

// getJSON GETs a path and decodes it. With ttl > 0 a cached copy younger than ttl is served
// without a request; otherwise the request is conditional (If-None-Match) so an unchanged
// answer costs no rate limit.
func (c *Client) getJSON(ctx context.Context, repo, path string, ttl time.Duration, out any) error {
	key := repo + " " + path
	c.mu.Lock()
	ent, ok := c.cache[key]
	c.mu.Unlock()
	if ok && ttl > 0 && c.now().Sub(ent.at) < ttl {
		return json.Unmarshal(ent.body, out)
	}
	hdr := http.Header{}
	if ok && ent.etag != "" {
		hdr.Set("If-None-Match", ent.etag)
	}
	res, err := c.do(ctx, repo, http.MethodGet, path, nil, hdr)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified && ok {
		ent.at = c.now()
		c.mu.Lock()
		c.cache[key] = ent
		c.mu.Unlock()
		return json.Unmarshal(ent.body, out)
	}
	if err := c.checked(res); err != nil {
		return err
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return err
	}
	if et := res.Header.Get("ETag"); et != "" {
		c.mu.Lock()
		if len(c.cache) > 500 {
			c.cache = map[string]cacheEntry{}
		}
		c.cache[key] = cacheEntry{etag: et, body: b, at: c.now()}
		c.mu.Unlock()
	}
	return json.Unmarshal(b, out)
}

func wf(name string) string { return url.PathEscape(name) }

// Dispatch starts a workflow_dispatch run. GitHub answers 204 and does not say which run
// it created; the caller correlates by the correlation_id input.
func (c *Client) Dispatch(ctx context.Context, repo, workflow, ref string, inputs map[string]string) error {
	res, err := c.do(ctx, repo, http.MethodPost, "/repos/"+repo+"/actions/workflows/"+wf(workflow)+"/dispatches",
		map[string]any{"ref": ref, "inputs": inputs}, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return c.checked(res)
}

// ListRuns lists recent runs of a workflow, newest first.
func (c *Client) ListRuns(ctx context.Context, repo, workflow string, perPage int, ttl time.Duration) ([]Run, error) {
	var out struct {
		Runs []Run `json:"workflow_runs"`
	}
	p := fmt.Sprintf("/repos/%s/actions/workflows/%s/runs?per_page=%d", repo, wf(workflow), perPage)
	if err := c.getJSON(ctx, repo, p, ttl, &out); err != nil {
		return nil, err
	}
	return out.Runs, nil
}

// GetRun reads one run.
func (c *Client) GetRun(ctx context.Context, repo string, id int64) (Run, error) {
	var r Run
	err := c.getJSON(ctx, repo, fmt.Sprintf("/repos/%s/actions/runs/%d", repo, id), 0, &r)
	return r, err
}

// ListJobs lists the jobs and steps of a run.
func (c *Client) ListJobs(ctx context.Context, repo string, runID int64) ([]Job, error) {
	var out struct {
		Jobs []Job `json:"jobs"`
	}
	err := c.getJSON(ctx, repo, fmt.Sprintf("/repos/%s/actions/runs/%d/jobs?per_page=50", repo, runID), 0, &out)
	return out.Jobs, err
}

// JobLogs downloads a job's log text, keeping at most maxBytes (the tail is kept, since
// failures are at the end). The second result says whether it was cut.
func (c *Client) JobLogs(ctx context.Context, repo string, jobID int64, maxBytes int) (string, bool, error) {
	res, err := c.do(ctx, repo, http.MethodGet, fmt.Sprintf("/repos/%s/actions/jobs/%d/logs", repo, jobID), nil, nil)
	if err != nil {
		return "", false, err
	}
	defer res.Body.Close()
	if err := c.checked(res); err != nil {
		return "", false, err
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, int64(maxBytes)*4+1))
	if err != nil {
		return "", false, err
	}
	if len(b) > maxBytes {
		return strings.ToValidUTF8(string(b[len(b)-maxBytes:]), ""), true, nil
	}
	return strings.ToValidUTF8(string(b), ""), false, nil
}

// Cancel asks GitHub to cancel a run.
func (c *Client) Cancel(ctx context.Context, repo string, runID int64) error {
	res, err := c.do(ctx, repo, http.MethodPost, fmt.Sprintf("/repos/%s/actions/runs/%d/cancel", repo, runID), nil, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return c.checked(res)
}

// DefaultBranch returns the repository's default branch.
func (c *Client) DefaultBranch(ctx context.Context, repo string) (string, error) {
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.getJSON(ctx, repo, "/repos/"+repo, 30*time.Second, &r); err != nil {
		return "", err
	}
	return r.DefaultBranch, nil
}

// TagForCommit finds a tag that points at the commit. ok is false when none does.
func (c *Client) TagForCommit(ctx context.Context, repo, sha string) (string, bool, error) {
	var tags []struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := c.getJSON(ctx, repo, "/repos/"+repo+"/tags?per_page=100", 30*time.Second, &tags); err != nil {
		return "", false, err
	}
	for _, t := range tags {
		if sha != "" && (t.Commit.SHA == sha || strings.HasPrefix(t.Commit.SHA, sha) || strings.HasPrefix(sha, t.Commit.SHA)) {
			return t.Name, true, nil
		}
	}
	return "", false, nil
}

// WorkflowFile returns the text of .github/workflows/<workflow>.
func (c *Client) WorkflowFile(ctx context.Context, repo, workflow string) (string, error) {
	var f struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := c.getJSON(ctx, repo, "/repos/"+repo+"/contents/.github/workflows/"+wf(workflow), 0, &f); err != nil {
		return "", err
	}
	if f.Encoding != "base64" {
		return f.Content, nil
	}
	b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(f.Content, "\n", ""))
	return string(b), err
}

// CheckAccess mints (or reuses) an installation token for the repository.
func (c *Client) CheckAccess(ctx context.Context, repo string) error {
	if _, _, ok := splitRepo(repo); !ok {
		return fmt.Errorf("invalid repository %q", repo)
	}
	// Forget cached installation data so an uninstalled App or a revoked key is noticed.
	c.Tokens.Invalidate(repo)
	_, err := c.Tokens.Token(ctx, repo)
	return err
}

// HasDispatchTrigger reports whether workflow YAML declares workflow_dispatch under "on".
// It is a text check, enough to catch the common mistake of a missing trigger.
func HasDispatchTrigger(yamlText string) bool {
	for _, ln := range strings.Split(yamlText, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasPrefix(t, "workflow_dispatch") || strings.Contains(t, "workflow_dispatch:") ||
			(strings.HasPrefix(t, "on:") && strings.Contains(t, "workflow_dispatch")) {
			return true
		}
	}
	return false
}
