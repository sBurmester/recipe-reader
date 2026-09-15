package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sBurmester/recipe-reader/internal/instagram"
)

// shutdownBlockingFetcher stands in for a long import: it reports that it has
// started, then holds until its context is cancelled.
type shutdownBlockingFetcher struct{ started chan struct{} }

func (f *shutdownBlockingFetcher) FetchNewPosts(ctx context.Context) ([]instagram.SavedPost, error) {
	close(f.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

// go #4: an import triggered through the API ran in a goroutine nobody owned.
// Shutdown drained the HTTP server and returned from run(), closing the pool
// under it, and the truncated run was never reported. A triggered run now
// belongs to the worker: cancelling Start's context stops it, and Wait holds
// until it has stopped — with the outcome recorded.
func TestWorker_TriggeredRunIsStoppedByShutdownAndAwaited(t *testing.T) {
	fetcher := &shutdownBlockingFetcher{started: make(chan struct{})}
	w := NewWorker(&Pipeline{Fetcher: fetcher, Threshold: 0.6}, time.Hour)

	ctx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	w.Start(ctx)
	w.Trigger()
	<-fetcher.started

	// While the run is in flight, Wait must not report it finished.
	early, cancelEarly := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelEarly()
	if err := w.Wait(early); err == nil {
		t.Fatal("Wait() returned nil while a triggered run was still in flight")
	}

	shutdown()
	done, cancelDone := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDone()
	if err := w.Wait(done); err != nil {
		t.Fatalf("Wait() after shutdown = %v, want the run to have stopped", err)
	}
	if err := w.Status().LastErr; !errors.Is(err, context.Canceled) {
		t.Errorf("Status().LastErr = %v, want the cancelled run recorded", err)
	}
}

func TestWorker_WaitWithNothingRunningReturnsAtOnce(t *testing.T) {
	w := NewWorker(&Pipeline{}, time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Wait(ctx); err != nil {
		t.Errorf("Wait() = %v, want nil with no runs started", err)
	}
}
