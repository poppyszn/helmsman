package health

import (
	"testing"
	"time"

	"github.com/poppyszn/helmsman/internal/store"
)

func probes(now time.Time, statuses ...string) []store.HealthRow {
	var out []store.HealthRow
	for i, s := range statuses {
		out = append(out, store.HealthRow{Status: s, CheckedAt: now.Add(-time.Duration(i) * 30 * time.Second)})
	}
	return out
}

func TestDerive(t *testing.T) {
	now := time.Now()
	run := Input{Now: now, AgentOnline: true, DockerOK: true, HasContainer: true, State: "running"}
	with := func(f func(*Input)) Input { in := run; f(&in); return in }
	cases := []struct {
		name string
		in   Input
		want string
	}{
		{"running and probing ok", with(func(i *Input) { i.Probes = probes(now, ProbeOK) }), Healthy},
		{"running, no health url", run, Healthy},
		{"one failed probe is not down (flapping)", with(func(i *Input) { i.Probes = probes(now, ProbeFail, ProbeOK) }), Healthy},
		{"two consecutive failures are down", with(func(i *Input) { i.Probes = probes(now, ProbeFail, ProbeFail) }), Down},
		{"slow answer is degraded", with(func(i *Input) { i.Probes = probes(now, ProbeSlow, ProbeOK) }), Degraded},
		{"docker reports unhealthy is degraded", with(func(i *Input) { i.DockerHealth = "unhealthy" }), Degraded},
		{"stopped container is down", with(func(i *Input) { i.State = "exited" }), Down},
		{"restarting container is down", with(func(i *Input) { i.State = "restarting" }), Down},
		{"agent offline, nothing fresh", Input{Now: now, HasContainer: true, State: "running"}, Unknown},
		{"agent offline but server probe is fresh", Input{Now: now, Probes: probes(now, ProbeOK)}, Healthy},
		{"agent offline, probe is stale", Input{Now: now, Probes: []store.HealthRow{{Status: ProbeOK, CheckedAt: now.Add(-10 * time.Minute)}}}, Unknown},
		{"docker unavailable, nothing fresh", Input{Now: now, AgentOnline: true, HasContainer: true, State: "running"}, Unknown},
		{"no container and no probe", Input{Now: now, AgentOnline: true, DockerOK: true}, Unknown},
	}
	for _, c := range cases {
		if got := Derive(c.in); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	if Classify(200, 100*time.Millisecond, "") != ProbeOK || Classify(204, 3*time.Second, "") != ProbeSlow ||
		Classify(500, time.Millisecond, "") != ProbeFail || Classify(0, 0, "request failed") != ProbeFail || Classify(302, time.Millisecond, "") != ProbeOK {
		t.Fatal("classification")
	}
}
