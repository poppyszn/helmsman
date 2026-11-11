// Command server runs the Helmsman API and UI.
package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/poppyszn/helmsman/internal/actions"
	"github.com/poppyszn/helmsman/internal/agenthub"
	"github.com/poppyszn/helmsman/internal/api"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/catalog"
	"github.com/poppyszn/helmsman/internal/config"
	"github.com/poppyszn/helmsman/internal/gh"
	"github.com/poppyszn/helmsman/internal/health"
	"github.com/poppyszn/helmsman/internal/store"
	"github.com/poppyszn/helmsman/web"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	aw := &audit.Writer{Pool: st.Pool}
	authSvc := auth.NewService(cfg, st, aw)
	cat := catalog.NewManager(st.Pool, cfg.CatalogPath, aw, st.AgentExists)
	if err := cat.Reload(ctx, "startup", true); err != nil {
		// An invalid catalog never stops the server; the error is shown on the admin page.
		log.Error("catalog rejected", "err", err)
	}
	go cat.Watch(ctx, 30*time.Second)
	go authSvc.RunRefresh(ctx, 15*time.Minute)

	hub := agenthub.New(st, aw, log)
	ghAPI := gh.NewClient(cfg.GitHubAPIURL, githubTokens(cfg, log))
	actSvc := actions.New(st, ghAPI, aw, log)
	go actSvc.WatchRepos(ctx, 10*time.Minute)
	go actSvc.Run(ctx)
	checker := &health.Checker{St: st, Agents: hub, Log: log}
	go checker.Run(ctx)
	go prune(ctx, st, log)

	srv, err := api.New(api.Deps{Cfg: cfg, DB: st, Store: st, Audit: aw, Auth: authSvc, Catalog: cat, Hub: hub, Actions: actSvc, Logs: hub, UI: web.Dist()})
	if err != nil {
		return err
	}
	hs := &http.Server{Addr: cfg.Listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	log.Info("listening", "addr", cfg.Listen)
	if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// prune keeps 14 days of container events and health checks.
func prune(ctx context.Context, st *store.Store, log *slog.Logger) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		if err := st.Prune(ctx, time.Now().Add(-14*24*time.Hour)); err != nil && ctx.Err() == nil {
			log.Error("prune", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// githubTokens builds the App credential source. Without a configured App the portal still
// runs; actions are then disabled per repository with an explanation on the admin page.
func githubTokens(cfg config.Config, log *slog.Logger) gh.TokenSource {
	var key *rsa.PrivateKey
	switch {
	case cfg.GitHubMode == "fake":
		// fake-github does not verify signatures, so an ephemeral key is enough for dev.
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			log.Error("generate dev app key", "err", err)
			return unconfigured("could not generate a development App key")
		}
		key = k
	case cfg.GitHubAppID == "" || cfg.GitHubAppKeyPath == "":
		log.Warn("GitHub App not configured; migrations and seeds are disabled (set GITHUB_APP_ID and GITHUB_APP_PRIVATE_KEY_PATH)")
		return unconfigured("the GitHub App is not configured (set GITHUB_APP_ID and GITHUB_APP_PRIVATE_KEY_PATH)")
	default:
		b, err := os.ReadFile(cfg.GitHubAppKeyPath)
		if err != nil {
			log.Error("read GitHub App key", "err", err)
			return unconfigured("the GitHub App private key could not be read")
		}
		if key, err = gh.ParseKey(b); err != nil {
			log.Error("parse GitHub App key", "err", err)
			return unconfigured("the GitHub App private key is invalid")
		}
	}
	id := cfg.GitHubAppID
	if id == "" {
		id = "1"
	}
	return &gh.AppAuth{AppID: id, Key: key, APIURL: cfg.GitHubAPIURL}
}

type unconfigured string

func (u unconfigured) Token(context.Context, string) (string, error) {
	return "", errors.New(string(u))
}
func (unconfigured) Invalidate(string) {}
