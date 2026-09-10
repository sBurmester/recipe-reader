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
	MaxItemsPerRun int
}

// FetchNewPosts returns up to MaxItemsPerRun saved posts (defaulting to 50
// when unset), drawn from CollectionName when one is configured and from the
// account's full saved feed otherwise. It returns every post it finds — the
// "new" in the name is the pipeline's job, which dedupes by source before
// importing.
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
