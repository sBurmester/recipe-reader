package repository

import (
	"context"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
	"github.com/sBurmester/recipe-reader/internal/domain"
)

// Associations given by name are found or created inside the write, and the
// caller's value comes back carrying the ids they resolved to.
func TestRecipeRepository_Create_ResolvesLookupsByName(t *testing.T) {
	ctx := context.Background()
	repo := NewRecipeRepository(testdb.New(t))

	r := &domain.Recipe{
		Name: "Brot", Source: "src-brot", Status: domain.StatusPublished,
		Categories: []domain.Category{{Name: "Backen"}},
		Ingredients: []domain.RecipeIngredient{
			{IngredientName: "Mehl", Amount: 500, UnitName: "g"},
			{IngredientName: "Salz", Amount: 1},
		},
	}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if r.Categories[0].ID == 0 || r.Ingredients[0].IngredientID == 0 || r.Ingredients[0].UnitID == nil {
		t.Errorf("caller's recipe not updated with resolved ids: %+v", r)
	}
	if r.Ingredients[1].UnitID != nil {
		t.Errorf("Ingredients[1].UnitID = %v, want nil for an ingredient with no unit", *r.Ingredients[1].UnitID)
	}

	loaded, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if len(loaded.Ingredients) != 2 ||
		loaded.Ingredients[0].IngredientName != "Mehl" || loaded.Ingredients[0].UnitName != "g" ||
		loaded.Ingredients[1].IngredientName != "Salz" || loaded.Ingredients[1].UnitName != "" {
		t.Errorf("Ingredients = %+v", loaded.Ingredients)
	}
	if len(loaded.Categories) != 1 || loaded.Categories[0].Name != "Backen" {
		t.Errorf("Categories = %+v", loaded.Categories)
	}
}

func TestRecipeRepository_Update_ResolvesLookupsByName(t *testing.T) {
	ctx := context.Background()
	repo := NewRecipeRepository(testdb.New(t))

	r := &domain.Recipe{Name: "Suppe", Source: "src-suppe", Status: domain.StatusPublished}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	r.Ingredients = []domain.RecipeIngredient{{IngredientName: "Pfeffer", Amount: 1, UnitName: "Prise"}}
	r.Categories = []domain.Category{{Name: "Vorspeise"}}
	if err := repo.Update(ctx, r); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	loaded, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if len(loaded.Ingredients) != 1 || loaded.Ingredients[0].IngredientName != "Pfeffer" || loaded.Ingredients[0].UnitName != "Prise" {
		t.Errorf("Ingredients = %+v", loaded.Ingredients)
	}
	if len(loaded.Categories) != 1 || loaded.Categories[0].Name != "Vorspeise" {
		t.Errorf("Categories = %+v", loaded.Categories)
	}
}

// The finding: lookup rows created for a write that then fails must not
// survive it. The failure here is deliberately placed *after* resolution — a
// unit id that does not exist trips the foreign key when the ingredient row is
// written — so the category and ingredient have already been created by name
// when the write fails, and only the transaction can take them back.
func TestRecipeRepository_FailedWrite_LeavesNoOrphanLookups(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	repo := NewRecipeRepository(pool)
	lookups := NewLookupRepository(pool)

	missingUnit := int64(999_999)
	r := &domain.Recipe{
		Name: "Kaputt", Source: "src-kaputt", Status: domain.StatusPublished,
		Categories:  []domain.Category{{Name: "Orphan-Kategorie"}},
		Ingredients: []domain.RecipeIngredient{{IngredientName: "Orphan-Zutat", Amount: 1, UnitID: &missingUnit}},
	}
	if err := repo.Create(ctx, r); err == nil {
		t.Fatal("Create() error = nil, want a foreign-key violation on the missing unit")
	}

	if r.ID != 0 || r.Categories[0].ID != 0 || r.Ingredients[0].IngredientID != 0 {
		t.Errorf("caller's recipe was modified by a failed write: %+v", r)
	}

	categories, err := lookups.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories() error = %v", err)
	}
	for _, c := range categories {
		if c.Name == "Orphan-Kategorie" {
			t.Error("category created for the failed write survived it")
		}
	}
	ingredients, err := lookups.ListIngredients(ctx)
	if err != nil {
		t.Fatalf("ListIngredients() error = %v", err)
	}
	for _, i := range ingredients {
		if i.Name == "Orphan-Zutat" {
			t.Error("ingredient created for the failed write survived it")
		}
	}
}
