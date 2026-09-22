package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/instagram"
	"github.com/sBurmester/recipe-reader/internal/pipeline"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

// ImportOnce runs exactly one import and returns its tally. It is the one-off
// half of what Run does on a schedule — same extractor, same fetcher, same
// pipeline, same cross-process lock — without the HTTP server, the worker and
// the shutdown sequence.
//
// cfg.HTTP is not read: an import serves nothing.
func ImportOnce(ctx context.Context, cfg config.Config) (pipeline.ImportResult, error) {
	if cfg.Instagram.Username == "" {
		return pipeline.ImportResult{}, errors.New("import: no Instagram account configured; set INSTAGRAM_USERNAME")
	}

	// Before anything external, exactly as in Run: a misconfigured extraction
	// mode is a startup error and should not be buried under a migration.
	extractor, err := newExtractor(cfg)
	if err != nil {
		return pipeline.ImportResult{}, err
	}

	pool, err := db.Open(ctx, cfg.Database.DSN)
	if err != nil {
		return pipeline.ImportResult{}, err
	}
	defer pool.Close()

	// Taken here, before newFetcher, and not by the Pipeline below. The lock
	// exists to stop two processes logging into one Instagram account (D1), and
	// the pipeline's own guard runs after the fetcher has already logged in and
	// written the session file — so a refused run used to pay the exact cost the
	// lock was introduced to avoid before it said no.
	//
	// The Pipeline built below therefore has no Lock: it would ask this pool for
	// a second connection and Postgres would refuse this process the lock it is
	// already holding, so the command would report itself as "another import".
	// serve keeps the pipeline's guard — there the login happens once at
	// startup, not per run, so the guard has nothing to get in front of.
	release, ok, err := (db.ImportLock{Pool: pool}).TryAcquire(ctx)
	if err != nil {
		return pipeline.ImportResult{}, fmt.Errorf("import: taking the import lock: %w", err)
	}
	if !ok {
		return pipeline.ImportResult{}, pipeline.ErrImportInProgress
	}
	defer release()

	recipes := repository.NewRecipeRepository(pool)
	lookups := repository.NewLookupRepository(pool)

	fetcher, loginErr := newFetcher(ctx, cfg, instagram.NewClient(), recipes)
	if loginErr != nil {
		// serve carries on and retries at the next tick, under the 15-minute
		// floor. A single run has no next tick, so a failed login is its result.
		return pipeline.ImportResult{}, loginErr
	}

	p := &pipeline.Pipeline{
		Fetcher:   fetcher,
		Extractor: extractor,
		Recipes:   recipes,
		// The lookup repository the API serves the category picker from, so an
		// import may only attach a category the picker already offers.
		Categories:       lookups,
		Threshold:        cfg.Extraction.Threshold,
		PublishThreshold: cfg.Extraction.PublishThreshold,
	}
	return p.Run(ctx)
}
