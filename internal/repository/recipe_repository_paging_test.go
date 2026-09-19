package repository

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/domain"
)

// sameIngredient compares two associations by value. RecipeIngredient carries
// a *int64 unit id, so == compares the pointers and never the units.
func sameIngredient(a, b domain.RecipeIngredient) bool {
	if (a.UnitID == nil) != (b.UnitID == nil) {
		return false
	}
	if a.UnitID != nil && *a.UnitID != *b.UnitID {
		return false
	}
	return a.IngredientID == b.IngredientID && a.IngredientName == b.IngredientName &&
		a.Amount == b.Amount && a.UnitName == b.UnitName
}

func showIngredient(ing domain.RecipeIngredient) string {
	unit := "<nil>"
	if ing.UnitID != nil {
		unit = fmt.Sprint(*ing.UnitID)
	}
	return fmt.Sprintf("{id:%d name:%q amount:%v unitID:%s unit:%q}",
		ing.IngredientID, ing.IngredientName, ing.Amount, unit, ing.UnitName)
}

// seedRecipes stores n recipes, each with two ingredients and one category, so
// a page has associations to assemble and a category to filter on.
func seedRecipes(t *testing.T, repo RecipeRepository, n int) {
	t.Helper()
	ctx := context.Background()
	for i := range n {
		r := &domain.Recipe{
			Name:   fmt.Sprintf("Rezept %02d", i),
			Source: fmt.Sprintf("src-page-%02d", i),
			Status: domain.StatusPublished,
			Ingredients: []domain.RecipeIngredient{
				{IngredientName: fmt.Sprintf("Zutat A%02d", i), Amount: float64(i + 1), UnitName: "g"},
				{IngredientName: fmt.Sprintf("Zutat B%02d", i), Amount: 2},
			},
			Categories: []domain.Category{{Name: "Hauptgericht"}},
		}
		if err := repo.Create(ctx, r); err != nil {
			t.Fatalf("Create(%q) error = %v", r.Name, err)
		}
	}
}

// A page assembled in two batched child queries must carry exactly what the
// per-row assemble carried: the same associations, in the same order.
func TestRecipeRepository_Search_BatchedPageMatchesSingleRowAssembly(t *testing.T) {
	ctx := context.Background()
	repo := newTestRecipeRepo(t)
	seedRecipes(t, repo, 5)

	page, total, err := repo.Search(ctx, SearchQuery{PageSize: 100})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if total != 5 || len(page) != 5 {
		t.Fatalf("Search() returned %d of %d rows, want 5 of 5", len(page), total)
	}

	for _, fromPage := range page {
		single, err := repo.GetByID(ctx, fromPage.ID)
		if err != nil {
			t.Fatalf("GetByID(%d) error = %v", fromPage.ID, err)
		}
		if len(fromPage.Ingredients) != len(single.Ingredients) {
			t.Fatalf("%q: page has %d ingredients, GetByID has %d",
				fromPage.Name, len(fromPage.Ingredients), len(single.Ingredients))
		}
		for i := range fromPage.Ingredients {
			if !sameIngredient(fromPage.Ingredients[i], single.Ingredients[i]) {
				t.Errorf("%q ingredient %d: page has %s, GetByID has %s",
					fromPage.Name, i, showIngredient(fromPage.Ingredients[i]), showIngredient(single.Ingredients[i]))
			}
		}
		if len(fromPage.Categories) != len(single.Categories) {
			t.Fatalf("%q: page has %d categories, GetByID has %d",
				fromPage.Name, len(fromPage.Categories), len(single.Categories))
		}
		for i := range fromPage.Categories {
			if fromPage.Categories[i] != single.Categories[i] {
				t.Errorf("%q category %d: page has %+v, GetByID has %+v",
					fromPage.Name, i, fromPage.Categories[i], single.Categories[i])
			}
		}
	}
}

// Associations must not leak between rows of a page — the failure a map keyed
// by the wrong id would produce, and one the old per-row assembly could not
// have.
func TestRecipeRepository_Search_AssociationsStayWithTheirRecipe(t *testing.T) {
	ctx := context.Background()
	repo := newTestRecipeRepo(t)
	seedRecipes(t, repo, 4)

	page, _, err := repo.Search(ctx, SearchQuery{PageSize: 100})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	for _, r := range page {
		var suffix string
		if _, err := fmt.Sscanf(r.Name, "Rezept %s", &suffix); err != nil {
			t.Fatalf("unexpected name %q", r.Name)
		}
		if len(r.Ingredients) != 2 {
			t.Fatalf("%q has %d ingredients, want 2", r.Name, len(r.Ingredients))
		}
		for _, ing := range r.Ingredients {
			if got := ing.IngredientName[len(ing.IngredientName)-2:]; got != suffix {
				t.Errorf("%q carries ingredient %q, which belongs to another recipe", r.Name, ing.IngredientName)
			}
		}
	}
}

// The semi-join replaced a LEFT JOIN plus DISTINCT. A recipe in several
// categories must still appear once, and the count must agree with the page.
func TestRecipeRepository_Search_MultipleCategoriesDoNotDuplicateARow(t *testing.T) {
	ctx := context.Background()
	repo := newTestRecipeRepo(t)

	r := &domain.Recipe{
		Name: "Ofengemüse", Source: "src-multi", Status: domain.StatusPublished,
		Categories: []domain.Category{{Name: "Hauptgericht"}, {Name: "Vegetarisch"}, {Name: "Vegan"}},
	}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	page, total, err := repo.Search(ctx, SearchQuery{PageSize: 100})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(page) != 1 {
		t.Errorf("Search() returned %d rows (%v), want 1 — a recipe in three categories must not be tripled",
			len(page), recipeNames(page))
	}
	if total != 1 {
		t.Errorf("total = %d, want 1 — the count disagrees with the page", total)
	}

	// And the filter itself still selects: by any one of its categories.
	categoryID := page[0].Categories[1].ID
	filtered, total, err := repo.Search(ctx, SearchQuery{CategoryID: &categoryID, PageSize: 100})
	if err != nil {
		t.Fatalf("Search(category) error = %v", err)
	}
	if len(filtered) != 1 || total != 1 {
		t.Errorf("Search(category) returned %d rows, total %d, want 1 and 1", len(filtered), total)
	}

	// A category nothing carries selects nothing.
	absent := categoryID + 99999
	none, total, err := repo.Search(ctx, SearchQuery{CategoryID: &absent, PageSize: 100})
	if err != nil {
		t.Fatalf("Search(absent category) error = %v", err)
	}
	if len(none) != 0 || total != 0 {
		t.Errorf("Search(absent category) returned %d rows, total %d, want 0 and 0", len(none), total)
	}
}

// page arrives from strconv.Atoi on a query parameter and only its lower bound
// was clamped, so the int32 OFFSET conversion wrapped: a negative offset
// Postgres rejects with a 500, or — worse — a small positive one that returns
// page 1's rows while claiming to be page N.
func TestRecipeRepository_Search_ClampsPageBeforeTheInt32Offset(t *testing.T) {
	ctx := context.Background()
	repo := newTestRecipeRepo(t)
	seedRecipes(t, repo, 3)

	firstPage, _, err := repo.Search(ctx, SearchQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search(page 1) error = %v", err)
	}

	for _, page := range []int{
		214748365,          // int32 offset wraps to -16 at the default page size
		3000000000,         // wraps to a large negative
		math.MaxInt32,      //
		math.MaxInt64 / 50, // the multiplication itself would overflow int64
		math.MaxInt64,
	} {
		t.Run(fmt.Sprint(page), func(t *testing.T) {
			got, total, err := repo.Search(ctx, SearchQuery{Page: page, PageSize: 20})
			if err != nil {
				t.Fatalf("Search(page=%d) error = %v, want an empty page", page, err)
			}
			if len(got) != 0 {
				t.Errorf("Search(page=%d) returned %d rows (%v), want none — this is past the last page",
					page, len(got), recipeNames(got))
			}
			if total != 3 {
				t.Errorf("Search(page=%d) total = %d, want 3 — the count still describes the whole result set",
					page, total)
			}
		})
	}

	// The guard must not have moved the ordinary pages.
	stillFirst, _, err := repo.Search(ctx, SearchQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search(page 1) error = %v", err)
	}
	if len(stillFirst) != len(firstPage) {
		t.Errorf("page 1 returned %d rows, want the %d it returned before", len(stillFirst), len(firstPage))
	}
}
