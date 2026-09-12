// Command recipe-reader is the composition root: it loads configuration,
// migrates and seeds the database, wires the extraction engine, Instagram
// client, import worker, and HTTP API together, and serves until signalled.
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

	// A nil llm leaves the hybrid extractor rules-only, which is the correct
	// degraded mode when no API key is configured rather than a failure.
	rules := extraction.NewRuleBasedExtractor()
	var llm extraction.Extractor
	if cfg.ExtractionMode == "hybrid" && cfg.AnthropicAPIKey != "" {
		llm = extraction.NewLLMExtractor(cfg.AnthropicAPIKey, cfg.AnthropicModel)
	}
	extractor := extraction.NewHybridExtractor(rules, llm, cfg.ExtractionThreshold)

	// Instagram is optional: without credentials, or when login fails, the
	// server still serves the API over whatever is already in the database —
	// only the import worker is withheld.
	igClient := instagram.NewClient()
	var fetcher pipeline.PostFetcher
	if cfg.InstagramUsername != "" {
		if err := igClient.LoginOrRestore(ctx, cfg.InstagramUsername, cfg.InstagramPassword, cfg.InstagramSessionPath); err != nil {
			slog.Warn("instagram login failed; import worker will not run", "error", err)
		} else {
			fetcher = &instagram.PipelineFetcher{
				Client:         igClient,
				CollectionName: cfg.InstagramCollection,
				Options:        instagram.FetchOptions{Known: alreadyImported(recipes)},
			}
		}
	}

	var worker *pipeline.Worker
	if fetcher != nil {
		p := &pipeline.Pipeline{
			Fetcher: fetcher, Extractor: extractor,
			Recipes: recipes, Lookups: lookups,
			Threshold:        cfg.ExtractionThreshold,
			PublishThreshold: cfg.ExtractionPublishThreshold,
		}
		worker = pipeline.NewWorker(p, cfg.ImportInterval)
		worker.Start(ctx)
	}

	// The embedded frontend takes everything the API does not claim. Go 1.22
	// ServeMux prefers the more specific "/api/" pattern, and does not strip
	// it, so the API router still sees the full path it registered.
	// An unauthenticated deployment is reachable only from loopback —
	// config.Load refuses any other bind without a token — but it is still
	// worth naming at boot, because "it works without one" is how it stays
	// that way when the address later changes.
	if cfg.APIToken == "" {
		slog.Warn("no API_TOKEN configured; writes are unauthenticated and the server is bound to loopback only", "addr", cfg.HTTPAddr)
	}
	apiRouter := api.NewRouter(
		api.Deps{Recipes: recipes, Lookups: lookups, Worker: worker},
		api.Security{Token: cfg.APIToken, AllowedOrigins: cfg.CORSOrigins},
	)
	frontend, err := webui.Handler()
	if err != nil {
		return err
	}
	if webui.IsPlaceholder() {
		slog.Warn("serving the placeholder frontend, not a real build — run `make frontend` (or `make build`, which now depends on it) and rebuild")
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", apiRouter)
	mux.Handle("/", frontend)

	server := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}

	// Shutdown runs on signal; run() waits for it to finish draining before
	// returning, so the deferred pool.Close above cannot pull the database out
	// from under a request that is still being served.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
	}()

	slog.Info("recipe-reader listening", "addr", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-shutdownDone
	return nil
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
