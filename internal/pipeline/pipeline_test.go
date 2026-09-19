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
	// errByCaption lets a test drive the typed outcomes — ErrNoRecipe above
	// all — rather than only the happy path.
	errByCaption map[string]error
}

func (f *fakeExtractor) Extract(_ context.Context, caption string) (*extraction.ExtractedRecipe, error) {
	if err, ok := f.errByCaption[caption]; ok {
		return nil, err
	}
	if r, ok := f.byCaption[caption]; ok {
		return r, nil
	}
	return nil, errors.New("no fixture for caption")
}

func newTestPipeline(t *testing.T, fetcher PostFetcher, extractor extraction.Extractor) *Pipeline {
	t.Helper()
	pool := testdb.New(t)
	return &Pipeline{
		Fetcher:          fetcher,
		Extractor:        extractor,
		Recipes:          repository.NewRecipeRepository(pool),
		Threshold:        0.6,
		PublishThreshold: 0.8,
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
		t.Errorf("Status = %q, want published (confidence 0.9 >= publish threshold 0.8)", stored.Status)
	}
	if len(stored.Ingredients) != 1 || stored.Ingredients[0].IngredientName != "Mehl" {
		t.Errorf("Ingredients = %+v", stored.Ingredients)
	}
	if len(stored.Categories) != 1 {
		t.Errorf("Categories = %+v", stored.Categories)
	}
}

// A result above the LLM-fallback threshold but below the publish threshold
// lands in needs_review. Under the single shared number it would have
// published — which is the half of E2 that lives in the pipeline.
func TestPipeline_BelowPublishThresholdNeedsReviewEvenAboveFallbackThreshold(t *testing.T) {
	post := instagram.SavedPost{Source: "src-5", Caption: "caption-5"}
	extracted := &extraction.ExtractedRecipe{Name: "Wackelig", Confidence: 0.7}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{"caption-5": extracted}},
	)

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	stored, err := p.Recipes.GetBySource(context.Background(), "src-5")
	if err != nil {
		t.Fatalf("GetBySource() error = %v", err)
	}
	if stored.Status != domain.StatusNeedsReview {
		t.Errorf("Status = %q, want needs_review (0.7 clears the 0.6 fallback threshold but not the 0.8 publish threshold)", stored.Status)
	}
}

// A caption with no recipe in it must not become a row. Before the gate, a gym
// selfie was imported as a recipe whose instructions were the literal sentinel
// — and because source is UNIQUE and the pipeline skips anything already
// present, that junk row permanently blocked the post from being re-imported
// by a better extractor.
func TestPipeline_NoRecipeCaptionCreatesNoRow(t *testing.T) {
	post := instagram.SavedPost{Source: "src-6", Caption: "gym selfie"}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{errByCaption: map[string]error{"gym selfie": extraction.ErrNoRecipe}},
	)

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != (ImportResult{Seen: 1, NoRecipe: 1}) {
		t.Errorf("result = %+v, want it counted as NoRecipe rather than Imported or Failed", result)
	}
	if _, err := p.Recipes.GetBySource(context.Background(), "src-6"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("GetBySource() error = %v, want ErrNotFound — no row should exist", err)
	}
}

// The rules-only path has no sentinel of its own: a caption without the German
// section headers scores zero, and that is the same "no recipe" answer.
func TestPipeline_ZeroConfidenceCreatesNoRow(t *testing.T) {
	post := instagram.SavedPost{Source: "src-7", Caption: "caption-7"}
	extracted := &extraction.ExtractedRecipe{Name: "Erste Zeile", Confidence: 0}
	p := newTestPipeline(t,
		&fakeFetcher{posts: []instagram.SavedPost{post}},
		&fakeExtractor{byCaption: map[string]*extraction.ExtractedRecipe{"caption-7": extracted}},
	)

	result, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != (ImportResult{Seen: 1, NoRecipe: 1}) {
		t.Errorf("result = %+v, want NoRecipe", result)
	}
	if _, err := p.Recipes.GetBySource(context.Background(), "src-7"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("GetBySource() error = %v, want ErrNotFound", err)
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
