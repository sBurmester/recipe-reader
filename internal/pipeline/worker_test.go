// internal/pipeline/worker_test.go
package pipeline

import (
	"context"
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

	lastRun, lastResult, lastErr, running := w.Status()
	if lastRun.IsZero() {
		t.Error("expected lastRun to be set after RunOnce")
	}
	if lastResult != result || lastErr != nil || running {
		t.Errorf("Status() = %v %v %v %v", lastRun, lastResult, lastErr, running)
	}
}

func TestWorker_RunOnce_SkipsConcurrentOverlap(t *testing.T) {
	fetcher := &blockingFetcher{release: make(chan struct{})}
	p := &Pipeline{
		Fetcher:   fetcher,
		Extractor: &fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{}},
		Recipes:   newTestPipeline(t, &fakeFetcher{}, nil).Recipes,
		Lookups:   newTestPipeline(t, &fakeFetcher{}, nil).Lookups,
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
