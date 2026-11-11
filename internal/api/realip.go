package api

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// DefaultTrustedProxies are the networks a reverse proxy in front of the server may come from:
// loopback (host nginx) and the private ranges Docker networks use (Caddy or nginx in compose).
// A client on the public internet cannot connect from these, so it cannot pass itself off as a proxy.
const DefaultTrustedProxies = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

func parseCIDRs(list string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, c := range strings.Split(list, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			if strings.Contains(c, ":") {
				c += "/128"
			} else {
				c += "/32"
			}
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", c, err)
		}
		out = append(out, n)
	}
	return out, nil
}

func inNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientAddr works out the real client from X-Forwarded-For. The header is believed only when
// the connection itself comes from a trusted proxy, and it is read from the right: the first
// address that is not one of our own proxies is the client. A caller that connects directly can
// put anything in the header and it is ignored, unlike chi's RealIP middleware, which trusts it.
func clientAddr(remoteAddr, xff string, trusted []*net.IPNet) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !inNets(peer, trusted) || xff == "" {
		return host
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip == nil {
			return host // malformed header: do not guess
		}
		if !inNets(ip, trusted) {
			return ip.String()
		}
	}
	return host
}

// realIP replaces chi's RealIP. It sets r.RemoteAddr to the client address so that rate limits
// and the audit log see the person, not the proxy.
func realIP(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.RemoteAddr = net.JoinHostPort(clientAddr(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), trusted), "0")
			next.ServeHTTP(w, r)
		})
	}
}
