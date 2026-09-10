// internal/pipeline/pipeline_test.go
package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
	"github.com/sBurmester/recipe-reader/internal/domain"
	"github.com/sBurmester/recipe-reader/internal/extraction"
	"github.com/sBurmester/recipe-reader/internal/instagram"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

type fakeFetcher struct {
	posts []instagram.SavedPost
	err   error
}

func (f *fakeFetcher) FetchNewPosts(_ context.Context) ([]instagram.SavedPost, error) {
	return f.posts, f.err
}

type fakeExtractor struct {
	byCaption map[string]*extraction.ExtractedRecipe
}

func (f *fakeExtractor) Extract(_ context.Context, caption string) (*extraction.ExtractedRecipe, error) {
	if r, ok := f.byCaption[caption]; ok {
		return r, nil
	}
	return nil, errors.New("no fixture for caption")
}

func newTestPipeline(t *testing.T, fetcher PostFetcher, extractor extraction.Extractor) *Pipeline {
	t.Helper()
	pool := testdb.New(t)
	return &Pipeline{
		Fetcher:   fetcher,
		Extractor: extractor,
		Recipes:   repository.NewRecipeRepository(pool),
		Lookups:   repository.NewLookupRepository(pool),
		Threshold: 0.6,
	}
}

func TestPipeline_ImportsNewRecipe(t *testing.T) {
	post := instagram.SavedPost{Source: "src-1", Caption: "caption-1", ImageURL: "img-1"}
	extracted := &extraction.ExtractedRecipe{
		Name:         "Pfannkuchen",
		Instructions: "Backen.",
		Ingredients:  []extraction.ExtractedIngredient{{Name: "Mehl", Amount: 200, Unit: "g"}},
		Categories:   []string{"Frühstück"},
		Confidence:   0.9,
	}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{"caption-1": extracted}},
	)

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != (ImportResult{Seen: 1, Imported: 1}) {
		t.Errorf("result = %+v", result)
	}

	stored, err := p.Recipes.GetBySource(context.Background(), "src-1")
	if err != nil {
		t.Fatalf("GetBySource() error = %v", err)
	}
	if stored.Status != domain.StatusPublished {
		t.Errorf("Status = %q, want published (confidence 0.9 >= threshold 0.6)", stored.Status)
	}
	if len(stored.Ingredients) != 1 || stored.Ingredients[0].IngredientName != "Mehl" {
		t.Errorf("Ingredients = %+v", stored.Ingredients)
	}
	if len(stored.Categories) != 1 {
		t.Errorf("Categories = %+v", stored.Categories)
	}
}

func TestPipeline_LowConfidenceMarkedNeedsReview(t *testing.T) {
	post := instagram.SavedPost{Source: "src-2", Caption: "caption-2"}
	extracted := &extraction.ExtractedRecipe{Name: "Unklar", Confidence: 0.2}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{"caption-2": extracted}},
	)

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	stored, err := p.Recipes.GetBySource(context.Background(), "src-2")
	if err != nil {
		t.Fatalf("GetBySource() error = %v", err)
	}
	if stored.Status != domain.StatusNeedsReview {
		t.Errorf("Status = %q, want needs_review", stored.Status)
	}
}

func TestPipeline_SkipsAlreadyImportedSource(t *testing.T) {
	post := instagram.SavedPost{Source: "src-3", Caption: "caption-3"}
	extracted := &extraction.ExtractedRecipe{Name: "X", Confidence: 0.9}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{"caption-3": extracted}},
	)

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if result != (ImportResult{Seen: 1, Skipped: 1}) {
		t.Errorf("second run result = %+v, want all skipped", result)
	}
}

func TestPipeline_ExtractionFailureCountsAsFailedNotFatal(t *testing.T) {
	post := instagram.SavedPost{Source: "src-4", Caption: "unrecognized-caption"}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{}},
	)

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v, want nil (a single failed item should not fail the whole run)", err)
	}
	if result != (ImportResult{Seen: 1, Failed: 1}) {
		t.Errorf("result = %+v", result)
	}
}
