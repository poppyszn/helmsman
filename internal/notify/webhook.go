package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

// WebhookSender posts one event to a webhook.
type WebhookSender interface {
	Post(ctx context.Context, url, secret, event string, p Payload) error
}

// MailSender sends one plain text email.
type MailSender interface {
	Send(ctx context.Context, to []string, subject, body string) error
}

// blockedIP is true for addresses a webhook must never reach: cloud metadata and link-local ranges,
// unspecified and multicast addresses. Loopback and private networks are allowed.
func blockedIP(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.Equal(net.ParseIP("169.254.169.254")) || ip.Equal(net.ParseIP("100.100.100.200"))
}

// safeControl runs when a connection is about to be made, after DNS, so a host name that resolves to a
// blocked address (or changes its answer between the check and the connection) is refused too.
func safeControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if blockedIP(host) {
		return fmt.Errorf("the address %s is not allowed", host)
	}
	return nil
}

type httpWebhook struct{ client *http.Client }

func (h *httpWebhook) http() *http.Client {
	if h.client == nil {
		d := &net.Dialer{Timeout: 5 * time.Second, Control: safeControl}
		h.client = &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: 5 * time.Second, DisableKeepAlives: true},
			// A redirect could send the signed body somewhere the admin did not choose.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	return h.client
}

// Sign returns the signature header value for a body sent at ts: HMAC-SHA256 over "ts.body".
func Sign(secret string, ts int64, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	_, _ = m.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	_, _ = m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// Post implements WebhookSender. A 2xx answer is success; anything else is an error, and the body of the
// answer is never kept.
func (h *httpWebhook) Post(ctx context.Context, url, secret, event string, p Payload) error {
	if p.Time.IsZero() {
		p.Time = time.Now()
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	ts := time.Now().Unix()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Helmsman-Webhook")
	req.Header.Set("X-Helmsman-Event", event)
	req.Header.Set("X-Helmsman-Timestamp", strconv.FormatInt(ts, 10))
	if secret != "" {
		req.Header.Set("X-Helmsman-Signature", Sign(secret, ts, body))
	}
	res, err := h.http().Do(req)
	if err != nil {
		return fmt.Errorf("the request failed: %w", unwrapURL(err))
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("the receiver answered HTTP %d", res.StatusCode)
	}
	return nil
}

// unwrapURL drops the URL from a transport error, so a path that carries a secret is not logged or shown.
func unwrapURL(err error) error {
	if ue, ok := err.(interface{ Unwrap() error }); ok {
		if in := ue.Unwrap(); in != nil {
			return in
		}
	}
	return err
}
