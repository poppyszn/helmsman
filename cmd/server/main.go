// Command server runs the Helmsman API and UI.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/poppyszn/helmsman/internal/api"
	"github.com/poppyszn/helmsman/internal/audit"
	"github.com/poppyszn/helmsman/internal/auth"
	"github.com/poppyszn/helmsman/internal/catalog"
	"github.com/poppyszn/helmsman/internal/config"
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

	srv, err := api.New(api.Deps{Cfg: cfg, DB: st, Store: st, Audit: aw, Auth: authSvc, Catalog: cat, UI: web.Dist()})
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
