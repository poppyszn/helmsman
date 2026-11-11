// Command demo-app is a tiny service for exercising Helmsman locally.
// POST /fail-health toggles health failures, POST /crash exits the process.
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
