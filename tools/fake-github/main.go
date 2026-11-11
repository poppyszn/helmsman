// Command fake-github stands in for GitHub in dev and tests. See internal/fakegithub.
package main

import (
	"log"
	"net/http"

	"github.com/poppyszn/helmsman/internal/fakegithub"
)

func main() {
	log.Println("fake-github listening on :8090")
	log.Fatal(http.ListenAndServe(":8090", fakegithub.New()))
}
