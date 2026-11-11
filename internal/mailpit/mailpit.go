// Package mailpit is a small read client for Mailpit's HTTP API (https://mailpit.axllent.org). Helmsman
// lists and reads the messages a development environment sent, and never changes or deletes them.
package mailpit

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

// Address is a sender or recipient.
type Address struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}

// Summary is one row of the message list.
type Summary struct {
	ID          string    `json:"ID"`
	Read        bool      `json:"Read"`
	From        Address   `json:"From"`
	To          []Address `json:"To"`
	Subject     string    `json:"Subject"`
	Created     time.Time `json:"Created"`
	Snippet     string    `json:"Snippet"`
	Size        int64     `json:"Size"`
	Attachments int       `json:"Attachments"`
	Tags        []string  `json:"Tags"`
}

// List is a page of messages.
type List struct {
	Total    int       `json:"total"`
	Unread   int       `json:"unread"`
	Messages []Summary `json:"messages"`
}

// Attachment describes a file attached to a message.
type Attachment struct {
	PartID      string `json:"PartID"`
	FileName    string `json:"FileName"`
	ContentType string `json:"ContentType"`
	Size        int64  `json:"Size"`
}

// Message is one full message.
type Message struct {
	ID          string       `json:"ID"`
	From        Address      `json:"From"`
	To          []Address    `json:"To"`
	Cc          []Address    `json:"Cc"`
	Bcc         []Address    `json:"Bcc"`
	ReplyTo     []Address    `json:"ReplyTo"`
	Subject     string       `json:"Subject"`
	Date        time.Time    `json:"Date"`
	Tags        []string     `json:"Tags"`
	Text        string       `json:"Text"`
	HTML        string       `json:"HTML"`
	Size        int64        `json:"Size"`
	Attachments []Attachment `json:"Attachments"`
	Inline      []Attachment `json:"Inline"`
}

// Reader is what the mail API needs, so tests can stand in a fake Mailpit.
type Reader interface {
	// Info returns the Mailpit version, which shows that the address answers like Mailpit.
	Info(ctx context.Context) (string, error)
	// List returns messages newest first. A non empty query uses Mailpit's search syntax.
	List(ctx context.Context, query string, start, limit int) (List, error)
	Message(ctx context.Context, id string) (Message, error)
	// Attachment streams one part. The caller closes the body.
	Attachment(ctx context.Context, id, part string) (io.ReadCloser, string, error)
}

// Source hands out the Mailpit to use right now, or nil when mail is not connected.
type Source interface{ Current() Reader }

// Static is a Source that always answers with one Reader (used by tests).
type Static struct{ R Reader }

// Current implements Source.
func (s Static) Current() Reader { return s.R }

// APIError is a non success answer from Mailpit.
type APIError struct{ Status int }

func (e *APIError) Error() string { return fmt.Sprintf("mailpit answered HTTP %d", e.Status) }

// IsNotFound reports whether Mailpit said the message or part does not exist.
func IsNotFound(err error) bool {
	e, ok := err.(*APIError)
	return ok && e.Status == http.StatusNotFound
}

// Client talks to Mailpit over HTTP.
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

func (c *Client) open(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.User != "" || c.Pass != "" {
		req.SetBasicAuth(c.User, c.Pass)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		return nil, &APIError{Status: res.StatusCode}
	}
	return res, nil
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	res, err := c.open(ctx, path, q)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	// A message with a large HTML body is the biggest answer; 32 MB is far above any real mail.
	return json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(out)
}

// Info implements Reader.
func (c *Client) Info(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"Version"`
	}
	if err := c.getJSON(ctx, "/api/v1/info", nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// List implements Reader.
func (c *Client) List(ctx context.Context, query string, start, limit int) (List, error) {
	q := url.Values{"start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}}
	path := "/api/v1/messages"
	if strings.TrimSpace(query) != "" {
		path = "/api/v1/search"
		q.Set("query", query)
	}
	var out List
	err := c.getJSON(ctx, path, q, &out)
	if out.Messages == nil {
		out.Messages = []Summary{}
	}
	return out, err
}

// Message implements Reader.
func (c *Client) Message(ctx context.Context, id string) (Message, error) {
	var out Message
	err := c.getJSON(ctx, "/api/v1/message/"+url.PathEscape(id), nil, &out)
	return out, err
}

// Attachment implements Reader.
func (c *Client) Attachment(ctx context.Context, id, part string) (io.ReadCloser, string, error) {
	res, err := c.open(ctx, "/api/v1/message/"+url.PathEscape(id)+"/part/"+url.PathEscape(part), nil)
	if err != nil {
		return nil, "", err
	}
	return res.Body, res.Header.Get("Content-Type"), nil
}
