package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sBurmester/recipe-reader/internal/extraction"
	"github.com/sBurmester/recipe-reader/internal/instagram"
)

// lockStub is an ImportLock whose answer the test chooses. The real lock is
// tested against Postgres in internal/db; what matters here is the branch:
// a refused lock must stop the run before it fetches anything.
type lockStub struct {
	ok       bool
	err      error
	acquired int
	released int
}

func (l *lockStub) TryAcquire(context.Context) (func(), bool, error) {
	l.acquired++
	if l.err != nil {
		return nil, false, l.err
	}
	if !l.ok {
		return nil, false, nil
	}
	return func() { l.released++ }, true, nil
}

func TestRun_RefusedLockStopsBeforeFetching(t *testing.T) {
	lock := &lockStub{ok: false}
	p := &Pipeline{Lock: lock}

	_, err := p.Run(context.Background())

	if !errors.Is(err, ErrImportInProgress) {
		t.Fatalf("Run() error = %v, want ErrImportInProgress", err)
	}
	if lock.acquired != 1 {
		t.Errorf("TryAcquire called %d times, want 1", lock.acquired)
	}
	// A Pipeline with no Fetcher would panic or error the moment it fetched;
	// reaching neither is the proof that the lock came first.
	if lock.released != 0 {
		t.Errorf("release called %d times after a refused lock, want 0", lock.released)
	}
}

func TestRun_LockFailureIsReported(t *testing.T) {
	wantErr := errors.New("boom")
	p := &Pipeline{Lock: &lockStub{err: wantErr}}

	if _, err := p.Run(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want it to wrap %v", err, wantErr)
	}
}

// A refusal used to be recorded as a completed run: lastRun moved to now,
// lastResult became the zero tally and lastErr the sentinel, so GET
// /api/import/status reported a failed import with every counter at 0 and the
// previous real tally was gone — while the import command that held the lock
// was importing perfectly well. The worker did not run, so it must not report
// as if it had.
func TestWorker_RefusedLockLeavesTheLastRunUntouched(t *testing.T) {
	post := instagram.SavedPost{Source: "src-lock", Caption: "caption-lock", ImageURL: "img-lock"}
	extracted := &extraction.ExtractedRecipe{
		Name:         "Pfannkuchen",
		Instructions: "Backen.",
		Ingredients:  []extraction.ExtractedIngredient{{Name: "Mehl", Amount: 200, Unit: "g"}},
		Confidence:   0.9,
	}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{"caption-lock": extracted}},
	)
	w := NewWorker(p, time.Hour)

	if _, ran := w.RunOnce(context.Background()); !ran {
		t.Fatal("RunOnce() ran = false on the first run, want a run to record")
	}
	before := w.Status()
	if before.LastResult.Imported != 1 {
		t.Fatalf("first run imported %d, want 1 — the baseline has to be a real tally",
			before.LastResult.Imported)
	}

	p.Lock = &lockStub{ok: false}
	result, ran := w.RunOnce(context.Background())

	if ran {
		t.Error("RunOnce() ran = true for a refused lock, want false")
	}
	if result != (ImportResult{}) {
		t.Errorf("RunOnce() result = %+v for a refused lock, want the zero tally", result)
	}
	after := w.Status()
	if after.LastRun != before.LastRun {
		t.Errorf("Status().LastRun = %v after a refused lock, want the previous run's %v",
			after.LastRun, before.LastRun)
	}
	if after.LastResult != before.LastResult {
		t.Errorf("Status().LastResult = %+v after a refused lock, want the previous run's %+v",
			after.LastResult, before.LastResult)
	}
	if after.LastErr != nil {
		t.Errorf("Status().LastErr = %v after a refused lock, want nil — nothing failed", after.LastErr)
	}
	if after.Running {
		t.Error("Status().Running = true after a refused lock, want false")
	}
}
