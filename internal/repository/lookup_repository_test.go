// internal/repository/lookup_repository_test.go
package repository

import (
	"context"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

func newTestLookupRepo(t *testing.T) LookupRepository {
	t.Helper()
	return NewLookupRepository(testdb.New(t))
}

func TestLookupRepository_FindOrCreate_Idempotent(t *testing.T) {
	ctx := context.Background()
	repo := newTestLookupRepo(t)

	first, err := repo.FindOrCreateIngredient(ctx, "Zucker")
	if err != nil {
		t.Fatalf("FindOrCreateIngredient() error = %v", err)
	}
	second, err := repo.FindOrCreateIngredient(ctx, "Zucker")
	if err != nil {
		t.Fatalf("FindOrCreateIngredient() second call error = %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("expected same ID, got %d and %d", first.ID, second.ID)
	}

	ingredients, err := repo.ListIngredients(ctx)
	if err != nil {
		t.Fatalf("ListIngredients() error = %v", err)
	}
	if len(ingredients) != 1 {
		t.Errorf("len(ingredients) = %d, want 1", len(ingredients))
	}
}

func TestLookupRepository_FindOrCreateUnitAndCategory(t *testing.T) {
	ctx := context.Background()
	repo := newTestLookupRepo(t)

	unit, err := repo.FindOrCreateUnit(ctx, "EL")
	if err != nil || unit.Name != "EL" {
		t.Fatalf("FindOrCreateUnit() = %+v, err = %v", unit, err)
	}
	cat, err := repo.FindOrCreateCategory(ctx, "Dessert")
	if err != nil || cat.Name != "Dessert" {
		t.Fatalf("FindOrCreateCategory() = %+v, err = %v", cat, err)
	}

	units, err := repo.ListUnits(ctx)
	if err != nil || len(units) != 1 {
		t.Fatalf("ListUnits() = %+v, err = %v", units, err)
	}
	cats, err := repo.ListCategories(ctx)
	if err != nil || len(cats) != 1 {
		t.Fatalf("ListCategories() = %+v, err = %v", cats, err)
	}
}
