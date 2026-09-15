// internal/pipeline/worker_test.go
package pipeline

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sBurmester/recipe-reader/internal/extraction"
	"github.com/sBurmester/recipe-reader/internal/instagram"
)

type blockingFetcher struct {
	release chan struct{}
	calls   int
	mu      sync.Mutex
}

func (f *blockingFetcher) FetchNewPosts(_ context.Context) ([]instagram.SavedPost, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	<-f.release
	return nil, nil
}

func TestWorker_RunOnce_ReturnsResultAndUpdatesStatus(t *testing.T) {
	p := newTestPipeline(t, &fakeFetcher{}, &fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{}})
	w := NewWorker(p, time.Hour)

	result := w.RunOnce(context.Background())
	if result != (ImportResult{}) {
		t.Errorf("result = %+v, want zero-value (no posts)", result)
	}

	status := w.Status()
	if status.LastRun.IsZero() {
		t.Error("expected LastRun to be set after RunOnce")
	}
	if status.LastResult != result || status.LastErr != nil || status.Running {
		t.Errorf("Status() = %+v", status)
	}
	if !status.CooldownUntil.IsZero() || w.Cooldown() != 0 {
		t.Errorf("CooldownUntil = %v, want zero after a clean run", status.CooldownUntil)
	}
}

// rateLimitedFetcher returns whatever posts it was given alongside a rate-limit
// error, the way a walk that is throttled part-way through does.
type rateLimitedFetcher struct {
	posts []instagram.SavedPost
	calls int
	mu    sync.Mutex
}

func (f *rateLimitedFetcher) FetchNewPosts(_ context.Context) ([]instagram.SavedPost, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.posts, fmt.Errorf("pipeline: fetch posts: %w", instagram.ErrRateLimited)
}

// A throttle used to arrive as an ordinary fetch failure: the run ended and the
// next tick walked straight back into it. The worker now stands down, which is
// the only defence an unofficial client has against being flagged.
func TestWorker_RunOnce_BacksOffAfterARateLimit(t *testing.T) {
	fetcher := &rateLimitedFetcher{}
	p := newTestPipeline(t, fetcher, &fakeExtractor{})
	w := NewWorker(p, time.Hour)

	w.RunOnce(context.Background())

	if w.Cooldown() <= 0 {
		t.Fatal("Cooldown() = 0 after a rate-limited run, want a positive backoff")
	}
	if status := w.Status(); status.CooldownUntil.IsZero() {
		t.Error("CooldownUntil is zero after a rate-limited run")
	}

	w.RunOnce(context.Background())

	fetcher.mu.Lock()
	calls := fetcher.calls
	fetcher.mu.Unlock()
	if calls != 1 {
		t.Errorf("fetcher.calls = %d, want 1 — the second run should have been refused during the cooldown", calls)
	}
}

// The posts a throttled walk already collected are the only work that run gets.
// Discarding them would mean re-fetching them after the cooldown — more
// requests against the endpoint that just asked for fewer.
func TestWorker_RunOnce_ImportsWhatARateLimitedFetchCollected(t *testing.T) {
	post := instagram.SavedPost{Source: "src-throttled", Caption: "caption-throttled"}
	extracted := &extraction.ExtractedRecipe{Name: "Teilweise", Confidence: 0.9}
	p := newTestPipeline(t,
		&rateLimitedFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{"caption-throttled": extracted}},
	)
	w := NewWorker(p, time.Hour)

	result := w.RunOnce(context.Background())

	if result.Imported != 1 {
		t.Errorf("Imported = %d, want 1 — partial results must still be imported", result.Imported)
	}
	if status := w.Status(); status.LastErr == nil {
		t.Error("LastErr = nil, want the rate-limit error recorded alongside the partial import")
	}
}

func TestWorker_RunOnce_SkipsConcurrentOverlap(t *testing.T) {
	fetcher := &blockingFetcher{release: make(chan struct{})}
	p := &Pipeline{
		Fetcher:   fetcher,
		Extractor: &fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{}},
		Recipes:   newTestPipeline(t, &fakeFetcher{}, nil).Recipes,
		Threshold: 0.6,
	}
	w := NewWorker(p, time.Hour)

	go w.RunOnce(context.Background())
	time.Sleep(50 * time.Millisecond) // let the first RunOnce enter Fetcher.FetchNewPosts and block

	w.RunOnce(context.Background()) // should return immediately without calling the fetcher again
	close(fetcher.release)
	time.Sleep(50 * time.Millisecond)

	fetcher.mu.Lock()
	calls := fetcher.calls
	fetcher.mu.Unlock()
	if calls != 1 {
		t.Errorf("fetcher.calls = %d, want 1 (second RunOnce should have been skipped while running)", calls)
	}
}
