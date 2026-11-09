// The Bastion panel: serves the web app and its API, and the connections from each server's agent.
// Settings come from the environment (internal/panel/config); docs/install.md has the full list.
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

	"github.com/Atrello/bastion/internal/panel/api"
	"github.com/Atrello/bastion/internal/panel/config"
	"github.com/Atrello/bastion/internal/panel/install"
	"github.com/Atrello/bastion/internal/panel/server"
	"github.com/Atrello/bastion/internal/panel/store"
	"github.com/Atrello/bastion/web"
)

// version is set when building: go build -ldflags "-X main.version=0.1.0".
var version = "dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	panelAPI, err := api.New(ctx, cfg, db, log)
	if err != nil {
		return err
	}
	handler := server.New(panelAPI, install.Handler(cfg.AgentBinaries), web.Handler(), cfg.Secure(), log)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Info("Bastion is running", "version", version, "address", cfg.Addr, "public", cfg.PublicURL.String())
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
