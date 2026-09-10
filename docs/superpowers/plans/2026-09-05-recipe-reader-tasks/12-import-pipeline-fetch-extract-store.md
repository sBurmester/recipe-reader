> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 4: Import Pipeline.
>
> **Status:** [x] done

# Task 12: Import Pipeline (Fetch → Extract → Store)

**Files:**
- Create: `internal/pipeline/pipeline.go`
- Test: `internal/pipeline/pipeline_test.go`

**Interfaces:**
- Consumes: `extraction.Extractor`, `extraction.ExtractedRecipe` (Task 6/9); `repository.RecipeRepository`, `repository.LookupRepository`, `repository.ErrNotFound` (Task 4/5); `instagram.SavedPost` (Task 11); `domain.Recipe`, `domain.RecipeIngredient`, `domain.StatusPublished`, `domain.StatusNeedsReview` (Task 3).
- Produces:

```go
type PostFetcher interface {
	FetchNewPosts(ctx context.Context) ([]instagram.SavedPost, error)
}

type ImportResult struct {
	Seen, Imported, Skipped, Failed int
}

type Pipeline struct {
	Fetcher   PostFetcher
	Extractor extraction.Extractor
	Recipes   repository.RecipeRepository
	Lookups   repository.LookupRepository
	Threshold float64
}

func (p *Pipeline) Run(ctx context.Context) (ImportResult, error)
```

Task 13 (worker) wraps `Pipeline.Run`. Task 18 (`main.go`) implements `PostFetcher` with a tiny adapter around `*instagram.Client` (Task 10/11's `PipelineFetcher`).

- [x] **Step 1: Write the failing test**

```go
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
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/... -v`
Expected: FAIL — package doesn't exist.

- [x] **Step 3: Implement**

```go
// internal/pipeline/pipeline.go
package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/sBurmester/recipe-reader/internal/domain"
	"github.com/sBurmester/recipe-reader/internal/extraction"
	"github.com/sBurmester/recipe-reader/internal/instagram"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

type PostFetcher interface {
	FetchNewPosts(ctx context.Context) ([]instagram.SavedPost, error)
}

type ImportResult struct {
	Seen, Imported, Skipped, Failed int
}

type Pipeline struct {
	Fetcher   PostFetcher
	Extractor extraction.Extractor
	Recipes   repository.RecipeRepository
	Lookups   repository.LookupRepository
	Threshold float64
}

func (p *Pipeline) Run(ctx context.Context) (ImportResult, error) {
	posts, err := p.Fetcher.FetchNewPosts(ctx)
	if err != nil {
		return ImportResult{}, fmt.Errorf("pipeline: fetch posts: %w", err)
	}

	var result ImportResult
	for _, post := range posts {
		result.Seen++

		if _, err := p.Recipes.GetBySource(ctx, post.Source); err == nil {
			result.Skipped++
			continue
		} else if !errors.Is(err, repository.ErrNotFound) {
			result.Failed++
			continue
		}

		extracted, err := p.Extractor.Extract(ctx, post.Caption)
		if err != nil {
			result.Failed++
			continue
		}

		recipe, err := p.toRecipe(ctx, post, extracted)
		if err != nil {
			result.Failed++
			continue
		}

		if err := p.Recipes.Create(ctx, recipe); err != nil {
			result.Failed++
			continue
		}
		result.Imported++
	}
	return result, nil
}

func (p *Pipeline) toRecipe(ctx context.Context, post instagram.SavedPost, ex *extraction.ExtractedRecipe) (*domain.Recipe, error) {
	status := domain.StatusPublished
	if ex.Confidence < p.Threshold {
		status = domain.StatusNeedsReview
	}

	recipe := &domain.Recipe{
		Name:         ex.Name,
		Instructions: ex.Instructions,
		ImageURL:     post.ImageURL,
		Source:       post.Source,
		Status:       status,
	}

	for _, catName := range ex.Categories {
		cat, err := p.Lookups.FindOrCreateCategory(ctx, catName)
		if err != nil {
			return nil, err
		}
		recipe.Categories = append(recipe.Categories, *cat)
	}

	for _, ing := range ex.Ingredients {
		ingredient, err := p.Lookups.FindOrCreateIngredient(ctx, ing.Name)
		if err != nil {
			return nil, err
		}
		ri := domain.RecipeIngredient{IngredientID: ingredient.ID, Amount: ing.Amount}
		if ing.Unit != "" {
			unit, err := p.Lookups.FindOrCreateUnit(ctx, ing.Unit)
			if err != nil {
				return nil, err
			}
			ri.UnitID = &unit.ID
		}
		recipe.Ingredients = append(recipe.Ingredients, ri)
	}

	return recipe, nil
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/... -v` (needs Docker running)
Expected: PASS (all four scenarios)

- [x] **Step 5: Commit**

```bash
git add internal/pipeline/pipeline.go internal/pipeline/pipeline_test.go
git commit -m "$(cat <<'EOF'
feat: add import pipeline (fetch, extract, dedupe, store)

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 11](11-saved-posts-collection-fetching.md) · [Task 13 →](13-background-worker-scheduled-manual-trigger.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
