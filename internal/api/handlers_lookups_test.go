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
