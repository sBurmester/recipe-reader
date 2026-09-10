// internal/pipeline/worker.go
package pipeline

import (
	"context"
	"sync"
	"time"
)

// Worker drives a Pipeline on a schedule and on demand, and remembers the
// outcome of the most recent run so the API can report import status. It
// serializes runs: while one is in flight, further triggers are ignored
// rather than queued, so a slow import can't pile up behind itself.
type Worker struct {
	pipeline *Pipeline
	interval time.Duration

	mu         sync.Mutex
	running    bool
	lastRun    time.Time
	lastResult ImportResult
	lastErr    error
}

// NewWorker returns a Worker that runs p every interval once Start is called.
// interval must be positive — Start's ticker panics otherwise.
func NewWorker(p *Pipeline, interval time.Duration) *Worker {
	return &Worker{pipeline: p, interval: interval}
}

// Start launches the schedule in a background goroutine and returns
// immediately. The first run happens one interval from now, not at boot;
// call RunOnce directly if an immediate import is wanted. The goroutine
// exits when ctx is cancelled.
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

// RunOnce runs the pipeline synchronously and records the outcome. If a run
// is already in progress it returns the previous result immediately without
// starting a second one. The pipeline runs outside the lock so that Status
// stays responsive during a long import.
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

// Status reports the most recent run's timestamp, tally, and error, plus
// whether a run is in flight right now. lastRun is the zero time until the
// first run completes.
func (w *Worker) Status() (lastRun time.Time, lastResult ImportResult, lastErr error, running bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastRun, w.lastResult, w.lastErr, w.running
}
