// Package server is the composition root of the long-running process: it
// migrates and seeds the database, wires the extraction engine, the Instagram
// client, the import worker and the HTTP API together, and serves until its
// context is cancelled.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sBurmester/recipe-reader/internal/api"
	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/instagram"
	"github.com/sBurmester/recipe-reader/internal/pipeline"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

// Run serves until ctx is cancelled, then drains in-flight requests and any
// running import before it returns. version is logged at startup and reported
// by GET /api/healthz.
//
// An error returns the same way: Run works on its own cancellable copy of ctx,
// so whatever it started is stopped and waited for before it returns. A caller
// that carries on after an error rather than exiting is left with nothing of
// Run's still running.
func Run(ctx context.Context, cfg config.Config, version string) error {
	// Run's own handle on cancellation: the caller's ctx still stops it, and a
	// failure below can stop it too, without reaching back into the caller's.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Before anything external: a misconfigured extraction mode is a startup
	// error, and reporting it after a database migration has already run buries
	// it under whatever that says instead.
	extractor, err := newExtractor(cfg)
	if err != nil {
		return err
	}

	pool, err := db.Open(ctx, cfg.Database.DSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	recipes := repository.NewRecipeRepository(pool)
	lookups := repository.NewLookupRepository(pool)

	// Instagram is optional: without an account configured the server still
	// serves the API over whatever is already in the database, and only the
	// import worker is withheld. A failed login no longer withholds it — see
	// newFetcher.
	fetcher, loginErr := newFetcher(ctx, cfg, instagram.NewClient(), recipes)

	var worker *pipeline.Worker
	if fetcher != nil {
		p := &pipeline.Pipeline{
			Fetcher: fetcher, Extractor: extractor,
			Recipes: recipes,
			// The same lookup repository the API serves the category picker
			// from, which is the point: an import may only attach a category
			// the picker already offers. Without it an attacker-authored
			// caption can talk the model into proposing any string and the
			// import creates that row for every user.
			Categories:       lookups,
			Threshold:        cfg.Extraction.Threshold,
			PublishThreshold: cfg.Extraction.PublishThreshold,
		}
		worker = pipeline.NewWorker(p, cfg.Import.Interval)
		if loginErr != nil {
			// Reported now rather than after the first scheduled run, which
			// is hours away: the import page shows it as the last error.
			// Wrapped in pipeline.ErrLogin so the status endpoint classifies it
			// as an authentication problem rather than as a generic run
			// failure — it is the one error that reaches Status without a run.
			worker.RecordFailure(fmt.Errorf("%w at startup; the next import retries it: %w", pipeline.ErrLogin, loginErr))
		}
	}

	srv, err := newHTTPServer(cfg, api.Deps{Recipes: recipes, Lookups: lookups, Worker: worker, Version: version})
	if err != nil {
		return err
	}

	// Started only now: a server that cannot be built is a startup failure, and
	// a schedule started before it would be work nobody is going to serve.
	if worker != nil {
		worker.Start(ctx)
	}

	// Shutdown runs on cancellation; Run waits for it to finish draining before
	// returning, so the deferred pool.Close above cannot pull the database out
	// from under a request that is still being served — or an import.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
		// Imports second, once no handler is left to trigger another. ctx is
		// already cancelled, so a run in flight stops at its next check;
		// waiting for it keeps the pool open until it has, and gets its outcome
		// logged instead of lost with the process. It shares the server's
		// budget rather than extending it.
		if worker != nil {
			if err := worker.Wait(shutdownCtx); err != nil {
				slog.Error("import did not stop within the shutdown budget; abandoning it", "error", err)
			}
		}
	}()

	slog.Info("recipe-reader listening", "addr", cfg.HTTP.Addr, "version", version)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		// The shutdown goroutine is waiting on ctx, and the schedule is running
		// under it. Cancelling is what releases both; waiting is what makes this
		// return mean "nothing of mine is still running".
		cancel()
		<-shutdownDone
		return err
	}
	<-shutdownDone
	return nil
}
