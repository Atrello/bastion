// The Bastion panel: serves the web app and its API, and the connections from each server's agent.
// Settings come from the environment (internal/panel/config); docs/install.md has the full list.
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Atrello/bastion/internal/panel/netdial"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	// `bastion healthcheck`, for the image's HEALTHCHECK: asks the panel running here if it works.
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

// healthcheck asks this machine's panel for /api/health: 0 if it works, 1 (and why) if not.
func healthcheck() int {
	addr := os.Getenv("BASTION_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get("http://" + addr + "/api/health")
	if err != nil {
		fmt.Fprintln(os.Stderr, "the panel isn't answering:", err)
		return 1
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		fmt.Fprintln(os.Stderr, strings.TrimSpace(string(body)))
		return 1
	}
	return 0
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg.Version = version
	netdial.Use(cfg.DNSFallback)
	db, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	panelAPI, err := api.New(ctx, cfg, db, log)
	if err != nil {
		return err
	}
	build := web.Build()
	withBuild := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Bastion-Build", build)
		panelAPI.ServeHTTP(w, r)
	})
	handler := server.New(withBuild, install.Handler(cfg.AgentBinaries), web.Handler(), cfg.Secure(), log)
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
