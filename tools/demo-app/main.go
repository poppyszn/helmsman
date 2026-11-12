// Command demo-app is a tiny service for exercising Helmsman locally.
// POST /fail-health toggles health failures, POST /crash exits the process, POST /log-samples writes one
// line of each common log format (JSON, key=value, plain with a level, a stack trace, an nginx access line).
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

func main() {
	name := os.Getenv("APP_NAME")
	if name == "" {
		name = "demo-app"
	}
	var failing atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		if failing.Load() {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("POST /fail-health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "failing=%v\n", !failing.Load())
		failing.Store(!failing.Load())
	})
	mux.HandleFunc("POST /log-samples", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Println(`{"level":"error","msg":"payment gateway timeout","order":1042,"ms":5003}`)
		fmt.Println(`level=warn msg="cache miss storm" keys=340`)
		fmt.Println(`2026/11/12 10:00:00 ERROR could not reach the mailer`)
		fmt.Println(`    at handler (/app/server.js:10:5)`)
		fmt.Println(`10.0.0.5 - - [12/Nov/2026:10:00:00 +0000] "GET /missing HTTP/1.1" 404 9 "-" "curl/8"`)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("POST /crash", func(http.ResponseWriter, *http.Request) {
		log.Println("crash requested")
		os.Exit(1)
	})
	go func() {
		for i := 0; ; i++ {
			log.Printf("%s heartbeat %d", name, i)
			time.Sleep(time.Second)
		}
	}()
	log.Fatal(http.ListenAndServe(":8080", mux))
}
