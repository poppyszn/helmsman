// Package loki is a small read client for Grafana Loki's HTTP API. Helmsman only ever queries it with
// selectors it builds itself from the catalog, never with LogQL typed by a user.
package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Entry is one stored log line with its stream labels.
type Entry struct {
	TS     time.Time
	Line   string
	Labels map[string]string
}

// Querier is what the history API needs, so tests can stand in a fake Loki.
type Querier interface {
	// QueryRange returns up to limit entries between start and end, newest first.
	QueryRange(ctx context.Context, logql string, start, end time.Time, limit int) ([]Entry, error)
	// Ready reports whether Loki answers.
	Ready(ctx context.Context) error
	// Labels lists the label names Loki knows, which shows whether Alloy has sent anything.
	Labels(ctx context.Context) ([]string, error)
}

// Source hands out the Loki to use right now, or nil when log history is not connected. The answer can
// change while the server runs, because an admin connects and disconnects Loki from the UI.
type Source interface{ Current() Querier }

// Static is a Source that always answers with one Querier (used by tests).
type Static struct{ Q Querier }

// Current implements Source.
func (s Static) Current() Querier { return s.Q }

// Client talks to Loki over HTTP.
type Client struct {
	BaseURL string
	User    string
	Pass    string
	HTTP    *http.Client
}

// New builds a client. user and pass are optional basic auth.
func New(baseURL, user, pass string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), User: user, Pass: pass, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.User != "" {
		req.SetBasicAuth(c.User, c.Pass)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(b))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("loki: %d %s", res.StatusCode, msg)
	}
	return b, nil
}

// Ready implements Querier.
func (c *Client) Ready(ctx context.Context) error {
	_, err := c.get(ctx, "/ready", nil)
	return err
}

// Labels implements Querier.
func (c *Client) Labels(ctx context.Context) ([]string, error) {
	b, err := c.get(ctx, "/loki/api/v1/labels", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("loki: decode labels: %w", err)
	}
	return out.Data, nil
}

// QueryRange implements Querier.
func (c *Client) QueryRange(ctx context.Context, logql string, start, end time.Time, limit int) ([]Entry, error) {
	q := url.Values{}
	q.Set("query", logql)
	q.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	q.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	q.Set("limit", strconv.Itoa(limit))
	q.Set("direction", "backward")
	b, err := c.get(ctx, "/loki/api/v1/query_range", q)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			Result []struct {
				Stream map[string]string `json:"stream"`
				Values [][2]string       `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("loki: decode response: %w", err)
	}
	var entries []Entry
	for _, s := range out.Data.Result {
		for _, v := range s.Values {
			ns, err := strconv.ParseInt(v[0], 10, 64)
			if err != nil {
				continue
			}
			entries = append(entries, Entry{TS: time.Unix(0, ns).UTC(), Line: v[1], Labels: s.Stream})
		}
	}
	return entries, nil
}

// Quote returns s as a LogQL string literal (double quoted, escaped).
func Quote(s string) string { return strconv.Quote(s) }
