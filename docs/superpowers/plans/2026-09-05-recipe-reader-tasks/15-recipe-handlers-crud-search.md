> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 5: REST API.
>
> **Status:** [x] done
>
> **Corrections made during implementation:**
> 1. **`dtoToRecipe` dropped ingredient and unit names on the wire (real bug, fixed).** It populated only the write-side `IngredientID`/`UnitID`, but `toRecipeDTO` reads the read-side `IngredientName`/`UnitName`, so a successful create returned `"ingredients":[{"name":"","amount":200,"unit":""}]`. Now copies `ingredient.Name`/`unit.Name` across as well — free, since `repository.writeAssociations` only reads the ID and Amount fields, and `domain.RecipeIngredient` is documented as deliberately carrying both sides. Verified end-to-end against a live server.
> 2. **`json.Unmarshal`'s return was ignored in the search test.** errcheck is enabled and does inspect `_test.go` files, so this would have failed lint. Now checked.
> 3. **The update test never verified the update took effect** — it PUT and checked only the status code. A GET assertion was added between the PUT and DELETE so a no-op `Update` would fail the test.
>
> **Found later, while building the frontend (Phase 6):**
>
> 4. **`toRecipeDTO` marshalled empty associations as `null`, crashing the UI (real bug, fixed in the Phase 6 branch).** `Ingredients` and `Categories` were built by appending to nil slices, so a recipe with no categories or no ingredients serialized them as `null` rather than `[]`. The frontend types them as arrays and calls `.map` directly, so the list page died with `Cannot read properties of null (reading 'map')` — and a recipe with no categories is entirely ordinary, since the rule-based extractor often finds none. `handleListRecipes` already applied exactly this principle one level up ("An empty result must marshal as [] rather than null") — it just wasn't applied to the nested slices. Both are now allocated with `make(..., 0, len(...))`, covered by `TestToRecipeDTO_EmptyAssociationsMarshalAsArrays`, and confirmed in a real browser.
> 5. **Open follow-up — `handleUpdateRecipe` performs no validation.** `handleCreateRecipe` rejects an empty `name` or `source`, but the update handler checks neither, and PUT is a full replace. Blanking `source` destroys the import dedup key, so the pipeline would re-import that post as a new recipe on every run. The Phase 6 detail page guards this client-side, but the API remains permissive for any other caller. Not fixed, to keep a Phase 6 branch from rewriting merged Phase 5 behaviour — worth a small follow-up applying the same check the create handler already has.

# Task 15: Recipe Handlers (CRUD + Search)

**Files:**
- Create: `internal/api/dto.go` (shared JSON helpers + DTOs)
- Create: `internal/api/handlers_recipes.go`
- Test: `internal/api/handlers_recipes_test.go`

**Interfaces:**
- Consumes: `Deps` (Task 14); `repository.RecipeRepository`, `repository.LookupRepository`, `repository.SearchQuery`, `repository.ErrNotFound` (Task 4/5); `domain.Recipe`, `domain.RecipeIngredient`, `domain.Category`, `domain.RecipeStatus` (Task 3); `testdb.New` (Task 3).
- Produces: `writeJSON`, `writeError` helpers; `RecipeDTO`, `IngredientDTO`, `CategoryDTO` (JSON shape for the frontend — Task 19's `types.ts` mirrors these field names exactly; IDs are JSON numbers either way, so the Go `int64` vs the old `uint` makes no difference on the wire), `toRecipeDTO(domain.Recipe) RecipeDTO`, and the five `Deps.handle*Recipe*` methods wired in Task 14's router.

- [x] **Step 1: Write the failing test**

```go
// internal/api/handlers_recipes_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
	"github.com/sBurmester/recipe-reader/internal/domain"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

func newTestDeps(t *testing.T) Deps {
	t.Helper()
	pool := testdb.New(t)
	return Deps{
		Recipes: repository.NewRecipeRepository(pool),
		Lookups: repository.NewLookupRepository(pool),
	}
}

func TestRecipeHandlers_CreateGetListUpdateDelete(t *testing.T) {
	deps := newTestDeps(t)
	router := NewRouter(deps)

	createBody, _ := json.Marshal(RecipeDTO{
		Name:         "Pfannkuchen",
		Instructions: "Backen.",
		Source:       "src-1",
		Status:       string(domain.StatusPublished),
		Ingredients:  []IngredientDTO{{Name: "Mehl", Amount: 200, Unit: "g"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/recipes", bytes.NewReader(createBody))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created RecipeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/recipes", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var listResp struct {
		Recipes []RecipeDTO `json:"recipes"`
		Total   int64       `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if listResp.Total != 1 || len(listResp.Recipes) != 1 {
		t.Fatalf("list response = %+v", listResp)
	}

	created.Name = "Pfannkuchen (süß)"
	updateBody, _ := json.Marshal(created)
	updatePath := fmt.Sprintf("/api/recipes/%d", created.ID)
	req = httptest.NewRequest(http.MethodPut, updatePath, bytes.NewReader(updateBody))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, updatePath, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, updatePath, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d, want 404", rec.Code)
	}
}

func TestRecipeHandlers_ListSearchQueryParams(t *testing.T) {
	deps := newTestDeps(t)
	router := NewRouter(deps)

	for _, name := range []string{"Apfelkuchen", "Bananenbrot"} {
		body, _ := json.Marshal(RecipeDTO{Name: name, Source: "src-" + name, Status: string(domain.StatusPublished)})
		req := httptest.NewRequest(http.MethodPost, "/api/recipes", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %q status = %d", name, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/recipes?q=apfel", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var listResp struct {
		Recipes []RecipeDTO `json:"recipes"`
		Total   int64       `json:"total"`
	}
	json.Unmarshal(rec.Body.Bytes(), &listResp)
	if listResp.Total != 1 {
		t.Errorf("total = %d, want 1", listResp.Total)
	}
}

func TestRecipeHandlers_CreateValidation(t *testing.T) {
	deps := newTestDeps(t)
	router := NewRouter(deps)

	body, _ := json.Marshal(RecipeDTO{})
	req := httptest.NewRequest(http.MethodPost, "/api/recipes", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing name/source", rec.Code)
	}
}
```

Add `"fmt"` to this test file's imports (used by `fmt.Sprintf` above).

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/... -run TestRecipeHandlers -v`
Expected: FAIL — `RecipeDTO` undefined.

- [x] **Step 3: Implement `dto.go`**

```go
// internal/api/dto.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/sBurmester/recipe-reader/internal/domain"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type IngredientDTO struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

type CategoryDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type RecipeDTO struct {
	ID           int64           `json:"id"`
	Name         string          `json:"name"`
	Instructions string          `json:"instructions"`
	ImageURL     string          `json:"image_url"`
	Source       string          `json:"source"`
	Status       string          `json:"status"`
	Ingredients  []IngredientDTO `json:"ingredients"`
	Categories   []CategoryDTO   `json:"categories"`
}

func toRecipeDTO(r domain.Recipe) RecipeDTO {
	dto := RecipeDTO{
		ID: r.ID, Name: r.Name, Instructions: r.Instructions,
		ImageURL: r.ImageURL, Source: r.Source, Status: string(r.Status),
	}
	for _, ri := range r.Ingredients {
		dto.Ingredients = append(dto.Ingredients, IngredientDTO{
			Name: ri.IngredientName, Amount: ri.Amount, Unit: ri.UnitName,
		})
	}
	for _, c := range r.Categories {
		dto.Categories = append(dto.Categories, CategoryDTO{ID: c.ID, Name: c.Name})
	}
	return dto
}
```

- [x] **Step 4: Implement handlers**

```go
// internal/api/handlers_recipes.go
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/sBurmester/recipe-reader/internal/domain"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

func (d Deps) handleListRecipes(w http.ResponseWriter, r *http.Request) {
	q := repository.SearchQuery{Text: r.URL.Query().Get("q")}
	if v := r.URL.Query().Get("category_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.CategoryID = &id
		}
	}
	if v := r.URL.Query().Get("status"); v != "" {
		status := domain.RecipeStatus(v)
		q.Status = &status
	}
	if v := r.URL.Query().Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			q.Page = p
		}
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil {
			q.PageSize = ps
		}
	}

	recipes, total, err := d.Recipes.Search(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	dtos := make([]RecipeDTO, 0, len(recipes))
	for _, rec := range recipes {
		dtos = append(dtos, toRecipeDTO(rec))
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipes": dtos, "total": total})
}

func (d Deps) handleGetRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	recipe, err := d.Recipes.GetByID(r.Context(), id)
	if errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get failed")
		return
	}
	writeJSON(w, http.StatusOK, toRecipeDTO(*recipe))
}

func (d Deps) handleCreateRecipe(w http.ResponseWriter, r *http.Request) {
	var dto RecipeDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if dto.Name == "" || dto.Source == "" {
		writeError(w, http.StatusBadRequest, "name and source are required")
		return
	}
	if dto.Status == "" {
		dto.Status = string(domain.StatusPublished)
	}

	recipe, err := d.dtoToRecipe(r, dto)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve ingredients/categories")
		return
	}
	if err := d.Recipes.Create(r.Context(), recipe); err != nil {
		writeError(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, toRecipeDTO(*recipe))
}

func (d Deps) handleUpdateRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var dto RecipeDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	recipe, err := d.dtoToRecipe(r, dto)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve ingredients/categories")
		return
	}
	recipe.ID = id
	if err := d.Recipes.Update(r.Context(), recipe); errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, toRecipeDTO(*recipe))
}

func (d Deps) handleDeleteRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := d.Recipes.Delete(r.Context(), id); errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) dtoToRecipe(r *http.Request, dto RecipeDTO) (*domain.Recipe, error) {
	recipe := &domain.Recipe{
		Name: dto.Name, Instructions: dto.Instructions, ImageURL: dto.ImageURL,
		Source: dto.Source, Status: domain.RecipeStatus(dto.Status),
	}
	for _, cat := range dto.Categories {
		c, err := d.Lookups.FindOrCreateCategory(r.Context(), cat.Name)
		if err != nil {
			return nil, err
		}
		recipe.Categories = append(recipe.Categories, *c)
	}
	for _, ing := range dto.Ingredients {
		ingredient, err := d.Lookups.FindOrCreateIngredient(r.Context(), ing.Name)
		if err != nil {
			return nil, err
		}
		ri := domain.RecipeIngredient{IngredientID: ingredient.ID, Amount: ing.Amount}
		if ing.Unit != "" {
			unit, err := d.Lookups.FindOrCreateUnit(r.Context(), ing.Unit)
			if err != nil {
				return nil, err
			}
			ri.UnitID = &unit.ID
		}
		recipe.Ingredients = append(recipe.Ingredients, ri)
	}
	return recipe, nil
}

func parseIDParam(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}
```

- [x] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/... -v` (needs Docker running)
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add internal/api/dto.go internal/api/handlers_recipes.go internal/api/handlers_recipes_test.go
git commit -m "$(cat <<'EOF'
feat: add recipe CRUD and search HTTP handlers

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 14](14-router-middleware-health-check.md) · [Task 16 →](16-lookup-handlers-categories-units-ingredients.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
