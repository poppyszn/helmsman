// Command fake-github stands in for GitHub in dev and tests. See internal/fakegithub.
package main

import (
	"log"
	"net/http"

	"github.com/poppyszn/helmsman/internal/fakegithub"
)

func main() {
	f := fakegithub.New()
	// Realistic pacing for the dev stack: the run shows up a few seconds after dispatch,
	// waits in the queue, then runs. Change it live with PUT /_fake/scenario.
	f.SetScenario(fakegithub.Scenario{AppearMS: 4000, QueuedMS: 2000, RunMS: 10000})
	log.Println("fake-github listening on :8090")
	log.Fatal(http.ListenAndServe(":8090", f))
}
