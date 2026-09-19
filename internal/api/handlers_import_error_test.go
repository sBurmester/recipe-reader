package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sBurmester/recipe-reader/internal/instagram"
	"github.com/sBurmester/recipe-reader/internal/pipeline"
)

// The chain a failed import leaves behind is wrapped around whatever the
// Instagram client returned: the endpoint it called, fragments of the upstream
// response, and — with a database error in it — parts of the DSN. This endpoint
// used to hand all of that to any caller, and per security S1 there are no
// authenticated callers to restrict it to.
func TestImportStatus_DoesNotEchoTheErrorChain(t *testing.T) {
	const secret = "postgres://recipes:hunter2@db:5432/recipes"
	leaky := fmt.Errorf("%w: %w",
		pipeline.ErrFetch,
		fmt.Errorf("instagram: GET https://i.instagram.com/api/v1/feed/saved/?max_id=QVFE: %s", secret))

	deps := newTestDeps(t)
	worker := pipeline.NewWorker(nil, time.Hour)
	worker.RecordFailure(leaky)
	deps.Worker = worker

	body := getStatus(t, deps)

	if strings.Contains(body, "hunter2") || strings.Contains(body, "i.instagram.com") {
		t.Fatalf("status response carries the error chain: %s", body)
	}

	var status struct {
		Error        string `json:"error"`
		ErrorMessage string `json:"error_message"`
	}
	if err := json.Unmarshal([]byte(body), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if status.Error != string(pipeline.FailureFetch) {
		t.Errorf("error = %q, want %q", status.Error, pipeline.FailureFetch)
	}
	if status.ErrorMessage == "" {
		t.Error("error_message is empty; the frontend has nothing to fall back to")
	}
}

// A rate limit has to stay distinguishable from a generic fetch failure: it is
// the one failure the UI explains with a countdown rather than an apology.
func TestImportStatus_ReportsTheRateLimitCode(t *testing.T) {
	deps := newTestDeps(t)
	worker := pipeline.NewWorker(nil, time.Hour)
	worker.RecordFailure(fmt.Errorf("%w: %w", pipeline.ErrFetch, instagram.ErrRateLimited))
	deps.Worker = worker

	var status struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(getStatus(t, deps)), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if status.Error != string(pipeline.FailureRateLimited) {
		t.Errorf("error = %q, want %q", status.Error, pipeline.FailureRateLimited)
	}
}

// A run that has not failed reports no error fields at all, rather than an
// empty string the frontend would have to test for.
func TestImportStatus_OmitsErrorFieldsWhenThereIsNoFailure(t *testing.T) {
	deps := newTestDeps(t)
	deps.Worker = pipeline.NewWorker(nil, time.Hour)

	var status map[string]any
	if err := json.Unmarshal([]byte(getStatus(t, deps)), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := status["error"]; ok {
		t.Errorf("error field present on a clean status: %+v", status)
	}
	if _, ok := status["error_message"]; ok {
		t.Errorf("error_message field present on a clean status: %+v", status)
	}
}

func getStatus(t *testing.T, deps Deps) string {
	t.Helper()
	rec := httptest.NewRecorder()
	NewRouter(deps, testSecurity).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/import/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}
