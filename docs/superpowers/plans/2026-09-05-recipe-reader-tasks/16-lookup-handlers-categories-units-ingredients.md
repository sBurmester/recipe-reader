> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 5: REST API.
>
> **Status:** [ ] not started

# Task 16: Lookup Handlers (Categories, Units, Ingredients)

**Files:**
- Modify: `internal/api/dto.go` (add `UnitDTO`, `IngredientLookupDTO`)
- Create: `internal/api/handlers_lookups.go`
- Test: `internal/api/handlers_lookups_test.go`

**Interfaces:**
- Consumes: `Deps`, `writeJSON` (Task 14/15); `repository.LookupRepository` (Task 5); `CategoryDTO` (Task 15).
- Produces: `Deps.handleListCategories`, `Deps.handleListUnits`, `Deps.handleListIngredients` (already referenced by Task 14's router).

- [ ] **Step 1: Write the failing test**

```go
// internal/api/handlers_lookups_test.go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookupHandlers_ListCategoriesUnitsIngredients(t *testing.T) {
	deps := newTestDeps(t)
	ctx := context.Background()
	if _, err := deps.Lookups.FindOrCreateCategory(ctx, "Dessert"); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	if _, err := deps.Lookups.FindOrCreateUnit(ctx, "g"); err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	if _, err := deps.Lookups.FindOrCreateIngredient(ctx, "Mehl"); err != nil {
		t.Fatalf("seed ingredient: %v", err)
	}
	router := NewRouter(deps)

	for path, want := range map[string]int{
		"/api/categories":  1,
		"/api/units":       1,
		"/api/ingredients": 1,
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
		var items []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("%s unmarshal: %v", path, err)
		}
		if len(items) != want {
			t.Errorf("%s len = %d, want %d", path, len(items), want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/... -run TestLookupHandlers -v`
Expected: FAIL — `handleListCategories` etc. undefined (router already references them per Task 14, so this is a compile error until they exist).

- [ ] **Step 3: Extend `dto.go`**

```go
type UnitDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type IngredientLookupDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
```

- [ ] **Step 4: Implement handlers**

```go
// internal/api/handlers_lookups.go
package api

import "net/http"

func (d Deps) handleListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := d.Lookups.ListCategories(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list categories")
		return
	}
	dtos := make([]CategoryDTO, 0, len(categories))
	for _, c := range categories {
		dtos = append(dtos, CategoryDTO{ID: c.ID, Name: c.Name})
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (d Deps) handleListUnits(w http.ResponseWriter, r *http.Request) {
	units, err := d.Lookups.ListUnits(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list units")
		return
	}
	dtos := make([]UnitDTO, 0, len(units))
	for _, u := range units {
		dtos = append(dtos, UnitDTO{ID: u.ID, Name: u.Name})
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (d Deps) handleListIngredients(w http.ResponseWriter, r *http.Request) {
	ingredients, err := d.Lookups.ListIngredients(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list ingredients")
		return
	}
	dtos := make([]IngredientLookupDTO, 0, len(ingredients))
	for _, i := range ingredients {
		dtos = append(dtos, IngredientLookupDTO{ID: i.ID, Name: i.Name})
	}
	writeJSON(w, http.StatusOK, dtos)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/dto.go internal/api/handlers_lookups.go internal/api/handlers_lookups_test.go
git commit -m "$(cat <<'EOF'
feat: add category/unit/ingredient lookup HTTP handlers

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 15](15-recipe-handlers-crud-search.md) · [Task 17 →](17-import-trigger-status-handlers.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
