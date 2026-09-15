// Command recipe-reader is the composition root: it loads configuration,
// migrates and seeds the database, wires the extraction engine, Instagram
// client, import worker, and HTTP API together, and serves until signalled.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sBurmester/recipe-reader/internal/api"
	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/extraction"
	"github.com/sBurmester/recipe-reader/internal/instagram"
	"github.com/sBurmester/recipe-reader/internal/pipeline"
	"github.com/sBurmester/recipe-reader/internal/repository"
	"github.com/sBurmester/recipe-reader/internal/webui"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Before anything external: a misconfigured extraction mode is a startup
	// error, and reporting it after a database migration has already run buries
	// it under whatever that says instead.
	extractor, err := newExtractor(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := db.Migrate(cfg.DBDSN); err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DBDSN)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Seed(ctx, pool); err != nil {
		return err
	}

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
			Recipes:          recipes,
			Threshold:        cfg.ExtractionThreshold,
			PublishThreshold: cfg.ExtractionPublishThreshold,
		}
		worker = pipeline.NewWorker(p, cfg.ImportInterval)
		if loginErr != nil {
			// Reported now rather than after the first scheduled run, which
			// is hours away: the import page shows it as the last error.
			worker.RecordFailure(fmt.Errorf("instagram login failed at startup; the next import retries it: %w", loginErr))
		}
		worker.Start(ctx)
	}

	server, err := newServer(cfg, api.Deps{Recipes: recipes, Lookups: lookups, Worker: worker})
	if err != nil {
		return err
	}

	// Shutdown runs on signal; run() waits for it to finish draining before
	// returning, so the deferred pool.Close above cannot pull the database out
	// from under a request that is still being served — or an import.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
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

	slog.Info("recipe-reader listening", "addr", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-shutdownDone
	return nil
}

// newExtractor builds the extraction engine for cfg.ExtractionMode.
//
// Each mode now does what its name says. Before, only "hybrid" was ever
// inspected: EXTRACTION_MODE=llm fell through to exactly the same nil-LLM
// hybrid as EXTRACTION_MODE=rule, so asking for the LLM selected the *weakest*
// extractor — on every post of every run, with a paid API key sitting unused,
// and nothing in the logs to contradict the operator. The resulting
// needs_review badge pointed at the captions rather than at the configuration,
// which sent anyone investigating in the wrong direction. The project recorded
// this as an open question in its own Task 18 notes and shipped it anyway.
//
// Hence also the startup log line: the selected mode is now stated, which is
// the cheapest possible contradiction of a wrong assumption.
func newExtractor(cfg config.Config) (extraction.Extractor, error) {
	rules := extraction.NewRuleBasedExtractor()
	settings, hasAPIKey := cfg.LLMSettings()

	newLLM := func() (*extraction.LLMExtractor, error) {
		return extraction.NewLLMExtractor(extraction.LLMConfig{
			Provider: extraction.LLMProvider(settings.Provider),
			APIKey:   settings.APIKey,
			Model:    settings.Model,
			BaseURL:  settings.BaseURL,
			Timeout:  settings.Timeout,
		})
	}

	switch cfg.ExtractionMode {
	case "rule":
		slog.Info("extraction mode selected", "mode", "rule", "llm", "disabled")
		return rules, nil

	case "llm":
		// Fail rather than degrade quietly. An operator who asked for the LLM
		// and configured no key has a broken deployment, not a rules-only one,
		// and saying so at boot costs one run instead of a month of weak
		// extractions nobody attributes to the configuration.
		if !hasAPIKey {
			return nil, fmt.Errorf(
				"extraction mode %q needs an API key: set LLM_API_KEY (or ANTHROPIC_API_KEY for the anthropic provider)",
				cfg.ExtractionMode)
		}
		llm, err := newLLM()
		if err != nil {
			return nil, err
		}
		slog.Info("extraction mode selected", "mode", "llm", "provider", settings.Provider, "model", settings.Model)
		return llm, nil

	case "hybrid":
		if !hasAPIKey {
			// The one degraded mode that is correct to enter without failing:
			// "hybrid" asks for the LLM only where the rules fall short, so
			// rules-only is a coherent answer. It is still said out loud.
			slog.Warn("extraction mode selected", "mode", "hybrid", "llm", "disabled",
				"reason", "no API key configured; running rules-only")
			return extraction.NewHybridExtractor(rules, nil, cfg.ExtractionThreshold), nil
		}
		llm, err := newLLM()
		if err != nil {
			return nil, err
		}
		slog.Info("extraction mode selected", "mode", "hybrid", "provider", settings.Provider,
			"model", settings.Model, "fallback_threshold", cfg.ExtractionThreshold)
		return extraction.NewHybridExtractor(rules, llm, cfg.ExtractionThreshold), nil

	default:
		// Unreachable through config.Load — the kong enum rejects it first —
		// but newExtractor is also called with hand-built values in tests, and
		// a silent fallthrough here is the defect this function exists to fix.
		return nil, fmt.Errorf("unknown extraction mode %q (want rule, llm or hybrid)", cfg.ExtractionMode)
	}
}

// Server timeouts. Without them a client that trickles a request in a byte at a
// time holds its connection — and a goroutine — for as long as it cares to, and
// enough of them exhaust the process. None of these bounds constrains a
// legitimate request: every handler answers from a handful of database round
// trips, and POST /api/import/run returns 202 before the import begins.
const (
	// readHeaderTimeout is the slowloris bound: the request line and headers
	// must arrive within it.
	readHeaderTimeout = 10 * time.Second
	// readTimeout bounds the whole request, headers and body together. Recipe
	// bodies are capped at 1 MiB in the api package, which arrives in well under
	// this on any link that can use the UI at all.
	readTimeout = 30 * time.Second
	// writeTimeout starts counting when the headers have been read, not when
	// the handler starts writing, so it covers the body read as well. Set below
	// readTimeout, a slow but legal upload would be cut off mid-response.
	writeTimeout = 60 * time.Second
	// idleTimeout closes keep-alive connections nobody is using. Left at zero
	// it silently inherits readTimeout; it is set on its own so the keep-alive
	// window is a decision rather than a side effect of the request bound.
	idleTimeout = 120 * time.Second
)

// newServer assembles the HTTP server: the API router under /api/ and the
// embedded frontend beneath it.
//
// It is separated from run() so the wiring can be exercised without a database,
// a signal handler or a listening socket — run() itself has no seam at all, and
// the composition root is where a mis-wired route or a missing middleware would
// otherwise go unnoticed until someone opened the page.
func newServer(cfg config.Config, deps api.Deps) (*http.Server, error) {
	// An unauthenticated deployment is reachable only from loopback —
	// config.Load refuses any other bind without a token — but it is still
	// worth naming at boot, because "it works without one" is how it stays
	// that way when the address later changes.
	if cfg.APIToken == "" {
		slog.Warn("no API_TOKEN configured; writes are unauthenticated and the server is bound to loopback only", "addr", cfg.HTTPAddr)
	}
	apiRouter := api.NewRouter(deps, api.Security{Token: cfg.APIToken, AllowedOrigins: cfg.CORSOrigins})

	frontend, err := webui.Handler()
	if err != nil {
		return nil, err
	}
	if webui.IsPlaceholder() {
		slog.Warn("serving the placeholder frontend, not a real build — run `make frontend` (or `make build`, which now depends on it) and rebuild")
	}

	// The embedded frontend takes everything the API does not claim. Go 1.22
	// ServeMux prefers the more specific "/api/" pattern, and does not strip
	// it, so the API router still sees the full path it registered.
	mux := http.NewServeMux()
	mux.Handle("/api/", apiRouter)
	mux.Handle("/", frontend)

	return &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}, nil
}

// alreadyImported adapts the recipe repository to instagram.FetchOptions.Known,
// which is what lets the fetcher page past posts it has already imported and
// reach the backlog behind them.
//
// A lookup failure answers "not known" rather than aborting the fetch: the
// pipeline re-checks authoritatively before writing, so the cost of being
// wrong here is one redundant extraction, where the cost of failing the run is
// the whole import. The error is logged, because a repository that is failing
// every lookup would otherwise show up only as an import that quietly does
// more work than it needs to.
func alreadyImported(recipes repository.RecipeRepository) func(context.Context, string) bool {
	return func(ctx context.Context, source string) bool {
		_, err := recipes.GetBySource(ctx, source)
		if err == nil {
			return true
		}
		if !errors.Is(err, repository.ErrNotFound) {
			slog.Warn("import: dedupe lookup failed; treating post as new", "source", source, "error", err)
		}
		return false
	}
}
