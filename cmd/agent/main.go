// Command agent is the Helmsman app-server agent.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/poppyszn/helmsman/internal/agent"
)

var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// Keep resident memory small on small hosts. The agent holds almost no state.
	debug.SetMemoryLimit(28 << 20)

	path := os.Getenv("IDP_AGENT_CONFIG")
	if path == "" {
		path = "/etc/idp-agent/agent.yaml"
	}
	cfg, err := agent.LoadConfig(path, os.Getenv)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	if len(cfg.Allow) == 0 {
		log.Warn("allow list is empty: the agent will report nothing and refuse every request")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Info("idp-agent starting", "version", version, "allowlist", cfg.Digest())
	_ = agent.New(cfg, version, log).Run(ctx)
	log.Info("idp-agent stopped")
}
