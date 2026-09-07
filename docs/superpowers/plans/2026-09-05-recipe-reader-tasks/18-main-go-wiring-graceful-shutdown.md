> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 5: REST API.
>
> **Status:** [ ] not started

# Task 18: `main.go` Wiring & Graceful Shutdown

**Files:**
- Modify: `cmd/recipe-reader/main.go` (replace the Task 1 placeholder entirely)
- Create: `internal/instagram/fetcher_adapter.go`

**Interfaces:**
- Consumes: everything from Tasks 2-17: `config.Load`, `db.Connect`/`Migrate`/`Seed`, `repository.NewRecipeRepository`/`NewLookupRepository`, `extraction.NewRuleBasedExtractor`/`NewLLMExtractor`/`NewHybridExtractor`, `instagram.NewClient`, `pipeline.Pipeline`/`NewWorker`, `api.NewRouter`/`Deps`.
- Produces: the running binary. No other task consumes this one — it is the composition root.

> **If [Task 25](25-provider-agnostic-llm-extractor.md) is done first,** replace the `NewLLMExtractor(cfg.AnthropicAPIKey, cfg.AnthropicModel)` block in Step 2's `run()` with the config-driven `extraction.LLMConfig` construction from Task 25 Step 6 (handles `LLM_PROVIDER` / `LLM_BASE_URL`, returns an `error`). The `llm == nil ⇒ rules-only` behaviour is unchanged.

- [ ] **Step 1: Implement the fetcher adapter**

```go
// internal/instagram/fetcher_adapter.go
package instagram

import "context"

// PipelineFetcher adapts *Client to pipeline.PostFetcher, choosing between
// the named saved collection (if configured) and the account's full saved
// posts otherwise.
type PipelineFetcher struct {
	Client         *Client
	CollectionName string
	MaxItemsPerRun int
}

func (f *PipelineFetcher) FetchNewPosts(_ context.Context) ([]SavedPost, error) {
	maxItems := f.MaxItemsPerRun
	if maxItems <= 0 {
		maxItems = 50
	}
	if f.CollectionName == "" {
		return f.Client.FetchSavedPosts(maxItems)
	}
	collectionID, err := f.Client.ResolveCollectionID(f.CollectionName)
	if err != nil {
		return nil, err
	}
	return f.Client.FetchCollectionPosts(collectionID, maxItems)
}
```

- [ ] **Step 2: Replace `cmd/recipe-reader/main.go`**

```go
// cmd/recipe-reader/main.go
package main

import (
	"context"
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

	rules := extraction.NewRuleBasedExtractor()
	var llm extraction.Extractor
	if cfg.ExtractionMode == "hybrid" && cfg.AnthropicAPIKey != "" {
		llm = extraction.NewLLMExtractor(cfg.AnthropicAPIKey, cfg.AnthropicModel)
	}
	extractor := extraction.NewHybridExtractor(rules, llm, cfg.ExtractionThreshold)

	igClient := instagram.NewClient()
	var fetcher pipeline.PostFetcher
	if cfg.InstagramUsername != "" {
		if err := igClient.LoginOrRestore(cfg.InstagramUsername, cfg.InstagramPassword, cfg.InstagramSessionPath); err != nil {
			slog.Warn("instagram login failed; import worker will not run", "error", err)
		} else {
			fetcher = &instagram.PipelineFetcher{Client: igClient, CollectionName: cfg.InstagramCollection}
		}
	}

	var worker *pipeline.Worker
	if fetcher != nil {
		p := &pipeline.Pipeline{
			Fetcher: fetcher, Extractor: extractor,
			Recipes: recipes, Lookups: lookups, Threshold: cfg.ExtractionThreshold,
		}
		worker = pipeline.NewWorker(p, cfg.ImportInterval)
		worker.Start(ctx)
	}

	router := api.NewRouter(api.Deps{Recipes: recipes, Lookups: lookups, Worker: worker})
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: router}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	slog.Info("recipe-reader listening", "addr", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
```

`api.Deps.Worker` is `*pipeline.Worker`; when Instagram credentials are not configured, `worker` stays `nil` and `handleImportRun`/`handleImportStatus` (Task 17) will panic on the nil pointer — the recovery middleware (Task 14) turns that into a 500. If this shows up in practice, add a `d.Worker == nil` → `503 Service Unavailable` guard as the first line of both handlers.

- [ ] **Step 3: Verify the full build and test suite**

```bash
go build ./...
go test ./...
```

Expected: build succeeds, all tests from Tasks 2-17 pass (Docker must be running — `internal/db`, `internal/repository`, `internal/pipeline`, and `internal/api` tests all spin up ephemeral Postgres containers via `testdb.New`).

- [ ] **Step 4: Manual smoke test**

```bash
cp .env.example .env
make db-up
make run
# in another shell:
curl -s localhost:8080/api/healthz
curl -s localhost:8080/api/categories
```

Expected: `{"status":"ok"}` and a JSON array of the seeded categories.

- [ ] **Step 5: Commit**

```bash
git add cmd/recipe-reader/main.go internal/instagram/fetcher_adapter.go
git commit -m "$(cat <<'EOF'
feat: wire config, db, extraction, instagram, pipeline, and API into main.go

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 17](17-import-trigger-status-handlers.md) · [Task 19 →](19-vite-scaffold-api-client-shared-types.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
