package instagram

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	ig "github.com/felipeinf/instago"
)

// collectionAccount stubs the two endpoints a collection import touches: the
// collection list, and one page of the collection's posts. fetchErr, when set,
// is what the posts endpoint fails with.
type collectionAccount struct {
	lists, fetches int
	fetchErr       error
}

func (a *collectionAccount) send(_ context.Context, opts ig.PrivateRequestOpts) (map[string]any, error) {
	switch opts.Endpoint {
	case "collections/list/":
		a.lists++
		return map[string]any{"items": []any{
			map[string]any{"collection_id": "c-other", "collection_name": "Reisen"},
			map[string]any{"collection_id": "c-recipes", "collection_name": "Rezepte"},
		}}, nil
	case "feed/collection/c-recipes/posts/":
		a.fetches++
		if a.fetchErr != nil {
			return nil, a.fetchErr
		}
		return map[string]any{"items": []any{map[string]any{"media": map[string]any{"code": "abc"}}}}, nil
	default:
		return nil, fmt.Errorf("unexpected endpoint %q", opts.Endpoint)
	}
}

func collectionFetcher(a *collectionAccount) *PipelineFetcher {
	return &PipelineFetcher{Client: &Client{send: a.send}, CollectionName: "Rezepte"}
}

// integration I9: the name used to be resolved on every run — one extra
// request per import against the API whose request volume is the risk.
func TestPipelineFetcher_ResolvesTheCollectionOnce(t *testing.T) {
	account := &collectionAccount{}
	f := collectionFetcher(account)

	for run := range 3 {
		posts, err := f.FetchNewPosts(context.Background())
		if err != nil {
			t.Fatalf("run %d: FetchNewPosts() error = %v", run, err)
		}
		if len(posts) != 1 {
			t.Fatalf("run %d: got %d posts, want 1", run, len(posts))
		}
	}
	if account.lists != 1 {
		t.Errorf("collections listed %d times over three runs, want once", account.lists)
	}
}

// What the endpoint answers for a deleted collection has never been observed,
// so a failure that could be about the id drops it, and the next run looks the
// name up again — instead of failing on a stale id until a restart.
func TestPipelineFetcher_ReResolvesAfterAFailedFetch(t *testing.T) {
	account := &collectionAccount{fetchErr: errors.New("instagram error: status 404")}
	f := collectionFetcher(account)

	if _, err := f.FetchNewPosts(context.Background()); err == nil {
		t.Fatal("FetchNewPosts() error = nil, want the fetch failure")
	}
	account.fetchErr = nil
	if _, err := f.FetchNewPosts(context.Background()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if account.lists != 2 {
		t.Errorf("collections listed %d times, want 2 — the failed fetch should have dropped the id", account.lists)
	}
}

// Failures that are plainly about something else keep the id. A rate limit
// above all: looking the name up again would be one more request against an
// API that has just said to stop.
func TestPipelineFetcher_KeepsTheIDThroughUnrelatedFailures(t *testing.T) {
	for name, fetchErr := range map[string]error{
		"rate limited":   fmt.Errorf("%w: slow down", ErrRateLimited),
		"session":        fmt.Errorf("%w: login_required", ErrReauthRequired),
		"watchdog":       fmt.Errorf("instagram: feed: %w", context.DeadlineExceeded),
		"shutting down":  context.Canceled,
		"after shutdown": fmt.Errorf("instagram: paging: %w", context.Canceled),
	} {
		t.Run(name, func(t *testing.T) {
			account := &collectionAccount{fetchErr: fetchErr}
			f := collectionFetcher(account)

			_, _ = f.FetchNewPosts(context.Background())
			account.fetchErr = nil
			if _, err := f.FetchNewPosts(context.Background()); err != nil {
				t.Fatalf("second run: %v", err)
			}
			if account.lists != 1 {
				t.Errorf("collections listed %d times, want once — %v says nothing about the id", account.lists, fetchErr)
			}
		})
	}
}

// The cache expires, so a collection renamed away and replaced under the
// configured name is picked up within a day rather than at the next restart.
func TestPipelineFetcher_ReResolvesOnceTheIDIsStale(t *testing.T) {
	account := &collectionAccount{}
	f := collectionFetcher(account)
	if _, err := f.FetchNewPosts(context.Background()); err != nil {
		t.Fatal(err)
	}

	f.resolvedAt = time.Now().Add(-collectionIDMaxAge - time.Minute)
	if _, err := f.FetchNewPosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if account.lists != 2 {
		t.Errorf("collections listed %d times, want 2 once the cached id had expired", account.lists)
	}
}

// A name that matches no collection is an error, and is not cached as one.
func TestPipelineFetcher_UnknownCollectionIsAnErrorEveryRun(t *testing.T) {
	account := &collectionAccount{}
	f := &PipelineFetcher{Client: &Client{send: account.send}, CollectionName: "Gibt es nicht"}

	for range 2 {
		if _, err := f.FetchNewPosts(context.Background()); err == nil {
			t.Fatal("FetchNewPosts() error = nil for a collection that does not exist")
		}
	}
	if account.lists != 2 || account.fetches != 0 {
		t.Errorf("lists = %d, fetches = %d; want 2 lookups and no fetch", account.lists, account.fetches)
	}
}
