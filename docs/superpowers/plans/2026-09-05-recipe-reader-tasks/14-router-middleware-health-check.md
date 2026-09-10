> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 5: REST API.
>
> **Status:** [x] done
>
> **Corrections made during implementation:**
> 1. **Step 6's "Expected: PASS" is unreachable as written.** `router.go` registers handler methods created in Tasks 15–17, so `internal/api` does not compile at the end of this task. The build was run after each task (undefined-handler count 10 → 5 → 2 → 0) and the suite only after Task 17.
> 2. **Step 1's `router_test.go` does not compile.** The bare type assertion `router.(interface{ ServeHTTP(...) })` is used as a statement; Go allows only calls and receives there. Removed (it was also redundant — `http.Handler` already has that method).
> 3. **Step 1's health-body assertion is wrong.** `writeJSON` uses `json.Encoder.Encode`, which appends a newline, so the body is `"{\"status\":\"ok\"}\n"`.

# Task 14: Router, Middleware & Health Check

**Files:**
- Create: `internal/api/router.go`, `internal/api/middleware.go`
- Test: `internal/api/router_test.go`

**Interfaces:**
- Produces:

```go
type Deps struct {
	Recipes repository.RecipeRepository
	Lookups repository.LookupRepository
	Worker  *pipeline.Worker
}

func NewRouter(deps Deps) http.Handler   // wraps the ServeMux with logging + recovery + CORS middleware
```

Task 15-17 add handler methods on `Deps`; Task 18 (`main.go`) is the only caller of `NewRouter`.

- [x] **Step 1: Write the failing test**

```go
// internal/api/router_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouter_Health(t *testing.T) {
	router := NewRouter(Deps{})
	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestRouter_RecoversFromPanic(t *testing.T) {
	router := NewRouter(Deps{})
	router.(interface {
		ServeHTTP(http.ResponseWriter, *http.Request)
	})
	// A handler that panics must produce a 500, not crash the process.
	// handleListRecipes will panic on a nil Deps.Recipes — exercised here
	// deliberately to prove the recovery middleware works before any real
	// handler logic exists (Task 15 replaces the nil-panic with a real 500).
	req := httptest.NewRequest(http.MethodGet, "/api/recipes", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (recovered panic)", rec.Code)
	}
}

func TestRouter_SetsCORSHeaders(t *testing.T) {
	router := NewRouter(Deps{})
	req := httptest.NewRequest(http.MethodOptions, "/api/healthz", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "" {
		t.Error("expected Access-Control-Allow-Origin header to be set")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/... -v`
Expected: FAIL — package doesn't exist.

- [x] **Step 3: Implement `middleware.go`**

```go
// internal/api/middleware.go
package api

import (
	"log/slog"
	"net/http"
	"time"
)

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "error", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

- [x] **Step 4: Implement `router.go`**

```go
// internal/api/router.go
package api

import (
	"net/http"

	"github.com/sBurmester/recipe-reader/internal/pipeline"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

type Deps struct {
	Recipes repository.RecipeRepository
	Lookups repository.LookupRepository
	Worker  *pipeline.Worker
}

func NewRouter(deps Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/healthz", handleHealth)

	mux.HandleFunc("GET /api/recipes", deps.handleListRecipes)
	mux.HandleFunc("POST /api/recipes", deps.handleCreateRecipe)
	mux.HandleFunc("GET /api/recipes/{id}", deps.handleGetRecipe)
	mux.HandleFunc("PUT /api/recipes/{id}", deps.handleUpdateRecipe)
	mux.HandleFunc("DELETE /api/recipes/{id}", deps.handleDeleteRecipe)

	mux.HandleFunc("GET /api/categories", deps.handleListCategories)
	mux.HandleFunc("GET /api/units", deps.handleListUnits)
	mux.HandleFunc("GET /api/ingredients", deps.handleListIngredients)

	mux.HandleFunc("POST /api/import/run", deps.handleImportRun)
	mux.HandleFunc("GET /api/import/status", deps.handleImportStatus)

	return withCORS(withRecovery(withLogging(mux)))
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [x] **Step 5: Add the shared JSON helpers**

These are used by every handler task that follows (15-17); put them in `internal/api/dto.go`'s preamble now so Task 15 doesn't need to touch this file again.

```go
// internal/api/dto.go (created fully in Task 15; this is the shared preamble)
package api

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: PASS (`TestRouter_Health`, `TestRouter_RecoversFromPanic` — nil `Deps.Recipes` in `handleListRecipes` will panic until Task 15 adds real handlers, which the recovery middleware turns into a 500 — and `TestRouter_SetsCORSHeaders`).

- [x] **Step 7: Commit**

```bash
git add internal/api/router.go internal/api/middleware.go internal/api/dto.go internal/api/router_test.go
git commit -m "$(cat <<'EOF'
feat: add HTTP router with logging, panic recovery, and CORS middleware

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 13](13-background-worker-scheduled-manual-trigger.md) · [Task 15 →](15-recipe-handlers-crud-search.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
