package actions

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/poppyszn/helmsman/internal/gh"
	"github.com/poppyszn/helmsman/internal/store"
)

// Run polls active runs until ctx ends. State lives in the database (next_poll_at), so
// a restart resumes every run that was in flight.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(s.Tick)
	defer t.Stop()
	for {
		s.Poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Poll advances every run that is due once. Exported so tests can drive it.
func (s *Service) Poll(ctx context.Context) {
	if s.blocked() > 0 {
		return // GitHub told us to back off
	}
	runs, err := s.Store.DuePolls(ctx, s.now(), 25)
	if err != nil {
		if ctx.Err() == nil {
			s.Log.Error("poll runs", "err", err)
		}
		return
	}
	for _, r := range runs {
		if ctx.Err() != nil || s.blocked() > 0 {
			return
		}
		s.advance(ctx, r)
	}
}

func (s *Service) advance(ctx context.Context, r store.ActionRun) {
	if r.DispatchedAt == nil {
		s.dispatch(ctx, r.ID) // dispatch was never attempted (approver or server went away)
		return
	}
	var err error
	if r.GHRunID == nil {
		err = s.correlate(ctx, r)
	} else {
		err = s.refresh(ctx, r)
	}
	if err == nil {
		return
	}
	var rl *gh.RateLimitError
	if errors.As(err, &rl) {
		s.block(rl.RetryAfter)
		_ = s.Store.SetNextPoll(ctx, r.ID, s.now().Add(rl.RetryAfter))
		s.Log.Warn("github rate limited; polling paused", "retry_after", rl.RetryAfter)
		return
	}
	s.Log.Warn("poll failed", "run", r.ID, "err", err)
	if errors.Is(err, gh.ErrNoInstallation) {
		s.setProblem(r.Repo, "the GitHub App is not installed on this repository")
	}
	_ = s.Store.SetNextPoll(ctx, r.ID, s.now().Add(15*time.Second))
}

// correlate finds the workflow run GitHub created for our dispatch. GitHub answers a
// dispatch with 204 and no run id, so the run is matched by the correlation id that the
// workflow puts in its run-name.
func (s *Service) correlate(ctx context.Context, r store.ActionRun) error {
	runs, err := s.GH.ListRuns(ctx, r.Repo, r.Workflow, 30, 0)
	if err != nil {
		return err
	}
	for _, gr := range runs {
		if containsID(gr.Title, r.CorrelationID) || containsID(gr.Name, r.CorrelationID) {
			id := gr.ID
			r.GHRunID = &id
			url := gr.HTMLURL
			r.GHRunURL = &url
			// Link the run first. A run that is already over is finished by refresh below, which
			// also captures logs and audits, so it must not be written as final here.
			linked := mapStatus(gr)
			if linked == "succeeded" || linked == "failed" || linked == "cancelled" {
				linked = "in_progress"
			}
			if err := s.Store.SetProgress(ctx, r.ID, store.Progress{Status: linked, GHRunID: &id, GHRunURL: &url, NextPoll: s.now()}); err != nil {
				return err
			}
			return s.refresh(ctx, r)
		}
	}
	if s.now().Sub(*r.DispatchedAt) >= s.CorrelateFor {
		msg := "GitHub did not report a run for this dispatch within " + s.CorrelateFor.Round(time.Second).String() + ". It may still start; check the workflow's run list."
		_ = s.Store.MarkUnknown(ctx, r.ID, msg)
		s.auditSystem(ctx, "action.run.completed", r, "unknown", map[string]any{"reason": "no run correlated"})
		return nil
	}
	return s.Store.SetNextPoll(ctx, r.ID, s.now().Add(s.CorrelateEvery))
}

func containsID(hay, id string) bool {
	return id != "" && len(hay) >= len(id) && (hay == id || indexOf(hay, id) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// mapStatus converts a GitHub run to one of our statuses.
func mapStatus(gr gh.Run) string {
	switch gr.Status {
	case "completed":
		switch gr.Conclusion {
		case "success", "neutral", "skipped":
			return "succeeded"
		case "cancelled":
			return "cancelled"
		}
		return "failed"
	case "in_progress":
		return "in_progress"
	case "waiting":
		return "waiting"
	}
	return "queued"
}

func (s *Service) refresh(ctx context.Context, r store.ActionRun) error {
	gr, err := s.GH.GetRun(ctx, r.Repo, *r.GHRunID)
	if err != nil {
		return err
	}
	jobs, err := s.GH.ListJobs(ctx, r.Repo, gr.ID)
	if err != nil {
		return err
	}
	jb, _ := json.Marshal(jobs)
	st := mapStatus(gr)
	url := gr.HTMLURL
	p := store.Progress{Status: st, GHRunID: &gr.ID, GHRunURL: &url, Jobs: jb, NextPoll: s.now().Add(s.PollEvery)}
	final := st == "succeeded" || st == "failed" || st == "cancelled"
	if final {
		p.Finished = true
		if gr.Conclusion != "" {
			c := gr.Conclusion
			p.Conclusion = &c
		}
		if st == "failed" {
			msg := "The workflow finished with conclusion " + gr.Conclusion + "."
			p.Error = &msg
		}
		s.captureLogs(ctx, r, jobs)
	}
	if err := s.Store.SetProgress(ctx, r.ID, p); err != nil {
		return err
	}
	if final {
		res := "ok"
		if st != "succeeded" {
			res = st
		}
		s.auditSystem(ctx, "action.run.completed", r, res, map[string]any{"status": st, "conclusion": gr.Conclusion, "gh_run_id": gr.ID})
	}
	return nil
}

// captureLogs stores each job's log, newest lines kept, within the per-run cap.
func (s *Service) captureLogs(ctx context.Context, r store.ActionRun, jobs []gh.Job) {
	budget := s.LogCap
	var out []store.RunLog
	for _, j := range jobs {
		if budget <= 0 {
			out = append(out, store.RunLog{JobID: j.ID, JobName: j.Name, Content: "", Truncated: true})
			continue
		}
		text, cut, err := s.GH.JobLogs(ctx, r.Repo, j.ID, budget)
		if err != nil {
			s.Log.Warn("job logs unavailable", "run", r.ID, "job", j.ID, "err", err)
			continue
		}
		budget -= len(text)
		out = append(out, store.RunLog{JobID: j.ID, JobName: j.Name, Content: text, Truncated: cut})
	}
	if err := s.Store.SaveLogs(ctx, r.ID, out); err != nil {
		s.Log.Error("save run logs", "run", r.ID, "err", err)
	}
}
