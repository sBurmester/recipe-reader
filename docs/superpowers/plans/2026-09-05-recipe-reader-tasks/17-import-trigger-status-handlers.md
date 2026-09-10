> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 5: REST API.
>
> **Status:** [x] done
>
> **Corrections made during implementation:**
> 1. **`go d.Worker.RunOnce(r.Context())` aborts the import instantly in production (real bug, fixed).** `net/http` cancels `r.Context()` as soon as the handler returns, so the detached goroutine ran against an already-cancelled context. The plan's test cannot catch this — `httptest.NewRequest` + a direct `ServeHTTP` call never cancels. Now uses `context.WithoutCancel(r.Context())`, keeping request-scoped values while dropping the cancellation.
> 2. **A nil `Deps.Worker` took down the whole process (fixed).** `RunOnce` on a nil `*pipeline.Worker` panics at `w.mu.Lock()` *inside the goroutine*, where `withRecovery` cannot reach it — an unrecovered goroutine panic kills the server rather than producing a 500. Both handlers now return `503 "import worker not configured"` when `Worker == nil`, which is the guard [Task 18](18-main-go-wiring-graceful-shutdown.md) Step 2 anticipated. Covered by `TestImportHandlers_NoWorker` and verified against a live server with no Instagram credentials.
> 3. Replaced the literal `"2006-01-02T15:04:05Z07:00"` with the identical `time.RFC3339`.

# Task 17: Import Trigger & Status Handlers

**Files:**
- Create: `internal/api/handlers_import.go`
- Test: `internal/api/handlers_import_test.go`

**Interfaces:**
- Consumes: `Deps`, `writeJSON` (Task 14/15); `pipeline.Worker`, `pipeline.ImportResult` (Task 13).
- Produces: `Deps.handleImportRun`, `Deps.handleImportStatus` (already referenced by Task 14's router).

- [x] **Step 1: Write the failing test**

```go
// internal/api/handlers_import_test.go
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
```

`Extractor: (*extraction.HybridExtractor)(nil)` is safe here only because `noopFetcher.FetchNewPosts` returns zero posts, so the pipeline loop never reaches `Extract` — no nil-pointer call actually happens.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/... -run TestImportHandlers -v`
Expected: FAIL — `handleImportRun` undefined.

- [x] **Step 3: Implement**

```go
// internal/api/handlers_import.go
package api

import "net/http"

func (d Deps) handleImportRun(w http.ResponseWriter, r *http.Request) {
	go d.Worker.RunOnce(r.Context())
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (d Deps) handleImportStatus(w http.ResponseWriter, r *http.Request) {
	lastRun, lastResult, lastErr, running := d.Worker.Status()
	resp := map[string]any{
		"running":  running,
		"last_run": lastRun.Format("2006-01-02T15:04:05Z07:00"),
		"seen":     lastResult.Seen,
		"imported": lastResult.Imported,
		"skipped":  lastResult.Skipped,
		"failed":   lastResult.Failed,
	}
	if lastErr != nil {
		resp["error"] = lastErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}
```

`handleImportRun` triggers the run in a background goroutine and returns immediately with 202 Accepted — matching PROJECT.md's requirement that extraction/storage need not be real-time, while the recipe-display API stays fast and unaffected by an in-flight import.

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: PASS (full `internal/api` suite: router, recipe handlers, lookup handlers, import handlers)

- [x] **Step 5: Commit**

```bash
git add internal/api/handlers_import.go internal/api/handlers_import_test.go
git commit -m "$(cat <<'EOF'
feat: add import trigger and status HTTP handlers

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 16](16-lookup-handlers-categories-units-ingredients.md) · [Task 18 →](18-main-go-wiring-graceful-shutdown.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
