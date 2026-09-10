package api

import (
	"context"
	"encoding/json"
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
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodPost, "/api/import/run", nil)
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
	router := NewRouter(Deps{})

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, "/api/import/run"},
		{http.MethodGet, "/api/import/status"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s status = %d, want 503", tc.method, tc.path, rec.Code)
		}
	}
}
