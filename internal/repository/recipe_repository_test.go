package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
	"github.com/sBurmester/recipe-reader/internal/domain"
)

func newTestRecipeRepo(t *testing.T) RecipeRepository {
	t.Helper()
	pool := testdb.New(t)
	return NewRecipeRepository(pool)
}

func TestRecipeRepository_CreateGetUpdateDelete(t *testing.T) {
	ctx := context.Background()
	repo := newTestRecipeRepo(t)

	r := &domain.Recipe{Name: "Pfannkuchen", Source: "src-1", Status: domain.StatusPublished}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if r.ID == 0 {
		t.Fatal("Create() did not populate ID")
	}

	got, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Name != "Pfannkuchen" {
		t.Errorf("Name = %q, want Pfannkuchen", got.Name)
	}

	got.Name = "Pfannkuchen (süß)"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	reloaded, _ := repo.GetByID(ctx, r.ID)
	if reloaded.Name != "Pfannkuchen (süß)" {
		t.Errorf("Name after update = %q", reloaded.Name)
	}

	if err := repo.Delete(ctx, r.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := repo.GetByID(ctx, r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByID() after delete error = %v, want ErrNotFound", err)
	}
}

func TestRecipeRepository_CreateWithIngredientsAndCategories(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	repo := NewRecipeRepository(pool)
	lookups := NewLookupRepository(pool) // same DB — child-row FKs must resolve against it — Task 5

	mehl, err := lookups.FindOrCreateIngredient(ctx, "Mehl")
	if err != nil {
		t.Fatalf("FindOrCreateIngredient() error = %v", err)
	}
	gramm, err := lookups.FindOrCreateUnit(ctx, "g")
	if err != nil {
		t.Fatalf("FindOrCreateUnit() error = %v", err)
	}
	backen, err := lookups.FindOrCreateCategory(ctx, "Backen")
	if err != nil {
		t.Fatalf("FindOrCreateCategory() error = %v", err)
	}

	r := &domain.Recipe{
		Name: "Brot", Source: "src-brot", Status: domain.StatusPublished,
		Categories: []domain.Category{*backen},
		Ingredients: []domain.RecipeIngredient{
			{IngredientID: mehl.ID, Amount: 500, UnitID: &gramm.ID},
		},
	}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	loaded, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if len(loaded.Ingredients) != 1 || loaded.Ingredients[0].IngredientName != "Mehl" || loaded.Ingredients[0].UnitName != "g" {
		t.Errorf("Ingredients = %+v", loaded.Ingredients)
	}
	if len(loaded.Categories) != 1 || loaded.Categories[0].Name != "Backen" {
		t.Errorf("Categories = %+v", loaded.Categories)
	}

	// Duplicate Source must be rejected — this is the pipeline's dedupe key.
	dup := &domain.Recipe{Name: "Dup", Source: "src-brot", Status: domain.StatusPublished}
	if err := repo.Create(ctx, dup); err == nil {
		t.Error("expected a unique constraint violation on duplicate Source, got nil")
	}
}

func TestRecipeRepository_Update_ReplacesIngredientsWithoutDuplicating(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	repo := NewRecipeRepository(pool)
	lookups := NewLookupRepository(pool)

	salz, _ := lookups.FindOrCreateIngredient(ctx, "Salz")
	pfeffer, _ := lookups.FindOrCreateIngredient(ctx, "Pfeffer")

	r := &domain.Recipe{
		Name: "Salat", Source: "src-salat", Status: domain.StatusPublished,
		Ingredients: []domain.RecipeIngredient{{IngredientID: salz.ID, Amount: 1}},
	}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	got.Ingredients = []domain.RecipeIngredient{{IngredientID: pfeffer.ID, Amount: 2}}
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	reloaded, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() after update error = %v", err)
	}
	if len(reloaded.Ingredients) != 1 {
		t.Fatalf("Ingredients after update = %+v, want exactly 1 (old ones must be replaced, not appended)", reloaded.Ingredients)
	}
	if reloaded.Ingredients[0].IngredientName != "Pfeffer" {
		t.Errorf("Ingredients[0] = %+v, want Pfeffer", reloaded.Ingredients[0])
	}
}

func TestRecipeRepository_Update_PreservesIngredientUnitOnRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	repo := NewRecipeRepository(pool)
	lookups := NewLookupRepository(pool)

	butter, err := lookups.FindOrCreateIngredient(ctx, "Butter")
	if err != nil {
		t.Fatalf("FindOrCreateIngredient() error = %v", err)
	}
	el, err := lookups.FindOrCreateUnit(ctx, "EL")
	if err != nil {
		t.Fatalf("FindOrCreateUnit() error = %v", err)
	}

	r := &domain.Recipe{
		Name: "Rührei", Source: "src-ruehrei", Status: domain.StatusPublished,
		Ingredients: []domain.RecipeIngredient{
			{IngredientID: butter.ID, Amount: 2, UnitID: &el.ID},
		},
	}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// A read must populate the write-side UnitID, not just the display UnitName.
	loaded, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if len(loaded.Ingredients) != 1 {
		t.Fatalf("Ingredients = %+v, want 1", loaded.Ingredients)
	}
	if got := loaded.Ingredients[0].UnitID; got == nil || *got != el.ID {
		t.Fatalf("GetByID Ingredients[0].UnitID = %v, want &%d", got, el.ID)
	}

	// Load-modify-save: change an unrelated field, hand the loaded Ingredients
	// slice straight back to Update — the unit must survive, not get nulled.
	loaded.Name = "Rührei mit Schnittlauch"
	if err := repo.Update(ctx, loaded); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	reloaded, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() after update error = %v", err)
	}
	if len(reloaded.Ingredients) != 1 {
		t.Fatalf("Ingredients after update = %+v, want 1", reloaded.Ingredients)
	}
	if got := reloaded.Ingredients[0].UnitName; got != "EL" {
		t.Errorf("UnitName after update = %q, want EL", got)
	}
	if got := reloaded.Ingredients[0].UnitID; got == nil || *got != el.ID {
		t.Errorf("UnitID after update = %v, want &%d", got, el.ID)
	}
}

func TestRecipeRepository_GetBySource_NotFound(t *testing.T) {
	repo := newTestRecipeRepo(t)
	_, err := repo.GetBySource(context.Background(), "does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestRecipeRepository_Search(t *testing.T) {
	ctx := context.Background()
	repo := newTestRecipeRepo(t)

	published := domain.StatusPublished
	for i, name := range []string{"Apfelkuchen", "Bananenbrot", "Apfelmus"} {
		r := &domain.Recipe{Name: name, Source: "src-" + name, Status: domain.StatusPublished}
		if i == 2 {
			r.Status = domain.StatusNeedsReview
		}
		if err := repo.Create(ctx, r); err != nil {
			t.Fatalf("Create(%q) error = %v", name, err)
		}
	}

	results, total, err := repo.Search(ctx, SearchQuery{Text: "apfel", PageSize: 10})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(results) != 2 {
		t.Errorf("len(results) = %d, want 2", len(results))
	}

	_, total, err = repo.Search(ctx, SearchQuery{Status: &published, PageSize: 10})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if total != 2 {
		t.Errorf("published total = %d, want 2", total)
	}
}
