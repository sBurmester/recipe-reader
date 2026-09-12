// internal/instagram/fetcher_adapter.go
package instagram

import "context"

// PipelineFetcher adapts *Client to pipeline.PostFetcher, choosing between
// the named saved collection (if configured) and the account's full saved
// posts otherwise.
//
// It satisfies pipeline.PostFetcher structurally rather than importing the
// pipeline package, which keeps the dependency pointing one way: the pipeline
// knows nothing about Instagram, and this package knows nothing about the
// pipeline. main.go is the only place the two meet.
type PipelineFetcher struct {
	Client         *Client
	CollectionName string

	// Options bounds one run. Its Known hook is what makes the fetcher skip
	// past already-imported posts instead of re-collecting the newest page
	// every time; main.go supplies it from the recipe repository.
	Options FetchOptions
}

// FetchNewPosts returns up to Options.MaxItems saved posts the importer has
// not seen before, drawn from CollectionName when one is configured and from
// the account's full saved feed otherwise.
//
// The "new" in the name used to be aspirational — the fetcher returned
// whatever was at the head of the feed and left deduplication entirely to the
// pipeline, which is why a backlog past the first page never imported. With
// Options.Known wired up it is now accurate, though the pipeline still dedupes
// authoritatively before writing.
func (f *PipelineFetcher) FetchNewPosts(ctx context.Context) ([]SavedPost, error) {
	if f.CollectionName == "" {
		return f.Client.FetchSavedPosts(ctx, f.Options)
	}
	collectionID, err := f.Client.ResolveCollectionID(ctx, f.CollectionName)
	if err != nil {
		return nil, err
	}
	return f.Client.FetchCollectionPosts(ctx, collectionID, f.Options)
}
