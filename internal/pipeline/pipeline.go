// Package pipeline wires the import flow together: fetch saved Instagram
// posts, extract a structured recipe from each caption, and store the ones
// that are new. It owns no I/O of its own — the fetcher, extractor, and
// repositories are injected, which is what makes the whole flow testable
// against fakes plus an ephemeral Postgres.
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

// PostFetcher supplies the saved posts to consider for import. main.go
// implements it with a small adapter over *instagram.Client that picks
// between the account's "All Posts" feed and a named collection.
type PostFetcher interface {
	FetchNewPosts(ctx context.Context) ([]instagram.SavedPost, error)
}

// ImportResult is the per-run tally. Seen counts every post the fetcher
// returned; the other four partition it — Imported (stored), Skipped (already
// present, matched by source), NoRecipe (the caption carried no recipe, so
// nothing was stored), Failed (extraction or storage error on that one post).
//
// NoRecipe is counted apart from Skipped and Failed on purpose: a saved-posts
// feed legitimately contains things that are not recipes, and folding them
// into Failed would make a healthy run look broken while folding them into
// Skipped would hide how much of the feed is noise.
type ImportResult struct {
	Seen, Imported, Skipped, NoRecipe, Failed int
}

// Pipeline runs one import pass over the fetcher's posts.
//
// Two different questions used to share one configured number: "were the rules
// good enough to skip the LLM" and "is this good enough to publish without a
// human looking at it". The first belongs to the hybrid extractor and the
// second belongs here; the second should be the stricter of the two, and
// conflating them is half of why every LLM result published unreviewed.
type Pipeline struct {
	Fetcher   PostFetcher
	Extractor extraction.Extractor
	Recipes   repository.RecipeRepository
	Lookups   repository.LookupRepository

	// Threshold is the original single knob, kept so an existing Pipeline
	// literal behaves as it did. Prefer PublishThreshold.
	Threshold float64

	// PublishThreshold is the confidence at or above which an extraction is
	// stored as published rather than needs_review. Zero falls back to
	// Threshold rather than to zero, so a partially-configured Pipeline
	// degrades to the old behaviour instead of publishing everything.
	PublishThreshold float64
}

// Run fetches posts and imports the new ones. A failure on any single post
// is counted in ImportResult.Failed and does not abort the run — only a
// failure to fetch at all returns an error, since that yields no work to do.
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
		if errors.Is(err, extraction.ErrNoRecipe) {
			result.NoRecipe++
			continue
		}
		if err != nil {
			result.Failed++
			continue
		}
		// The rules-only path has no sentinel of its own: a caption without
		// the German section headers yields empty ingredients, empty
		// instructions and a name scraped off the first line, scoring zero.
		// Storing that produced one row per post regardless of whether any of
		// them were recipes — and because source is UNIQUE and the pipeline
		// skips anything already present, each junk row permanently blocked
		// its post from ever being re-imported by a better extractor.
		if extracted.Confidence <= 0 {
			result.NoRecipe++
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

// toRecipe maps an extraction result onto a domain.Recipe, resolving the
// free-text category, ingredient, and unit names to lookup-table rows
// (creating them on first sight) so Create only has to write IDs.
func (p *Pipeline) toRecipe(ctx context.Context, post instagram.SavedPost, ex *extraction.ExtractedRecipe) (*domain.Recipe, error) {
	status := domain.StatusPublished
	if ex.Confidence < p.publishThreshold() {
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

// publishThreshold is PublishThreshold, or Threshold when it is unset.
func (p *Pipeline) publishThreshold() float64 {
	if p.PublishThreshold > 0 {
		return p.PublishThreshold
	}
	return p.Threshold
}
