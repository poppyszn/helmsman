// Command fake-github stands in for GitHub in dev and tests.
// M0 serves only a health endpoint; OAuth, dispatch, runs, jobs and logs arrive in M1 and M3.
package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	log.Println("fake-github listening on :8090")
	log.Fatal(http.ListenAndServe(":8090", mux))
}
