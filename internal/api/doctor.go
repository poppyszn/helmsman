package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/poppyszn/helmsman/internal/health"
	"github.com/poppyszn/helmsman/internal/rbac"
)

// Check statuses. A failed check is something that is broken now, a warning is something that will bite
// later or that is probably unintended, and info is a fact worth seeing.
const (
	checkOK   = "ok"
	checkWarn = "warn"
	checkFail = "fail"
	checkInfo = "info"
)

// doctorCheck is one line of the setup check page.
type doctorCheck struct {
	ID     string `json:"id"`
	Group  string `json:"group"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	// Fix says what to do, in words an admin can follow. Empty for ok and info.
	Fix string `json:"fix,omitempty"`
}

type doctor struct{ checks []doctorCheck }

func (d *doctor) add(id, group, title, status, detail, fix string) {
	d.checks = append(d.checks, doctorCheck{ID: id, Group: group, Title: title, Status: status, Detail: detail, Fix: fix})
}

func isPrivateOrLoopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// handleDoctor runs the checks the server can make by itself. Checks that need the browser, such as the
// WebSocket test, run on the page.
func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	d := &doctor{}
	s.doctorServer(d, r)
	s.doctorSignIn(ctx, d)
	s.doctorGitHub(d)
	s.doctorCatalog(ctx, d)
	s.doctorDatabase(ctx, d)
	counts := map[string]int{}
	for _, c := range d.checks {
		counts[c.Status]++
	}
	writeJSON(w, http.StatusOK, map[string]any{"checks": d.checks, "counts": counts, "generated_at": time.Now()})
}

func (s *Server) doctorServer(d *doctor, r *http.Request) {
	const g = "Server and HTTPS"
	base, err := url.Parse(s.Cfg.BaseURL)
	switch {
	case err != nil || base.Host == "":
		d.add("base-url", g, "Public address", checkFail, "IDP_BASE_URL is not a valid address: "+s.Cfg.BaseURL, "Set IDP_BASE_URL to the address people use, for example https://portal.example.com, and recreate the server.")
		return
	case base.Scheme != "https" && !isPrivateOrLoopback(base.Hostname()) && base.Hostname() != "localhost":
		d.add("base-url", g, "Public address", checkWarn, "IDP_BASE_URL is "+s.Cfg.BaseURL+", which is not https.", "Serve Helmsman over https and set IDP_BASE_URL to the https address. Cookies, GitHub sign-in and agents expect it.")
	default:
		d.add("base-url", g, "Public address", checkOK, s.Cfg.BaseURL, "")
	}
	if !strings.EqualFold(r.Host, base.Host) {
		d.add("host-match", g, "This page's address matches", checkFail,
			fmt.Sprintf("You opened Helmsman as %s, but IDP_BASE_URL says %s.", r.Host, base.Host),
			"Open it at the address in IDP_BASE_URL, or correct IDP_BASE_URL. The GitHub callback URL must be that address plus /auth/github/callback. If this is a reverse proxy, check that it passes the Host header ($host in nginx).")
	} else {
		d.add("host-match", g, "This page's address matches", checkOK, "The proxy passes the Host header through.", "")
	}
	if base.Scheme == "https" {
		if p := r.Header.Get("X-Forwarded-Proto"); p != "https" {
			d.add("forwarded-proto", g, "Proxy tells Helmsman the request was https", checkWarn, "X-Forwarded-Proto is "+orNone(p)+".", "Add proxy_set_header X-Forwarded-Proto $scheme; to the proxy (Caddy does this by itself).")
		} else {
			d.add("forwarded-proto", g, "Proxy tells Helmsman the request was https", checkOK, "X-Forwarded-Proto: https", "")
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && isPrivateOrLoopback(host) && r.Header.Get("X-Forwarded-For") == "" {
		d.add("forwarded-for", g, "Real client addresses reach Helmsman", checkWarn, "Every request appears to come from "+host+", and no X-Forwarded-For header arrives.",
			"Add proxy_set_header X-Forwarded-For $remote_addr; to the proxy. Without it, rate limits and the audit log record the proxy instead of the person.")
	} else {
		d.add("forwarded-for", g, "Real client addresses reach Helmsman", checkOK, "Requests carry the client's own address.", "")
	}
	d.add("websocket", g, "Live connections (WebSocket)", checkInfo, "Checked in your browser, below.", "")
}

func orNone(v string) string {
	if v == "" {
		return "missing"
	}
	return `"` + v + `"`
}

func (s *Server) doctorSignIn(ctx context.Context, d *doctor) {
	const g = "Sign-in"
	id := s.Cfg.OAuthClientID
	switch {
	case s.Cfg.GitHubMode != "real":
		d.add("oauth", g, "GitHub sign-in", checkWarn, "Helmsman is in "+s.Cfg.GitHubMode+" mode, which uses a fake GitHub.", "Set IDP_GITHUB_MODE=real on a deployed server.")
	case id == "":
		d.add("oauth", g, "GitHub sign-in", checkFail, "GITHUB_OAUTH_CLIENT_ID is empty.", "Create a GitHub OAuth App and put its client ID and secret in the server's .env (docs/server-setup.md, step 2).")
	case strings.HasPrefix(id, "Iv"):
		d.add("oauth", g, "GitHub sign-in", checkFail, "The client ID starts with \"Iv\", which is a GitHub App's, not an OAuth App's.",
			"Sign-in needs an OAuth App (it reads organisation and team membership). Create one under Developer settings > OAuth Apps and use its client ID and secret. The GitHub App belongs in GITHUB_APP_ID only.")
	default:
		d.add("oauth", g, "GitHub sign-in", checkOK, "An OAuth App client ID is set.", "")
	}
	if s.Cfg.OAuthClientSecret == "" && s.Cfg.GitHubMode == "real" {
		d.add("oauth-secret", g, "OAuth client secret", checkFail, "GITHUB_OAUTH_CLIENT_SECRET is empty.", "Generate a client secret on the OAuth App and set it in .env.")
	}
	if len(s.Cfg.AllowedOrgs) == 0 {
		d.add("orgs", g, "Who may sign in", checkFail, "IDP_ALLOWED_ORGS is empty, so nobody outside the break-glass admin can sign in.", "Set IDP_ALLOWED_ORGS to the GitHub organisation slug or slugs whose members may sign in.")
	} else {
		d.add("orgs", g, "Who may sign in", checkInfo, "Members of: "+strings.Join(s.Cfg.AllowedOrgs, ", "), "")
	}
	var admins int
	if err := s.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE is_admin`).Scan(&admins); err == nil {
		switch {
		case admins == 0:
			d.add("admins", g, "Administrators", checkWarn, "No administrator has signed in yet.", "Set IDP_BOOTSTRAP_ADMIN_GITHUB_IDS to your numeric GitHub ID (gh api users/<login> --jq .id), recreate the server and sign in.")
		default:
			d.add("admins", g, "Administrators", checkOK, fmt.Sprintf("%d administrator(s).", admins), "")
		}
	}
	if s.Cfg.BreakglassEnabled {
		d.add("breakglass", g, "Break-glass admin", checkWarn, "The emergency admin login is turned on.", "It is meant for when GitHub sign-in is down. Turn it off (IDP_BREAKGLASS_ENABLED=false) when you do not need it.")
	}
}

func (s *Server) doctorGitHub(d *doctor) {
	const g = "GitHub runs (migrations, seeds, deployments)"
	if s.Cfg.GitHubMode != "real" {
		return
	}
	if s.Cfg.GitHubAppID == "" {
		d.add("app", g, "GitHub App", checkWarn, "GITHUB_APP_ID is not set, so migrations, seeds and the Deployments tab are off.", "Create a GitHub App (Actions read and write, Contents read, Metadata read), install it on your repositories, and set GITHUB_APP_ID and the key file. Skip this if you only want status, logs and restarts.")
		return
	}
	if p := s.Cfg.GitHubAppKeyPath; p == "" {
		d.add("app-key", g, "GitHub App key", checkFail, "GITHUB_APP_PRIVATE_KEY_PATH is empty.", "Set GITHUB_APP_KEY_FILE in .env to the .pem file on the host.")
	} else if f, err := os.Open(p); err != nil {
		d.add("app-key", g, "GitHub App key", checkFail, "The key file cannot be read: "+sanitizeErr(err), "The server runs as user 65532, which must own the file: sudo chown 65532:65532 <file> && sudo chmod 400 <file>, then recreate the server.")
	} else {
		_ = f.Close()
		d.add("app-key", g, "GitHub App key", checkOK, "The key file is readable.", "")
	}
	for _, c := range s.Actions.Checks() {
		title := "Repository " + c.Repo
		switch {
		case !c.Installed:
			d.add("repo-"+c.Repo, g, title, checkFail, "The GitHub App is not installed on this repository.", "Install the App on it (GitHub > the org > Settings > GitHub Apps > Configure). An App owned by another org must be public to be installed here.")
		case c.Problem != "":
			d.add("repo-"+c.Repo, g, title, checkWarn, c.Problem, "Check the workflow files named in the catalog: they must exist on the default branch and have a workflow_dispatch trigger that declares the inputs Helmsman sends (docs/server-setup.md, \"Migrations and seeds\").")
		default:
			d.add("repo-"+c.Repo, g, title, checkOK, "Installed, and the workflows can be started.", "")
		}
	}
}

func sanitizeErr(err error) string { return err.Error() }

func (s *Server) doctorCatalog(ctx context.Context, d *doctor) {
	const g = "Catalog, agents and apps"
	st := s.Catalog.Status()
	switch {
	case st.LastError != "":
		d.add("catalog", g, "Catalog", checkFail, "The last reload was rejected, so the previous catalog stays active: "+firstLine(st.LastError), "Fix the file named on the Catalog page. A reload is all or nothing.")
	case st.Apps == 0:
		d.add("catalog", g, "Catalog", checkWarn, "The catalog has no apps yet.", "Add an app to apps.yaml (docs/catalog.md). The Agents page can draft the entry from what it discovers.")
	default:
		d.add("catalog", g, "Catalog", checkOK, fmt.Sprintf("%d app(s) loaded.", st.Apps), "")
	}
	apps, err := s.Store.ListApps(ctx)
	if err != nil {
		return
	}
	seenAgent := map[string]bool{}
	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
	for _, a := range apps {
		for _, e := range a.Envs {
			spec := parseSpec(e.Spec)
			title := fmt.Sprintf("%s / %s", a.ID, e.Env)
			if !seenAgent[e.Agent] {
				seenAgent[e.Agent] = true
				s.doctorAgent(ctx, d, e.Agent)
			}
			v, err := s.computeStatus(ctx, a.ID, e, rbac.Principal{}, false)
			if err != nil {
				d.add("env-"+a.ID+"-"+e.Env, g, title, checkWarn, "Its status could not be worked out.", "")
				continue
			}
			switch {
			case v.Reason == "container_missing":
				d.add("env-"+a.ID+"-"+e.Env, g, title, checkWarn,
					fmt.Sprintf("The agent %s is online but sees no container for compose_project %q, service %q.", e.Agent, spec.Container.ComposeProject, spec.Container.Service),
					"The names must match the container's compose labels exactly (docker ps --format '{{.Label \"com.docker.compose.project\"}} {{.Label \"com.docker.compose.service\"}}'), and agent.yaml must list that service under allow. The Agents page shows what the agent can see.")
			case v.Reason == "agent_offline" || v.Reason == "docker_unavailable":
				// Reported once on the agent's own line.
			case v.Status == health.Down:
				d.add("env-"+a.ID+"-"+e.Env, g, title, checkFail, "The environment is down.", "")
			case v.Status == health.Degraded:
				d.add("env-"+a.ID+"-"+e.Env, g, title, checkWarn, "The environment is degraded (slow health check or an unhealthy container).", "")
			default:
				if spec.Health.URL == "" {
					d.add("env-"+a.ID+"-"+e.Env, g, title, checkInfo, "No health check is configured.", "")
				} else {
					d.add("env-"+a.ID+"-"+e.Env, g, title, checkOK, "Healthy.", "")
				}
			}
		}
	}
}

func (s *Server) doctorAgent(ctx context.Context, d *doctor, id string) {
	const g = "Catalog, agents and apps"
	a, err := s.Store.GetAgent(ctx, id)
	title := "Agent " + id
	switch {
	case err != nil:
		d.add("agent-"+id, g, title, checkFail, "The catalog names an agent that is not registered.", "Create it on the Agents page, with exactly this name.")
	case a.RevokedAt != nil:
		d.add("agent-"+id, g, title, checkFail, "The agent is revoked.", "Create a new agent and point the catalog at it.")
	case !a.Joined:
		d.add("agent-"+id, g, title, checkFail, "The agent has not joined yet.", "Start it on the host with its join token (a new token is on the Agents page; each is single use and valid for an hour).")
	default:
		online, _ := s.Hub.Online(id)
		switch {
		case !online:
			d.add("agent-"+id, g, title, checkFail, "The agent is offline.", "On its host: docker compose -f compose.agent.yml logs --tail 30. Check that the host can reach the portal's /agent/ws address.")
		case a.Alert != nil && *a.Alert != "":
			d.add("agent-"+id, g, title, checkFail, "The agent raised an alert: "+*a.Alert, "Acknowledge it on the Agents page once you know the cause. A second agent using the same credential means it was copied.")
		case a.DockerOK != nil && !*a.DockerOK:
			d.add("agent-"+id, g, title, checkFail, "The agent cannot reach Docker.", "Check the Docker socket mount and DOCKER_GID in the agent's .env.")
		default:
			d.add("agent-"+id, g, title, checkOK, "Online.", "")
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func (s *Server) doctorDatabase(ctx context.Context, d *doctor) {
	const g = "Database and security"
	var who, owner string
	if err := s.Store.Pool.QueryRow(ctx, `SELECT current_user, (SELECT tableowner FROM pg_tables WHERE tablename = 'audit_events' LIMIT 1)`).Scan(&who, &owner); err != nil {
		return
	}
	if who == owner {
		d.add("db-role", g, "Database role", checkWarn, "The server connects to the database as the table owner ("+who+"), so it could change the audit log's schema.",
			"Run the server as the limited helmsman_app role and keep the owner for migrations only (IDP_DATABASE_URL and IDP_MIGRATE_DATABASE_URL, docs/security.md).")
	} else {
		d.add("db-role", g, "Database role", checkOK, "The server runs as "+who+", not as the table owner.", "")
	}
	var immutable bool
	if err := s.Store.Pool.QueryRow(ctx, `SELECT count(*) = 2 FROM pg_trigger WHERE tgrelid = 'audit_events'::regclass AND NOT tgisinternal AND tgname IN ('audit_events_no_update_delete', 'audit_events_no_truncate')`).Scan(&immutable); err == nil {
		if immutable {
			d.add("audit", g, "Audit log protection", checkOK, "The audit log rejects updates, deletes and truncation.", "")
		} else {
			d.add("audit", g, "Audit log protection", checkFail, "The audit log's protective triggers are missing.", "Re-run the migrations as the owner role.")
		}
	}
}

// handleDoctorWS is the endpoint the setup check page connects to from the browser. It answers once and
// closes, which proves that WebSocket upgrades and the same-origin check work through the proxy.
func (s *Server) handleDoctorWS(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_ = ws.Write(ctx, websocket.MessageText, []byte("ok"))
	_ = ws.Close(websocket.StatusNormalClosure, "done")
}
