// Package notify sends the events an admin chooses to email recipients and webhooks. Events come from the
// run tracker and from a watcher of environment, agent and deployment changes. A delivery is stored first,
// then sent by a worker with retries, so a slow or down receiver never blocks Helmsman and nothing is
// lost across a restart.
package notify

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Event types an admin can subscribe a channel to.
const (
	RunStarted         = "run.started"
	RunFailed          = "run.failed"
	RunSucceeded       = "run.succeeded"
	RunPendingApproval = "run.pending_approval"
	DeployStarted      = "deploy.started"
	DeployFailed       = "deploy.failed"
	DeploySucceeded    = "deploy.succeeded"
	WorkflowFailed     = "workflow.failed"
	WorkflowSucceeded  = "workflow.succeeded"
	EnvDown            = "env.down"
	EnvDegraded        = "env.degraded"
	EnvRecovered       = "env.recovered"
	EnvCrashLoop       = "env.crash_loop"
	AgentOffline       = "agent.offline"
	AgentOnline        = "agent.online"
)

// Type describes an event type for the admin page.
type Type struct {
	ID      string `json:"id"`
	Group   string `json:"group"`
	Label   string `json:"label"`
	Default bool   `json:"default"` // pre-selected for a new channel
}

// Types lists every event type, grouped, in the order the page shows them.
var Types = []Type{
	{EnvDown, "Uptime", "An environment went down", true},
	{EnvDegraded, "Uptime", "An environment became slow or unhealthy", false},
	{EnvRecovered, "Uptime", "An environment recovered", false},
	{EnvCrashLoop, "Uptime", "A container is crash looping", true},
	{AgentOffline, "Uptime", "An agent (a host) went offline", true},
	{AgentOnline, "Uptime", "An agent came back online", false},
	{DeployStarted, "Deployments", "A deployment started", false},
	{DeployFailed, "Deployments", "A deployment failed", true},
	{DeploySucceeded, "Deployments", "A deployment succeeded", false},
	{RunPendingApproval, "Migrations and seeds", "A run is waiting for approval", true},
	{RunStarted, "Migrations and seeds", "A migration or seed started", false},
	{RunFailed, "Migrations and seeds", "A migration or seed failed", true},
	{RunSucceeded, "Migrations and seeds", "A migration or seed succeeded", false},
	{WorkflowFailed, "Other GitHub workflows", "Another workflow of the app's repository failed (tests, builds, and so on)", false},
	{WorkflowSucceeded, "Other GitHub workflows", "Another workflow of the app's repository succeeded", false},
}

// ValidType reports whether id is a known event type.
func ValidType(id string) bool {
	for _, t := range Types {
		if t.ID == id {
			return true
		}
	}
	return false
}

// Event is something worth telling people about. It never carries log text or secrets.
type Event struct {
	Type string `json:"type"`
	// App and Env are catalog ids. Both are empty for an event about an agent or a whole repository.
	App string `json:"app,omitempty"`
	Env string `json:"env,omitempty"`
	// Apps lists every app an event about a host or a repository concerns (the apps on an agent, the apps of
	// a repository), so a channel limited to some apps still gets it.
	Apps []string `json:"apps,omitempty"`
	// Tier is nonprod or prod when the event belongs to an environment, empty otherwise.
	Tier    string `json:"tier,omitempty"`
	Title   string `json:"title"`
	Message string `json:"message,omitempty"`
	// Path is a page in the portal that explains the event, for example /apps/billing-api.
	Path    string            `json:"path,omitempty"`
	Details map[string]string `json:"details,omitempty"`
	// Key identifies what happened, so the same thing is not announced twice. Events of type env.* and
	// agent.* are also held back for ten minutes after the same key was announced, to stop a flapping
	// environment from filling inboxes.
	Key string    `json:"key"`
	At  time.Time `json:"at"`
}

// cooldown is how long a repeated env.* or agent.* event is suppressed per channel.
const cooldown = 10 * time.Minute

func flappy(t string) bool { return strings.HasPrefix(t, "env.") || strings.HasPrefix(t, "agent.") }

// Scope is what a channel is limited to. Empty lists mean everything.
type Scope struct {
	Tier string // all | prod | nonprod
	Apps []string
	Envs []string
}

func inList(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Matches reports whether a channel with these events and scope wants the event.
func Matches(events []string, sc Scope, ev Event) bool {
	if !inList(events, ev.Type) {
		return false
	}
	switch sc.Tier {
	case "prod":
		if ev.Tier != "prod" {
			return false
		}
	case "nonprod":
		if ev.Tier != "nonprod" {
			return false
		}
	}
	if len(sc.Apps) > 0 {
		ok := ev.App != "" && inList(sc.Apps, ev.App)
		for _, a := range ev.Apps {
			ok = ok || inList(sc.Apps, a)
		}
		if !ok {
			return false
		}
	}
	// An event with no environment (a host going offline) cannot be scoped to one, so it does not pass an
	// environment filter.
	if len(sc.Envs) > 0 && (ev.Env == "" || !inList(sc.Envs, ev.Env)) {
		return false
	}
	return true
}

// Text renders an event for people: a subject line and a plain text body.
func Text(ev Event, baseURL string) (subject, body string) {
	subject = "[Helmsman] " + ev.Title
	var b strings.Builder
	b.WriteString(ev.Title + "\n\n")
	if ev.Message != "" {
		b.WriteString(ev.Message + "\n\n")
	}
	keys := make([]string, 0, len(ev.Details))
	for k := range ev.Details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, ev.Details[k])
	}
	if ev.Path != "" && baseURL != "" {
		fmt.Fprintf(&b, "\nOpen in Helmsman: %s%s\n", strings.TrimRight(baseURL, "/"), ev.Path)
	}
	return subject, b.String()
}

// Payload is the JSON a webhook receives.
type Payload struct {
	Event   string            `json:"event"`
	Time    time.Time         `json:"time"`
	App     string            `json:"app,omitempty"`
	Env     string            `json:"env,omitempty"`
	Tier    string            `json:"tier,omitempty"`
	Title   string            `json:"title"`
	Message string            `json:"message,omitempty"`
	URL     string            `json:"url,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}

// ToPayload builds the webhook body.
func ToPayload(ev Event, baseURL string) Payload {
	p := Payload{Event: ev.Type, Time: ev.At, App: ev.App, Env: ev.Env, Tier: ev.Tier, Title: ev.Title, Message: ev.Message, Details: ev.Details}
	if ev.Path != "" && baseURL != "" {
		p.URL = strings.TrimRight(baseURL, "/") + ev.Path
	}
	return p
}
