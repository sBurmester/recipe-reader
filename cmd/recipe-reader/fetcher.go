package main

import (
	"context"
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
		Options:        instagram.FetchOptions{Known: alreadyImported(recipes)},
	}, loginErr
}
