// Command agent is the Helmsman app-server agent. M0 is a stub that idles;
// the real connection logic arrives in M2.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	log.Info("idp-agent stub started", "version", version)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Info("idp-agent stopping")
}
