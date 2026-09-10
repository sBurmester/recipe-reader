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
	req = httptest.NewRequest(http.MethodPut, updatePath, bytes.NewReader(updateBody))
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
		body, err := json.Marshal(RecipeDTO{Name: name, Source: "src-" + name, Status: string(domain.StatusPublished)})
		if err != nil {
			t.Fatalf("marshal %q: %v", name, err)
		}
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
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if listResp.Total != 1 {
		t.Errorf("total = %d, want 1", listResp.Total)
	}
}

func TestRecipeHandlers_CreateValidation(t *testing.T) {
	deps := newTestDeps(t)
	router := NewRouter(deps)

	body, err := json.Marshal(RecipeDTO{})
	if err != nil {
		t.Fatalf("marshal empty body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/recipes", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing name/source", rec.Code)
	}
}
