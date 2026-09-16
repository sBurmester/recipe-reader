package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
	"github.com/sBurmester/recipe-reader/internal/domain"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

// testSecurity is the loopback deployment: no token, so these tests exercise
// the handlers rather than the auth check. The token path has its own tests in
// middleware_test.go.
var testSecurity = Security{AllowedOrigins: []string{"http://localhost:5173"}}

// jsonRequest builds a request the write guards accept. Every state-changing
// route now requires Content-Type: application/json — that header is not
// CORS-"simple", which is what forces a cross-origin write to be preflighted
// and so brings it under the origin allowlist.
func jsonRequest(method, path string, body []byte) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

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
	router := NewRouter(deps, testSecurity)

	createBody, err := json.Marshal(RecipeDTO{
		Name:         "Pfannkuchen",
		Instructions: "Backen.",
		Source:       "src-1",
		Status:       string(domain.StatusPublished),
		Ingredients:  []IngredientDTO{{Name: "Mehl", Amount: 200, Unit: "g"}},
	})
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}
	req := jsonRequest(http.MethodPost, "/api/recipes", createBody)
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
	if len(created.Ingredients) != 1 || created.Ingredients[0].Name != "Mehl" || created.Ingredients[0].Unit != "g" {
		t.Errorf("create response ingredients = %+v, want the submitted name/unit echoed back", created.Ingredients)
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
	updateBody, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("marshal update body: %v", err)
	}
	updatePath := fmt.Sprintf("/api/recipes/%d", created.ID)
	req = jsonRequest(http.MethodPut, updatePath, updateBody)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, updatePath, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	var fetched RecipeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("unmarshal get response: %v", err)
	}
	if fetched.Name != "Pfannkuchen (süß)" {
		t.Errorf("name after update = %q, want the updated name", fetched.Name)
	}

	req = jsonRequest(http.MethodDelete, updatePath, nil)
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
	router := NewRouter(deps, testSecurity)

	for _, name := range []string{"Apfelkuchen", "Bananenbrot"} {
		body, err := json.Marshal(RecipeDTO{Name: name, Source: "src-" + name, Status: string(domain.StatusPublished)})
		if err != nil {
			t.Fatalf("marshal %q: %v", name, err)
		}
		req := jsonRequest(http.MethodPost, "/api/recipes", body)
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
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if listResp.Total != 1 {
		t.Errorf("total = %d, want 1", listResp.Total)
	}
}

func TestRecipeHandlers_CreateValidation(t *testing.T) {
	deps := newTestDeps(t)
	router := NewRouter(deps, testSecurity)

	body, err := json.Marshal(RecipeDTO{})
	if err != nil {
		t.Fatalf("marshal empty body: %v", err)
	}
	req := jsonRequest(http.MethodPost, "/api/recipes", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing name/source", rec.Code)
	}
}

// recipeBodyOfSize returns valid recipe JSON of exactly n bytes, padded out
// through the instructions field and carrying no name or source.
func recipeBodyOfSize(n int) []byte {
	const prefix, suffix = `{"instructions":"`, `"}`
	body := make([]byte, 0, n)
	body = append(body, prefix...)
	body = append(body, bytes.Repeat([]byte("x"), n-len(prefix)-len(suffix))...)
	return append(body, suffix...)
}

// A body one byte over the cap is refused as too large, not as malformed — it
// is valid JSON, just more of it than a recipe needs. These run against empty
// Deps with no database: the cap is enforced before any repository is touched,
// and a request that slipped past it would panic on a nil repository and come
// back 500, not 413.
func TestRecipeHandlers_OversizedBodyIs413(t *testing.T) {
	router := NewRouter(Deps{}, testSecurity)
	body := recipeBodyOfSize(maxRecipeBodyBytes + 1)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/recipes"},
		{http.MethodPut, "/api/recipes/1"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, jsonRequest(tc.method, tc.path, body))
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("status = %d, want 413; body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// The cap is inclusive: a body of exactly maxRecipeBodyBytes still decodes and
// reaches the handler's own validation, which rejects it for the missing name
// and source — a 400 that proves the decoder accepted it.
func TestRecipeHandlers_BodyAtTheLimitIsDecoded(t *testing.T) {
	router := NewRouter(Deps{}, testSecurity)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/recipes", recipeBodyOfSize(maxRecipeBodyBytes)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 from validation; body = %s", rec.Code, rec.Body.String())
	}
	if want := "name and source are required"; !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
		t.Errorf("body = %s, want the validation error %q rather than a decode error", rec.Body.String(), want)
	}
}

// A status outside the domain's two values used to persist on both write
// paths, and a PUT without a name or status stored empty strings. Validation
// runs before any repository call, so these need no database: a request that
// slipped past it would panic on a nil repository and come back 500.
func TestRecipeHandlers_RejectOutOfDomainWrites(t *testing.T) {
	router := NewRouter(Deps{}, testSecurity)

	for _, tc := range []struct {
		name, method, path string
		dto                RecipeDTO
	}{
		{"POST bogus status", http.MethodPost, "/api/recipes", RecipeDTO{Name: "x", Source: "s", Status: "banana"}},
		{"PUT bogus status", http.MethodPut, "/api/recipes/1", RecipeDTO{Name: "x", Status: "banana"}},
		{"PUT empty status", http.MethodPut, "/api/recipes/1", RecipeDTO{Name: "x"}},
		{"PUT empty name", http.MethodPut, "/api/recipes/1", RecipeDTO{Status: string(domain.StatusPublished)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.dto)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, jsonRequest(tc.method, tc.path, body))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// Create still defaults an omitted status rather than rejecting it — the
// validation above runs on the defaulted value, not on the raw field.
func TestRecipeHandlers_CreateDefaultsOmittedStatusToPublished(t *testing.T) {
	router := NewRouter(newTestDeps(t), testSecurity)

	body, err := json.Marshal(RecipeDTO{Name: "Brot", Source: "src-default-status"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/recipes", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	var created RecipeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.Status != string(domain.StatusPublished) {
		t.Errorf("status = %q, want %q", created.Status, domain.StatusPublished)
	}
}

// persistence P2's scenario end to end. The second POST fails on the duplicate
// source, and the ingredient, unit and category it named must not outlive it.
// They used to: the handler resolved them on the pool before the write began,
// so they were committed before the insert could fail.
func TestRecipeHandlers_FailedCreateLeavesNoOrphanLookups(t *testing.T) {
	deps := newTestDeps(t)
	router := NewRouter(deps, testSecurity)

	post := func(dto RecipeDTO) int {
		t.Helper()
		body, err := json.Marshal(dto)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/recipes", body))
		return rec.Code
	}

	if code := post(RecipeDTO{Name: "Erstes", Source: "src-dup"}); code != http.StatusCreated {
		t.Fatalf("first create status = %d, want 201", code)
	}
	code := post(RecipeDTO{
		Name: "Zweites", Source: "src-dup",
		Categories:  []CategoryDTO{{Name: "Orphan-Kategorie"}},
		Ingredients: []IngredientDTO{{Name: "Orphan-Zutat", Amount: 1, Unit: "Orphan-Einheit"}},
	})
	// 409, not 500: the duplicate is the unique index on source firing inside
	// CreateRecipe's ON CONFLICT clause, which the repository reports as
	// ErrDuplicateSource rather than as an opaque write failure.
	if code != http.StatusConflict {
		t.Fatalf("duplicate-source create status = %d, want 409", code)
	}

	ctx := t.Context()
	categories, err := deps.Lookups.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories() error = %v", err)
	}
	ingredients, err := deps.Lookups.ListIngredients(ctx)
	if err != nil {
		t.Fatalf("ListIngredients() error = %v", err)
	}
	units, err := deps.Lookups.ListUnits(ctx)
	if err != nil {
		t.Fatalf("ListUnits() error = %v", err)
	}
	if len(categories) != 0 || len(ingredients) != 0 || len(units) != 0 {
		t.Errorf("lookup rows left behind by the failed create: categories=%+v ingredients=%+v units=%+v",
			categories, ingredients, units)
	}
}
