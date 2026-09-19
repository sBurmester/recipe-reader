package instagram

import (
	"context"
	"errors"
	"sync"
	"time"
)

// collectionIDMaxAge is how long a resolved collection id is reused before the
// name is looked up again.
//
// Collection ids are stable, so the cache is not guarding against the id
// changing. It guards against the *name* moving: a collection renamed away and
// a new one created under the configured name would otherwise keep importing
// the old one, silently, until a restart. A day bounds that at one list request
// in four runs on the default six-hour interval, instead of one in every run.
const collectionIDMaxAge = 24 * time.Hour

// PipelineFetcher adapts *Client to pipeline.PostFetcher, choosing between
// the named saved collection (if configured) and the account's full saved
// posts otherwise.
//
// It satisfies pipeline.PostFetcher structurally rather than importing the
// pipeline package, which keeps the dependency pointing one way: the pipeline
// knows nothing about Instagram, and this package knows nothing about the
// pipeline. internal/server is the only place the two meet.
type PipelineFetcher struct {
	Client         *Client
	CollectionName string

	// Options bounds one run. Its Known hook is what makes the fetcher skip
	// past already-imported posts instead of re-collecting the newest page
	// every time; internal/server supplies it from the recipe repository.
	Options FetchOptions

	// mu guards the cached collection id. The worker already runs one import
	// at a time; this keeps the cache sound for a caller that does not.
	mu           sync.Mutex
	collectionID string
	resolvedAt   time.Time
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
	collectionID, err := f.resolveCollectionID(ctx)
	if err != nil {
		return nil, err
	}
	posts, err := f.Client.FetchCollectionPosts(ctx, collectionID, f.Options)
	if err != nil && !saysNothingAboutTheCollection(err) {
		f.forgetCollectionID(collectionID)
	}
	return posts, err
}

// resolveCollectionID returns the id for CollectionName, from the cache when it
// is fresh and by listing the account's collections otherwise.
//
// Every run used to list the collections to translate the same name into the
// same id: one extra request per import against an API where request volume is
// the risk being managed, and a second way for every run to fail before it had
// fetched anything.
func (f *PipelineFetcher) resolveCollectionID(ctx context.Context) (string, error) {
	f.mu.Lock()
	id, resolvedAt := f.collectionID, f.resolvedAt
	f.mu.Unlock()
	if id != "" && time.Since(resolvedAt) < collectionIDMaxAge {
		return id, nil
	}

	id, err := f.Client.ResolveCollectionID(ctx, f.CollectionName)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	f.collectionID, f.resolvedAt = id, time.Now()
	f.mu.Unlock()
	return id, nil
}

// forgetCollectionID drops the cached id, so the next run looks the name up
// again — unless another run has already replaced it.
func (f *PipelineFetcher) forgetCollectionID(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.collectionID == id {
		f.collectionID, f.resolvedAt = "", time.Time{}
	}
}

// saysNothingAboutTheCollection reports whether a failed collection fetch leaves
// the cached id trustworthy.
//
// The finding asked to re-resolve "only on a not-found error", but what this
// private endpoint answers for a deleted collection has never been observed
// (review task T-43): it may be a 404, a 400, or a page that does not decode.
// Betting on one of them risks a stale id that fails every run until a restart.
// So the cache is kept only through failures that are plainly about something
// else, and dropped on the rest — which costs one list request on the next run,
// the price every run paid before. A rate limit is kept above all: looking the
// name up again would be one more request against an API that just said stop.
func saysNothingAboutTheCollection(err error) bool {
	return errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrReauthRequired) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}
