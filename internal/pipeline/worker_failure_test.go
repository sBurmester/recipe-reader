package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sBurmester/recipe-reader/internal/extraction"
)

// A failure before the first run — a startup login — is reported straight
// away, and the next run's outcome replaces it rather than leaving a stale
// error beside a successful tally.
func TestWorker_RecordFailure_IsReportedUntilTheNextRun(t *testing.T) {
	p := newTestPipeline(t, &fakeFetcher{}, &fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{}})
	w := NewWorker(p, time.Hour)

	loginErr := errors.New("instagram login failed at startup")
	w.RecordFailure(loginErr)
	if got := w.Status().LastErr; !errors.Is(got, loginErr) {
		t.Fatalf("Status().LastErr = %v, want the recorded failure", got)
	}

	w.RunOnce(context.Background())
	if got := w.Status().LastErr; got != nil {
		t.Errorf("Status().LastErr = %v after a successful run, want nil", got)
	}
}
