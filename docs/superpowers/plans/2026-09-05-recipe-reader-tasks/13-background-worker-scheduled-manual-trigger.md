> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 4: Import Pipeline.
>
> **Status:** [ ] not started

# Task 13: Background Worker (Scheduled + Manual Trigger)

**Files:**
- Create: `internal/pipeline/worker.go`
- Test: `internal/pipeline/worker_test.go`

**Interfaces:**
- Consumes: `Pipeline`, `ImportResult` (Task 12).
- Produces:

```go
type Worker struct { /* unexported fields */ }

func NewWorker(p *Pipeline, interval time.Duration) *Worker
func (w *Worker) Start(ctx context.Context)          // runs Pipeline.Run every interval until ctx is done
func (w *Worker) RunOnce(ctx context.Context) ImportResult
func (w *Worker) Status() (lastRun time.Time, lastResult ImportResult, lastErr error, running bool)
```

Task 17 (`handlers_import.go`) calls `RunOnce` (trigger endpoint) and `Status` (status endpoint) on the single `*Worker` instance constructed in Task 18 (`main.go`), which also calls `Start` once at boot.

- [ ] **Step 1: Write the failing test**

```go
// internal/pipeline/worker_test.go
package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/... -run TestWorker -v`
Expected: FAIL — `NewWorker` undefined.

- [ ] **Step 3: Implement**

```go
// internal/pipeline/worker.go
package pipeline

import (
	"context"
	"sync"
	"time"
)

type Worker struct {
	pipeline *Pipeline
	interval time.Duration

	mu         sync.Mutex
	running    bool
	lastRun    time.Time
	lastResult ImportResult
	lastErr    error
}

func NewWorker(p *Pipeline, interval time.Duration) *Worker {
	return &Worker{pipeline: p, interval: interval}
}

func (w *Worker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.RunOnce(ctx)
			}
		}
	}()
}

func (w *Worker) RunOnce(ctx context.Context) ImportResult {
	w.mu.Lock()
	if w.running {
		result := w.lastResult
		w.mu.Unlock()
		return result
	}
	w.running = true
	w.mu.Unlock()

	result, err := w.pipeline.Run(ctx)

	w.mu.Lock()
	w.running = false
	w.lastRun = time.Now()
	w.lastResult = result
	w.lastErr = err
	w.mu.Unlock()

	return result
}

func (w *Worker) Status() (lastRun time.Time, lastResult ImportResult, lastErr error, running bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastRun, w.lastResult, w.lastErr, w.running
}
```

- [ ] **Step 4: Add the missing test-file imports**

`internal/pipeline/worker_test.go` needs `"github.com/sBurmester/recipe-reader/internal/extraction"` and `"github.com/sBurmester/recipe-reader/internal/instagram"` added to its import block (used by `fakeExtractor`/`instagram.SavedPost` inside `blockingFetcher`).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/pipeline/... -v -race`
Expected: PASS. Run with `-race` since `Worker` is accessed from two goroutines in the overlap test — this is the moment to catch a missing lock, not later in production.

- [ ] **Step 6: Commit**

```bash
git add internal/pipeline/worker.go internal/pipeline/worker_test.go
git commit -m "$(cat <<'EOF'
feat: add background import worker with scheduled and manual triggers

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 12](12-import-pipeline-fetch-extract-store.md) · [Task 14 →](14-router-middleware-health-check.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
