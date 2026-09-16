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
	"log/slog"

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
//
// Degraded overlaps Imported rather than partitioning Seen: it counts how many
// of the imported recipes came from the rules because the LLM call failed. A
// run where it equals Imported is a run where the LLM never worked at all.
type ImportResult struct {
	Seen, Imported, Skipped, NoRecipe, Failed int
	Degraded                                  int
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
// is counted in ImportResult.Failed and does not abort the run.
//
// A fetch that fails part-way still hands back what it collected, and those
// posts are imported before the error is returned. That matters most for a
// rate limit: the posts already in hand are the only work this run will get,
// and discarding them would mean re-fetching them after the cooldown — more
// requests against the endpoint that just asked for fewer.
func (p *Pipeline) Run(ctx context.Context) (ImportResult, error) {
	posts, fetchErr := p.Fetcher.FetchNewPosts(ctx)
	if fetchErr != nil && len(posts) == 0 {
		return ImportResult{}, fmt.Errorf("pipeline: fetch posts: %w", fetchErr)
	}

	var result ImportResult
	for _, post := range posts {
		// Checked per post rather than only before the loop: extraction can
		// make a network call per item, so a run over 50 posts is where a
		// shutdown actually needs to take effect.
		if err := ctx.Err(); err != nil {
			slog.Warn("import: cancelled mid-run", "error", err, "result", result)
			return result, fmt.Errorf("pipeline: %w", err)
		}
		result.Seen++

		if _, err := p.Recipes.GetBySource(ctx, post.Source); err == nil {
			result.Skipped++
			continue
		} else if !errors.Is(err, repository.ErrNotFound) {
			// Every Failed branch below names its stage. The tally alone says
			// a post failed; it never said where, and these four failures need
			// four different responses from whoever reads the log.
			result.Failed++
			slog.Warn("import: post failed", "stage", "dedupe-lookup", "source", post.Source, "error", err)
			continue
		}

		extracted, err := p.Extractor.Extract(ctx, post.Caption)
		if errors.Is(err, extraction.ErrNoRecipe) {
			result.NoRecipe++
			continue
		}
		if err != nil {
			result.Failed++
			slog.Warn("import: post failed", "stage", "extract", "source", post.Source, "error", err)
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
			slog.Warn("import: post failed", "stage", "resolve-lookups", "source", post.Source, "error", err)
			continue
		}

		if err := p.Recipes.Create(ctx, recipe); err != nil {
			result.Failed++
			slog.Warn("import: post failed", "stage", "store", "source", post.Source, "error", err)
			continue
		}
		result.Imported++
		if extracted.Degraded {
			result.Degraded++
		}
	}

	if fetchErr != nil {
		return result, fmt.Errorf("pipeline: fetch posts: %w", fetchErr)
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
