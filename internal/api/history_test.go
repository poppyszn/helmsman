package api

import (
	"strings"
	"testing"
)

func TestQueriesAreBuiltFromCatalogValuesAndEscaped(t *testing.T) {
	got := containerQuery("agent-1", "billing", "api", []string{"access", "error"}, []string{"warn", "error"}, "")
	want := `{source="container",host="agent-1",compose_project="billing",compose_service="api",kind=~"access|error",level=~"warn|error"}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if b := browserQuery("billing-web", "staging", []string{"exception"}, nil, ""); b != `{source="browser",app="billing-web",env="staging",kind=~"exception"}` {
		t.Fatalf("browser: %s", b)
	}
	// A name that tries to break out of the selector stays inside its quotes.
	evil := containerQuery(`a",compose_service=~".+`, "p", "s", nil, nil, "")
	if strings.Contains(evil, `",compose_service=~".+"`) || !strings.Contains(evil, `host="a\",compose_service=~\".+"`) {
		t.Fatalf("selector was not escaped: %s", evil)
	}
}

func TestSearchTextIsAnEscapedCaseInsensitiveLiteral(t *testing.T) {
	q := containerQuery("h", "p", "s", nil, nil, `a.b"|c`+"\n")
	if !strings.HasSuffix(q, ` |~ "(?i)a\\.b\"\\|c\n"`) {
		t.Fatalf("got %s", q)
	}
	if textFilter("") != "" {
		t.Fatal("no text, no filter")
	}
}

func TestParseLogfmtHandlesQuotesAndEscapes(t *testing.T) {
	f := parseLogfmt(`kind=log message="say \"hi\" now" level=info empty="" url=http://x/y?a=b`)
	if f["message"] != `say "hi" now` || f["level"] != "info" || f["url"] != "http://x/y?a=b" || f["kind"] != "log" {
		t.Fatalf("%v", f)
	}
}

func TestBrowserLinesAreRenderedForPeople(t *testing.T) {
	cases := []struct {
		kind, line, want string
	}{
		{"app", `kind=log message="payment failed" context_cart=3 context_user=u1`, "payment failed  cart=3 user=u1"},
		{"exception", `kind=exception type=TypeError value="x is undefined" stacktrace="TypeError: x is undefined\n  at pay (app.js:10:5)"`, "TypeError: x is undefined\n  at pay (app.js:10:5)"},
		{"event", `kind=event event_name=button_click event_data_id=pay`, "button_click  id=pay"},
		{"measurement", `kind=measurement type=web-vitals value_lcp=1234.5 value_cls=0.1`, "web-vitals  cls=0.1 lcp=1234.5"},
		{"app", `kind=log`, "(empty)"},
	}
	for _, c := range cases {
		if got := renderBrowser(c.kind, parseLogfmt(c.line)); got != c.want {
			t.Errorf("%s: got %q want %q", c.kind, got, c.want)
		}
	}
}
