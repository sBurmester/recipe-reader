package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sBurmester/recipe-reader/internal/extraction"
	"github.com/sBurmester/recipe-reader/internal/instagram"
	"github.com/sBurmester/recipe-reader/internal/pipeline"
)

type noopFetcher struct{}

func (noopFetcher) FetchNewPosts(_ context.Context) ([]instagram.SavedPost, error) {
	return nil, nil
}

func TestImportHandlers_RunAndStatus(t *testing.T) {
	deps := newTestDeps(t)
	// The nil *HybridExtractor is safe only because noopFetcher returns zero
	// posts: the pipeline loop never reaches Extract, so no nil method call
	// actually happens.
	deps.Worker = pipeline.NewWorker(&pipeline.Pipeline{
		Fetcher:   noopFetcher{},
		Extractor: (*extraction.HybridExtractor)(nil),
		Recipes:   deps.Recipes,
		Lookups:   deps.Lookups,
		Threshold: 0.6,
	}, time.Hour)
	router := NewRouter(deps, testSecurity)

	req := jsonRequest(http.MethodPost, "/api/import/run", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/import/status", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status status = %d", rec.Code)
	}
	var status struct {
		Running bool   `json:"running"`
		LastRun string `json:"last_run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

// TestImportHandlers_NoWorker covers the deployment where the import pipeline
// is not configured. Without the nil check, handleImportRun would panic inside
// a background goroutine, where the recovery middleware cannot reach it and
// the whole process would go down.
func TestImportHandlers_NoWorker(t *testing.T) {
	router := NewRouter(Deps{}, testSecurity)

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, "/api/import/run"},
		{http.MethodGet, "/api/import/status"},
	} {
		req := jsonRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s status = %d, want 503", tc.method, tc.path, rec.Code)
		}
	}
}

// rateLimitedFetcher drives the worker into its cooldown the way a throttled
// Instagram walk does.
type rateLimitedFetcher struct{}

func (rateLimitedFetcher) FetchNewPosts(_ context.Context) ([]instagram.SavedPost, error) {
	return nil, fmt.Errorf("pipeline: fetch posts: %w", instagram.ErrRateLimited)
}

// A 202 the worker then ignores would tell the caller a run started when none
// did, and the status endpoint would go on reporting the previous run's tally.
func TestImportHandlers_RefusesWhileCoolingDown(t *testing.T) {
	deps := newTestDeps(t)
	deps.Worker = pipeline.NewWorker(&pipeline.Pipeline{
		Fetcher:   rateLimitedFetcher{},
		Extractor: (*extraction.HybridExtractor)(nil),
		Recipes:   deps.Recipes,
		Lookups:   deps.Lookups,
		Threshold: 0.6,
	}, time.Hour)
	deps.Worker.RunOnce(context.Background())
	router := NewRouter(deps, testSecurity)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/import/run", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("run status = %d, want 429 during the cooldown", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected a Retry-After header")
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/import/status", nil))
	var status struct {
		CooldownSeconds int    `json:"cooldown_seconds"`
		CooldownUntil   string `json:"cooldown_until"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if status.CooldownSeconds <= 0 || status.CooldownUntil == "" {
		t.Errorf("status = %+v, want the cooldown reported so the UI can say paused rather than ready", status)
	}
}
