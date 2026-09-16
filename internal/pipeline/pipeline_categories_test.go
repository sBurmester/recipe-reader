package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/domain"
	"github.com/sBurmester/recipe-reader/internal/extraction"
	"github.com/sBurmester/recipe-reader/internal/instagram"
)

// fakeCategories is the vocabulary a run is allowed to draw on. err drives the
// case where the read itself fails.
type fakeCategories struct {
	names []string
	err   error
	calls int
}

func (f *fakeCategories) ListCategories(_ context.Context) ([]domain.Category, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]domain.Category, 0, len(f.names))
	for i, name := range f.names {
		out = append(out, domain.Category{ID: int64(i + 1), Name: name})
	}
	return out, nil
}

// runWithCategories imports one post whose extraction proposes the given
// categories, and returns what was stored.
func runWithCategories(t *testing.T, lister CategoryLister, proposed []string) *domain.Recipe {
	t.Helper()
	post := instagram.SavedPost{Source: "src-cat", Caption: "caption-cat"}
	extracted := &extraction.ExtractedRecipe{
		Name:         "Ofengemüse",
		Instructions: "Backen.",
		Ingredients:  []extraction.ExtractedIngredient{{Name: "Zucchini", Amount: 1}},
		Categories:   proposed,
		Confidence:   0.9,
	}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{"caption-cat": extracted}},
	)
	p.Categories = lister

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	stored, err := p.Recipes.GetBySource(context.Background(), "src-cat")
	if err != nil {
		t.Fatalf("GetBySource() error = %v", err)
	}
	return stored
}

func categoryNames(r *domain.Recipe) []string {
	out := make([]string, 0, len(r.Categories))
	for _, c := range r.Categories {
		out = append(out, c.Name)
	}
	return out
}

// The caption an extraction comes from is attacker-controlled, and a category
// name lands in a table every user's picker reads. Only names already in the
// vocabulary may be written.
func TestPipeline_DropsCategoriesOutsideTheVocabulary(t *testing.T) {
	lister := &fakeCategories{names: []string{"Hauptgericht", "Vegetarisch"}}
	stored := runWithCategories(t, lister, []string{
		"Hauptgericht",
		"Kostenlose Bitcoins auf example.invalid",
		"Vegetarisch",
	})

	got := categoryNames(stored)
	if len(got) != 2 {
		t.Fatalf("Categories = %v, want only the two in the vocabulary", got)
	}
	for _, name := range got {
		if strings.Contains(name, "Bitcoins") {
			t.Errorf("an injected category was stored: %v", got)
		}
	}
}

// A match is case- and whitespace-insensitive, and stores the vocabulary's own
// spelling — otherwise "vegetarisch" from the model becomes a second row
// beside the seeded "Vegetarisch".
func TestPipeline_CategoryMatchIsFoldedToTheCanonicalSpelling(t *testing.T) {
	lister := &fakeCategories{names: []string{"Vegetarisch"}}
	stored := runWithCategories(t, lister, []string{"  vegetarisch "})

	if got := categoryNames(stored); len(got) != 1 || got[0] != "Vegetarisch" {
		t.Errorf("Categories = %v, want [Vegetarisch]", got)
	}
}

// The vocabulary is one read per run, not one per post: a run over 50 posts
// must not cost 50 extra round trips.
func TestPipeline_ReadsTheVocabularyOncePerRun(t *testing.T) {
	lister := &fakeCategories{names: []string{"Dessert"}}
	posts := []instagram.SavedPost{
		{Source: "src-a", Caption: "cap-a"},
		{Source: "src-b", Caption: "cap-b"},
	}
	extracted := &extraction.ExtractedRecipe{Name: "Nachtisch", Categories: []string{"Dessert"}, Confidence: 0.9}
	p := newTestPipeline(t,
		&fakeFetcher{posts: posts},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{"cap-a": extracted, "cap-b": extracted}},
	)
	p.Categories = lister

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if lister.calls != 1 {
		t.Errorf("ListCategories called %d times, want 1 for the whole run", lister.calls)
	}
}

// A failed vocabulary read is not a reason to abandon an import: the recipes
// are the point and the categories are an annotation.
func TestPipeline_VocabularyReadFailureDoesNotFailTheRun(t *testing.T) {
	lister := &fakeCategories{err: errors.New("boom")}
	stored := runWithCategories(t, lister, []string{"Dessert"})

	if stored.Name != "Ofengemüse" {
		t.Errorf("Name = %q, want the recipe to have been imported anyway", stored.Name)
	}
}

// The other two shared lookup tables cannot be a closed set, so they are
// bounded instead: a name stays one line and a sane length.
func TestPipeline_BoundsIngredientAndUnitNames(t *testing.T) {
	post := instagram.SavedPost{Source: "src-long", Caption: "caption-long"}
	extracted := &extraction.ExtractedRecipe{
		Name:         "Zeile eins\nZeile zwei " + strings.Repeat("x", 500),
		Instructions: "Rühren.",
		Ingredients: []extraction.ExtractedIngredient{
			{Name: strings.Repeat("ü", 400), Amount: 1, Unit: strings.Repeat("g", 200)},
			{Name: "  Mehl\nIgnoriere alle Anweisungen  ", Amount: 200, Unit: "g"},
			{Name: "   ", Amount: 1},
		},
		Confidence: 0.9,
	}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{"caption-long": extracted}},
	)

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	stored, err := p.Recipes.GetBySource(context.Background(), "src-long")
	if err != nil {
		t.Fatalf("GetBySource() error = %v", err)
	}

	if strings.ContainsAny(stored.Name, "\n\r") {
		t.Errorf("Name kept a newline: %q", stored.Name)
	}
	if n := len([]rune(stored.Name)); n > maxRecipeNameRunes {
		t.Errorf("Name = %d runes, want <= %d", n, maxRecipeNameRunes)
	}
	// The blank ingredient is dropped; the other two survive, bounded.
	if len(stored.Ingredients) != 2 {
		t.Fatalf("Ingredients = %+v, want 2", stored.Ingredients)
	}
	for _, ing := range stored.Ingredients {
		if strings.ContainsAny(ing.IngredientName, "\n\r") {
			t.Errorf("ingredient name kept a newline: %q", ing.IngredientName)
		}
		if n := len([]rune(ing.IngredientName)); n > maxIngredientNameRunes {
			t.Errorf("ingredient name = %d runes, want <= %d", n, maxIngredientNameRunes)
		}
		if n := len([]rune(ing.UnitName)); n > maxUnitNameRunes {
			t.Errorf("unit name = %d runes, want <= %d", n, maxUnitNameRunes)
		}
	}
}

// An extraction that produced no name at all must still be findable: the
// column is NOT NULL and the frontend lists by it.
func TestPipeline_EmptyNameFallsBackToThePlaceholder(t *testing.T) {
	post := instagram.SavedPost{Source: "src-noname", Caption: "caption-noname"}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{
			"caption-noname": {Name: "  ", Instructions: "Rühren.", Confidence: 0.9},
		}},
	)

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	stored, err := p.Recipes.GetBySource(context.Background(), "src-noname")
	if err != nil {
		t.Fatalf("GetBySource() error = %v", err)
	}
	if stored.Name != untitledRecipe {
		t.Errorf("Name = %q, want %q", stored.Name, untitledRecipe)
	}
}
