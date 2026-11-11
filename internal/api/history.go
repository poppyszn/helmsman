package api

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/loki"
)

// Log history comes from Loki, filled by Alloy (container logs) and Alloy's Faro receiver (browser
// logs, errors and events). Helmsman never passes LogQL through: it builds the stream selector from the
// catalog, adds label matchers from fixed lists, and escapes the one free text filter.

const (
	defaultHistoryLimit = 200
	maxHistoryLimit     = 1000
	maxSearchLength     = 200
)

var (
	historyRanges = map[string]time.Duration{"15m": 15 * time.Minute, "1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}
	// kinds each source can produce. nginx access and error lines are container kinds; Faro adds
	// exception, event and measurement. "app" is plain application output in both.
	containerKinds = []string{"access", "error", "app"}
	browserKinds   = []string{"app", "exception", "event", "measurement"}
	allLevels      = []string{"error", "warn", "info", "debug"}
)

type historyEntry struct {
	TS      time.Time `json:"ts"`
	Source  string    `json:"source"` // container | browser
	Kind    string    `json:"kind"`
	Level   string    `json:"level"`
	Text    string    `json:"text"`
	Session string    `json:"session,omitempty"`
	Page    string    `json:"page,omitempty"`
	Version string    `json:"version,omitempty"`
}

type historyResponse struct {
	Entries    []historyEntry `json:"entries"`
	Sources    historySources `json:"sources"`
	Kinds      historyKinds   `json:"kinds"`
	Truncated  bool           `json:"truncated"`
	NextBefore *time.Time     `json:"next_before"`
	Retention  string         `json:"retention"`
}

type historySources struct {
	Container bool `json:"container"`
	Browser   bool `json:"browser"`
}

type historyKinds struct {
	Container []string `json:"container"`
	Browser   []string `json:"browser"`
}

func csv(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// subset keeps want only if every value is allowed. ok is false when a value is not in the list.
func subset(want, allowed []string) (kept []string, ok bool) {
	for _, w := range want {
		found := false
		for _, a := range allowed {
			if w == a {
				found = true
				break
			}
		}
		if !found {
			return nil, false
		}
		kept = append(kept, w)
	}
	return kept, true
}

func intersect(a, b []string) []string {
	var out []string
	for _, x := range a {
		for _, y := range b {
			if x == y {
				out = append(out, x)
			}
		}
	}
	return out
}

func matcher(label string, values []string) string {
	if len(values) == 0 {
		return ""
	}
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = regexp.QuoteMeta(v)
	}
	return "," + label + "=~" + loki.Quote(strings.Join(quoted, "|"))
}

// containerQuery selects one environment's container logs. host is the agent id: Alloy sets it from
// HOST_NAME so the same compose project on two hosts (staging and prod) never mixes.
func containerQuery(host, project, service string, kinds, levels []string, text string) string {
	q := "{source=\"container\",host=" + loki.Quote(host) + ",compose_project=" + loki.Quote(project) + ",compose_service=" + loki.Quote(service) +
		matcher("kind", kinds) + matcher("level", levels) + "}"
	return q + textFilter(text)
}

// browserQuery selects the Faro logs of one environment. The Faro app's environment must equal the
// catalog env name.
func browserQuery(faroApp, env string, kinds, levels []string, text string) string {
	q := "{source=\"browser\",app=" + loki.Quote(faroApp) + ",env=" + loki.Quote(env) +
		matcher("kind", kinds) + matcher("level", levels) + "}"
	return q + textFilter(text)
}

func textFilter(text string) string {
	if text == "" {
		return ""
	}
	return " |~ " + loki.Quote("(?i)"+regexp.QuoteMeta(text))
}

func (s *Server) handleLogHistory(w http.ResponseWriter, r *http.Request) {
	if s.Loki == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "log history is not set up on this server", "code": "history_disabled"})
		return
	}
	app, env := chi.URLParam(r, "app"), chi.URLParam(r, "env")
	e, err := s.Store.EnvByName(r.Context(), app, env)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	spec := parseSpec(e.Spec)
	q := r.URL.Query()

	bad := func(msg string) { writeErr(w, http.StatusBadRequest, msg) }
	kindsAsked := csv(q.Get("kind"))
	if _, ok := subset(kindsAsked, append(append([]string{}, containerKinds...), browserKinds...)); !ok {
		bad("unknown kind")
		return
	}
	levels, ok := subset(csv(q.Get("level")), allLevels)
	if !ok {
		bad("unknown level")
		return
	}
	sources := csv(q.Get("source"))
	if _, ok := subset(sources, []string{"container", "browser"}); !ok {
		bad("unknown source")
		return
	}
	span := historyRanges[q.Get("range")]
	if q.Get("range") == "" {
		span = time.Hour
	} else if span == 0 {
		bad("range must be one of 15m, 1h, 6h, 24h, 7d")
		return
	}
	limit := defaultHistoryLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			bad("invalid limit")
			return
		}
		limit = min(n, maxHistoryLimit)
	}
	text := strings.TrimSpace(q.Get("q"))
	if len(text) > maxSearchLength {
		bad("search text is too long")
		return
	}
	end := time.Now()
	if v := q.Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			bad("invalid before")
			return
		}
		end = t
	}
	start := time.Now().Add(-span)

	hasBrowser := spec.Logs.FaroApp != ""
	wantContainer := len(sources) == 0 || contains(sources, "container")
	wantBrowser := hasBrowser && (len(sources) == 0 || contains(sources, "browser"))
	cKinds, bKinds := intersect(kindsAsked, containerKinds), intersect(kindsAsked, browserKinds)
	// Asking only for kinds a source cannot produce leaves that source out.
	if len(kindsAsked) > 0 {
		wantContainer = wantContainer && len(cKinds) > 0
		wantBrowser = wantBrowser && len(bKinds) > 0
	}

	c := rc(r)
	if err := s.Audit.Write(r.Context(), audit.Event{ActorType: audit.ActorUser, ActorID: c.User.Login, Action: "logs.history",
		Target: app + "/" + env, Result: "ok", IP: auth.ClientIP(r), Details: map[string]any{
			"tier": e.Tier, "range": q.Get("range"), "sources": sources, "kinds": kindsAsked, "levels": levels, "text_filtered": text != ""}}); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not write the audit log")
		return
	}

	type result struct {
		source  string
		entries []loki.Entry
		err     error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	run := func(source, query string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			es, err := s.Loki.QueryRange(ctx, query, start, end, limit+1)
			results <- result{source, es, err}
		}()
	}
	if wantContainer {
		run("container", containerQuery(e.Agent, spec.Container.ComposeProject, spec.Container.Service, cKinds, levels, text))
	}
	if wantBrowser {
		run("browser", browserQuery(spec.Logs.FaroApp, env, bKinds, levels, text))
	}
	wg.Wait()
	close(results)

	var merged []historyEntry
	seenBrowser := map[string]bool{}
	for res := range results {
		if res.err != nil {
			s.logHistoryError(app, env, res.err)
			writeErr(w, http.StatusBadGateway, "log history is unavailable right now")
			return
		}
		for _, en := range res.entries {
			// The Faro SDK sometimes delivers a batch twice, which Loki stores as two identical lines.
			// A browser line carries the SDK's own timestamp, session and page, so an identical line is the
			// same report. Container lines are never merged: a program may genuinely print the same line twice.
			if res.source == "browser" {
				if seenBrowser[en.Line] {
					continue
				}
				seenBrowser[en.Line] = true
			}
			merged = append(merged, toHistoryEntry(res.source, en))
		}
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].TS.After(merged[j].TS) })
	resp := historyResponse{Entries: merged, Retention: "14 days",
		Sources: historySources{Container: true, Browser: hasBrowser},
		Kinds:   historyKinds{Container: containerKinds, Browser: browserKinds}}
	if resp.Entries == nil {
		resp.Entries = []historyEntry{}
	}
	if len(resp.Entries) > limit {
		resp.Entries = resp.Entries[:limit]
		resp.Truncated = true
		t := resp.Entries[len(resp.Entries)-1].TS.Add(-time.Nanosecond)
		resp.NextBefore = &t
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) logHistoryError(app, env string, err error) {
	// The error text is Loki's own and carries no log content.
	if s.Log != nil {
		s.Log.Error("log history query failed", "app", app, "env", env, "err", err)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func toHistoryEntry(source string, e loki.Entry) historyEntry {
	h := historyEntry{TS: e.TS, Source: source, Kind: e.Labels["kind"], Level: e.Labels["level"], Text: e.Line}
	if h.Kind == "" {
		h.Kind = "app"
	}
	if h.Level == "" {
		h.Level = "info"
	}
	if source == "browser" {
		f := parseLogfmt(e.Line)
		h.Text = renderBrowser(h.Kind, f)
		h.Session, h.Page, h.Version = f["session_id"], f["page_url"], f["app_version"]
	}
	return h
}

// parseLogfmt reads key=value pairs where a value may be double quoted with backslash escapes.
func parseLogfmt(line string) map[string]string {
	out := map[string]string{}
	i := 0
	for i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		j := i
		for j < len(line) && line[j] != '=' && line[j] != ' ' {
			j++
		}
		if j >= len(line) || line[j] != '=' {
			i = j + 1
			continue
		}
		key := line[i:j]
		i = j + 1
		var val string
		if i < len(line) && line[i] == '"' {
			i++
			var b strings.Builder
			for i < len(line) && line[i] != '"' {
				if line[i] == '\\' && i+1 < len(line) {
					i++
					switch line[i] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte(line[i])
					}
				} else {
					b.WriteByte(line[i])
				}
				i++
			}
			i++ // closing quote
			val = b.String()
		} else {
			k := i
			for i < len(line) && line[i] != ' ' {
				i++
			}
			val = line[k:i]
		}
		out[key] = val
	}
	return out
}

func prefixed(f map[string]string, prefix string) string {
	var keys []string
	for k := range f {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = strings.TrimPrefix(k, prefix) + "=" + f[k]
	}
	return strings.Join(parts, " ")
}

// renderBrowser turns a Faro logfmt line into something a person reads.
func renderBrowser(kind string, f map[string]string) string {
	var text string
	switch kind {
	case "exception":
		text = strings.TrimSpace(f["type"] + ": " + f["value"])
		if st := f["stacktrace"]; st != "" {
			if lines := strings.Split(st, "\n"); len(lines) > 1 {
				text += "\n" + strings.Join(lines[1:], "\n")
			}
		}
	case "event":
		text = f["event_name"]
		if a := prefixed(f, "event_data_"); a != "" {
			text += "  " + a
		}
	case "measurement":
		text = f["type"]
		if v := prefixed(f, "value_"); v != "" {
			text += "  " + v
		}
	default:
		text = f["message"]
		if c := prefixed(f, "context_"); c != "" {
			text += "  " + c
		}
	}
	if text == "" {
		return "(empty)"
	}
	return text
}
