package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/sBurmester/recipe-reader/internal/instagram"
)

// rateLimitCooldown is how long the worker stands down after Instagram
// throttles a run.
//
// The dependency's own message asks callers to "wait a few minutes"; this is
// deliberately longer than that. The cost of waiting too long is a delayed
// import on a schedule that already defaults to six hours. The cost of not
// waiting is an unofficial client walking straight back into the throttle on
// its next tick, which is how an account gets flagged.
const rateLimitCooldown = 30 * time.Minute

// Worker drives a Pipeline on a schedule and on demand, and remembers the
// outcome of the most recent run so the API can report import status. It
// serializes runs: while one is in flight, further triggers are ignored
// rather than queued, so a slow import can't pile up behind itself.
type Worker struct {
	pipeline *Pipeline
	interval time.Duration

	// runs counts the goroutines this worker started — the schedule loop and
	// every triggered run — so Wait can hold shutdown until they are done.
	runs sync.WaitGroup

	mu sync.Mutex
	// base is the context Start was given, kept so that runs started later by
	// Trigger execute under it and stop with it. Nil before Start.
	base          context.Context
	running       bool
	lastRun       time.Time
	lastResult    ImportResult
	lastErr       error
	cooldownUntil time.Time
}

// Status is a snapshot of the worker's state for the import status endpoint.
// LastRun is the zero time until the first run completes, and CooldownUntil is
// the zero time unless Instagram rate-limited the last run.
type Status struct {
	LastRun       time.Time
	LastResult    ImportResult
	LastErr       error
	Running       bool
	CooldownUntil time.Time
}

// NewWorker returns a Worker that runs p every interval once Start is called.
// interval must be positive; Start refuses to schedule anything otherwise.
func NewWorker(p *Pipeline, interval time.Duration) *Worker {
	return &Worker{pipeline: p, interval: interval}
}

// Start launches the schedule in a background goroutine and returns
// immediately. The first run happens one interval from now, not at boot; call
// Trigger for an immediate import. The schedule stops when ctx is cancelled,
// and ctx is also what runs started by Trigger execute under.
func (w *Worker) Start(ctx context.Context) {
	w.mu.Lock()
	w.base = ctx
	w.mu.Unlock()

	// time.NewTicker panics for a non-positive duration, on this goroutine —
	// so a bad interval used to abort the process with a stack trace at
	// startup. config.validate rejects one now, which is where an operator's
	// typo belongs; this is the second line of defence for the callers that do
	// not come through config, and it refuses loudly rather than crashing.
	// Trigger still works, so an on-demand import is unaffected.
	if w.interval <= 0 {
		slog.Error("import: schedule not started, interval must be positive", "interval", w.interval)
		return
	}

	ticker := time.NewTicker(w.interval)
	w.runs.Add(1)
	go func() {
		defer w.runs.Done()
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

// Trigger starts a run in the background and returns at once. The run belongs
// to the worker, not to the caller: it executes under the context Start was
// given, so a shutdown cancels it, and Wait waits for it. Before Start it runs
// under context.Background.
//
// It replaces the import handler's `go RunOnce(context.WithoutCancel(...))`.
// Detaching the run from the request was right — the request's context ends
// the moment the handler answers 202. Detaching it from everything was not:
// nothing owned that goroutine, so shutdown drained the HTTP server, returned
// from run() (now server.Run) and closed the database pool beneath it, and the
// truncated run was never reported because the process was already gone.
func (w *Worker) Trigger() {
	w.mu.Lock()
	ctx := w.base
	w.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}

	w.runs.Add(1)
	go func() {
		defer w.runs.Done()
		w.RunOnce(ctx)
	}()
}

// Wait blocks until every goroutine the worker started has returned, or until
// ctx is done — in which case it returns ctx's error and leaves them running.
//
// Call it after cancelling the context given to Start, or the schedule loop
// never returns; and after the HTTP server has shut down, so no handler can
// Trigger another run while it waits.
func (w *Worker) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		w.runs.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RunOnce runs the pipeline synchronously, records the outcome, and returns it
// with ran set. If a run is already in progress, or Instagram is still being
// waited out after a rate limit, it starts nothing and returns ran false with
// a zero result. The pipeline runs outside the lock so that Status stays
// responsive during a long import.
//
// A skip used to return the previous run's result, so a caller could not tell
// "ran and produced this" from "was busy, here is old data" — and old data is
// exactly what a tally that looks healthy is made of. Status is where the last
// run's outcome is read; RunOnce reports only its own.
func (w *Worker) RunOnce(ctx context.Context) (result ImportResult, ran bool) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return ImportResult{}, false
	}
	if remaining := time.Until(w.cooldownUntil); remaining > 0 {
		w.mu.Unlock()
		// Logged rather than skipped silently: a worker that declines to run
		// looks, from the outside, identical to one that ran and found nothing.
		slog.Warn("import: skipped, still cooling down after a rate limit", "retry_in", remaining.Round(time.Second))
		return ImportResult{}, false
	}
	w.running = true
	w.mu.Unlock()

	result, err := w.pipeline.Run(ctx)

	w.mu.Lock()
	w.running = false
	w.lastRun = time.Now()
	w.lastResult = result
	w.lastErr = err
	if errors.Is(err, instagram.ErrRateLimited) {
		w.cooldownUntil = time.Now().Add(rateLimitCooldown)
	}
	cooldownUntil := w.cooldownUntil
	w.mu.Unlock()

	if err != nil {
		if errors.Is(err, instagram.ErrRateLimited) {
			slog.Warn("import: rate limited by Instagram; backing off",
				"until", cooldownUntil.Format(time.RFC3339), "imported", result.Imported, "error", err)
		} else {
			slog.Warn("import: run failed", "error", err, "result", result)
		}
	}
	return result, true
}

// Cooldown reports how long the worker will refuse to start a run, and is zero
// when it will start one now.
func (w *Worker) Cooldown() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	if remaining := time.Until(w.cooldownUntil); remaining > 0 {
		return remaining
	}
	return 0
}

// Status reports the most recent run's timestamp, tally, and error, plus
// whether a run is in flight right now and when a rate-limit cooldown expires.
func (w *Worker) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Status{
		LastRun:       w.lastRun,
		LastResult:    w.lastResult,
		LastErr:       w.lastErr,
		Running:       w.running,
		CooldownUntil: w.cooldownUntil,
	}
}

// RecordFailure sets err as the error Status reports, without a run. It is for
// failures that happen before the first run — a startup login, above all —
// which would otherwise stay invisible until the first scheduled import, hours
// later. The next run's outcome replaces it.
func (w *Worker) RecordFailure(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastErr = err
}
