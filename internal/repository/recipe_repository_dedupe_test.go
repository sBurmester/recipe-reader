package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
	"github.com/sBurmester/recipe-reader/internal/domain"
)

// The unique index on source is the dedupe now, not a GetBySource that ran
// before it. A second insert of the same source must come back as a sentinel
// the caller can branch on rather than as a driver error nobody can classify.
func TestRecipeRepository_Create_DuplicateSourceIsASentinel(t *testing.T) {
	ctx := context.Background()
	// One pool for both repositories: testdb.New empties the database on every
	// call, so taking it twice in one test would wipe what the first half wrote.
	pool := testdb.New(t)
	repo := NewRecipeRepository(pool)
	lookups := NewLookupRepository(pool)

	first := &domain.Recipe{Name: "Pfannkuchen", Source: "src-dup", Status: domain.StatusPublished}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	second := &domain.Recipe{
		Name: "Pfannkuchen, nochmal", Source: "src-dup", Status: domain.StatusPublished,
		Categories:  []domain.Category{{Name: "Waise-Kategorie"}},
		Ingredients: []domain.RecipeIngredient{{IngredientName: "Waise-Zutat", Amount: 1, UnitName: "Waise-Einheit"}},
	}
	err := repo.Create(ctx, second)
	if !errors.Is(err, ErrDuplicateSource) {
		t.Fatalf("Create() error = %v, want ErrDuplicateSource", err)
	}

	// The conflict fires inside the transaction, before resolveLookups runs, so
	// the rejected recipe leaves no lookup rows behind either.
	categories, err := lookups.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories() error = %v", err)
	}
	ingredients, err := lookups.ListIngredients(ctx)
	if err != nil {
		t.Fatalf("ListIngredients() error = %v", err)
	}
	if len(categories) != 0 || len(ingredients) != 0 {
		t.Errorf("rejected create left lookup rows: categories=%+v ingredients=%+v", categories, ingredients)
	}

	// And the row that was already there is untouched.
	stored, err := repo.GetBySource(ctx, "src-dup")
	if err != nil {
		t.Fatalf("GetBySource() error = %v", err)
	}
	if stored.Name != "Pfannkuchen" {
		t.Errorf("stored name = %q, want the first insert's Pfannkuchen", stored.Name)
	}
}
