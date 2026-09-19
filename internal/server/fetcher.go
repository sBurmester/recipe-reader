package server

import (
	"context"
	"errors"
	"log/slog"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/instagram"
	"github.com/sBurmester/recipe-reader/internal/pipeline"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

// newFetcher returns the import's post fetcher, or nil when no Instagram
// account is configured. loginErr reports a startup login that failed — and the
// fetcher is returned regardless.
//
// That last part is the fix. A failed login used to withhold the fetcher, so no
// worker was built and both import routes answered 503 for the life of the
// process: a network blip or a transient checkpoint at boot looked exactly like
// never having configured an account, and only a restart recovered from it.
// The client keeps the credentials it was given and logs in before its next
// request, under the 15-minute floor that rations every login, so a later run
// recovers on its own.
func newFetcher(ctx context.Context, cfg config.Config, client *instagram.Client, recipes repository.RecipeRepository) (fetcher pipeline.PostFetcher, loginErr error) {
	if cfg.InstagramUsername == "" {
		return nil, nil
	}
	loginErr = client.LoginOrRestore(ctx, cfg.InstagramUsername, cfg.InstagramPassword, cfg.InstagramSessionPath)
	if loginErr != nil {
		slog.Warn("instagram login failed at startup; the next import retries it", "error", loginErr)
	}
	return &instagram.PipelineFetcher{
		Client:         client,
		CollectionName: cfg.InstagramCollection,
		Options: instagram.FetchOptions{
			MaxItems: cfg.ImportMaxItems,
			MaxPages: cfg.ImportMaxPages,
			Known:    alreadyImported(recipes),
		},
	}, loginErr
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
