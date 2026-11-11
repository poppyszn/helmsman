// Package api holds HTTP handlers and the central route table.
package api

import "net/http"

// Public marks a route that needs no permission (health probes, login flow).
// Every route must declare either Public or a real permission; there is no default.
const Public = "public"

// Route is one entry of the central route table.
type Route struct {
	Method     string
	Path       string
	Permission string // Public or a permission name such as "app.view"
	Handler    http.HandlerFunc
}

// Routes returns the route table. Deny by default: anything not listed is a 404,
// and a route without a Permission fails the route-table test and refuses to mount.
func (s *Server) Routes() []Route {
	return []Route{
		{http.MethodGet, "/healthz", Public, s.handleHealthz},
		{http.MethodGet, "/readyz", Public, s.handleReadyz},
	}
}
