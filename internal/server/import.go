package server

import (
	"context"
	"errors"

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
		Lock:             db.ImportLock{Pool: pool},
	}
	return p.Run(ctx)
}
