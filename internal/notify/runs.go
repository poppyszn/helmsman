package notify

import (
	"strings"

	"github.com/poppyszn/helmsman/internal/store"
)

// RunEvent turns a migration or seed run into an event. kind is what the run tracker reported:
// "pending_approval", "started", "failed" or "succeeded".
func RunEvent(kind string, r store.ActionRun) (Event, bool) {
	typ := map[string]string{"pending_approval": RunPendingApproval, "started": RunStarted, "failed": RunFailed, "succeeded": RunSucceeded}[kind]
	if typ == "" {
		return Event{}, false
	}
	what := map[string]string{"migrate": "Migration", "seed": "Seed"}[r.Action]
	if what == "" {
		what = strings.ToUpper(r.Action[:1]) + r.Action[1:]
	}
	where := r.App + " / " + r.Env
	ev := Event{Type: typ, App: r.App, Env: r.Env, Tier: r.Tier, Path: "/apps/" + r.App, Key: typ + ":run:" + r.ID,
		Details: map[string]string{"command": r.Action, "requested by": r.RequestedByLogin, "environment": where}}
	switch kind {
	case "pending_approval":
		ev.Title = what + " on " + where + " is waiting for approval"
		ev.Message = r.RequestedByLogin + " asked to run a " + strings.ToLower(what) + " on a production environment. A second person with the approve permission must approve it."
		ev.Path = "/approvals"
	case "started":
		ev.Title = what + " started on " + where
		ev.Message = "Requested by " + r.RequestedByLogin + "."
	case "failed":
		ev.Title = what + " failed on " + where
		if r.Error != nil {
			ev.Message = *r.Error
		}
	case "succeeded":
		ev.Title = what + " succeeded on " + where
	}
	if r.GHRunURL != nil {
		ev.Details["github run"] = *r.GHRunURL
	}
	return ev, true
}
