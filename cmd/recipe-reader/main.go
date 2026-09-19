// Command recipe-reader loads its configuration and hands over to the server —
// or, with --health-check, probes one that is already running.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/healthcheck"
	"github.com/sBurmester/recipe-reader/internal/server"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(version)
	if err != nil {
		return err
	}
	// A probe of another process, not a server: it touches no database and
	// starts nothing. See healthcheck.Probe.
	if cfg.HealthCheck {
		return healthcheck.Probe(context.Background(), cfg.HTTPAddr)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return server.Run(ctx, cfg, version)
}
