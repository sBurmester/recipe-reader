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
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sBurmester/recipe-reader/internal/domain"
	"github.com/sBurmester/recipe-reader/internal/extraction"
	"github.com/sBurmester/recipe-reader/internal/instagram"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

// PostFetcher supplies the saved posts to consider for import. internal/server
// wires in instagram.PipelineFetcher, the small adapter over *instagram.Client
// that implements it by picking between the account's "All Posts" feed and a
// named collection.
type PostFetcher interface {
	FetchNewPosts(ctx context.Context) ([]instagram.SavedPost, error)
}

// CategoryLister supplies the category vocabulary an import may attach to a
// recipe. repository.LookupRepository satisfies it; the narrow interface is
// what keeps this package testable without a database.
type CategoryLister interface {
	ListCategories(ctx context.Context) ([]domain.Category, error)
}

// ImportLock serializes import runs across processes. The scheduled worker in a
// running server and a one-off `recipe-reader import` are two processes sharing
// one Instagram account, and the 15-minute floor between logins lives in the
// client — that is, in each process separately. Two runs at once would ration
// nothing, and repeated logins are what gets an account flagged.
//
// It is an interface here and a Postgres advisory lock in internal/db: the
// pipeline has no business knowing there is a database behind it.
type ImportLock interface {
	// TryAcquire takes the lock without waiting. ok reports whether it got it;
	// release is valid only when ok is true and must be called when the run
	// ends. An error means the lock could not be consulted at all.
	TryAcquire(ctx context.Context) (release func(), ok bool, err error)
}

// ErrImportInProgress reports that another import holds the lock. For the
// import command it is the whole outcome — exit 1, nothing done — so it is a
// sentinel rather than a string.
var ErrImportInProgress = errors.New("import: another import is already running")

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

	// Categories closes the category vocabulary an import may write. The
	// caption an extraction is derived from is attacker-controlled, and a
	// category name goes straight into a table every user's picker reads —
	// so a caption that talks the model into proposing "Buy crypto at ..."
	// used to create that row for everyone. With a lister wired, a proposed
	// category is matched against the ones that already exist (the seeded set
	// plus anything a human has since created) and dropped if it does not.
	//
	// Nil disables the check, which is the behaviour a Pipeline literal built
	// without it has always had. Run says so in the log the first time a
	// dropped-or-not decision actually arises, rather than filtering silently
	// or not filtering silently.
	Categories CategoryLister

	// Threshold is the original single knob, kept so an existing Pipeline
	// literal behaves as it did. Prefer PublishThreshold.
	Threshold float64

	// PublishThreshold is the confidence at or above which an extraction is
	// stored as published rather than needs_review. Zero falls back to
	// Threshold rather than to zero, so a partially-configured Pipeline
	// degrades to the old behaviour instead of publishing everything.
	PublishThreshold float64

	// Lock, when set, serializes this run against every other process using the
	// same database. Nil means unlocked, which is what the pipeline's own tests
	// and any single-process use want.
	Lock ImportLock
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
	if p.Lock != nil {
		release, ok, err := p.Lock.TryAcquire(ctx)
		if err != nil {
			return ImportResult{}, fmt.Errorf("import: taking the import lock: %w", err)
		}
		if !ok {
			return ImportResult{}, ErrImportInProgress
		}
		defer release()
	}

	posts, fetchErr := p.Fetcher.FetchNewPosts(ctx)
	if fetchErr != nil && len(posts) == 0 {
		return ImportResult{}, fmt.Errorf("%w: %w", ErrFetch, fetchErr)
	}

	// Read once per run, not once per post: the vocabulary does not change
	// mid-run in any way worth a query per import, and a run over 50 posts
	// should not cost 50 extra round trips to find that out.
	vocab := p.categoryVocabulary(ctx)

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

		// Create resolves the category, ingredient and unit names inside its
		// own transaction, so a lookup failure is a store failure too — and
		// either way the post leaves no lookup rows behind.
		//
		// The insert is also the dedupe. There used to be a GetBySource here,
		// before the extraction, and the gap between that read and this write
		// was a window in which the same post could be stored twice. Now the
		// unique index on source decides, and a duplicate comes back as
		// ErrDuplicateSource. The Failed branches below each name their stage:
		// the tally alone says a post failed, never where.
		//
		// What that trades: a duplicate is now discovered after extraction
		// rather than before it, so it can cost one LLM call. It stays a
		// non-issue because the fetcher already pages past imported posts
		// (FetchOptions.Known), so a duplicate only reaches here when that
		// advisory check could not answer — and the cost of being wrong is one
		// wasted call, against a read on every post of every run.
		err = p.Recipes.Create(ctx, p.toRecipe(post, extracted, vocab))
		if errors.Is(err, repository.ErrDuplicateSource) {
			result.Skipped++
			continue
		}
		if err != nil {
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
		return result, fmt.Errorf("%w: %w", ErrFetch, fetchErr)
	}
	return result, nil
}

// toRecipe maps an extraction result onto a domain.Recipe, carrying the
// free-text category, ingredient and unit names as names. They are resolved to
// lookup-table rows by RecipeRepository.Create, inside the transaction that
// stores the recipe — resolving them here, before the write began, committed
// them even when the write then failed.
func (p *Pipeline) toRecipe(post instagram.SavedPost, ex *extraction.ExtractedRecipe, vocab *categoryVocabulary) *domain.Recipe {
	status := domain.StatusPublished
	if ex.Confidence < p.publishThreshold() {
		status = domain.StatusNeedsReview
	}

	recipe := &domain.Recipe{
		Name:         sanitizeName(ex.Name, maxRecipeNameRunes),
		Instructions: ex.Instructions,
		ImageURL:     post.ImageURL,
		Source:       post.Source,
		Status:       status,
	}
	if recipe.Name == "" {
		recipe.Name = untitledRecipe
	}
	for _, name := range vocab.resolve(post.Source, ex.Categories) {
		recipe.Categories = append(recipe.Categories, domain.Category{Name: name})
	}
	for _, ing := range ex.Ingredients {
		// Ingredient and unit names are the other two shared lookup tables an
		// extraction writes into, and unlike categories they cannot be a closed
		// set — no list enumerates every ingredient. Bounding them is what is
		// left: a name is one line and a sane length, so an injected caption
		// cannot push a paragraph, a newline-built fake entry or a megabyte of
		// text into the pickers every user reads.
		name := sanitizeName(ing.Name, maxIngredientNameRunes)
		if name == "" {
			continue
		}
		recipe.Ingredients = append(recipe.Ingredients, domain.RecipeIngredient{
			IngredientName: name, Amount: ing.Amount, UnitName: sanitizeName(ing.Unit, maxUnitNameRunes),
		})
	}
	return recipe
}

// The bounds on a lookup name written from an extraction. Generous enough that
// no real ingredient, unit or title is touched, small enough that the row stays
// a label rather than a payload.
const (
	maxRecipeNameRunes     = 200
	maxIngredientNameRunes = 120
	maxUnitNameRunes       = 32
)

// untitledRecipe is the fallback when an extraction produced no usable name.
// recipes.name is NOT NULL and the frontend lists by it, so an empty string
// would be a row nobody can find.
const untitledRecipe = "Unbenanntes Rezept"

// sanitizeName reduces a free-text name to a single bounded line: control
// characters and newlines collapse to spaces, runs of whitespace collapse to
// one, and the result is truncated to maxRunes on a rune boundary.
func sanitizeName(name string, maxRunes int) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)
	cleaned = strings.TrimSpace(strings.Join(strings.Fields(cleaned), " "))
	if utf8.RuneCountInString(cleaned) <= maxRunes {
		return cleaned
	}
	return strings.TrimSpace(string([]rune(cleaned)[:maxRunes]))
}

// categoryVocabulary is one run's view of the categories an import may attach.
// A nil *categoryVocabulary means no vocabulary was available — either no
// lister is wired or the read failed — and resolve then passes names through,
// which is the behaviour before this check existed.
type categoryVocabulary struct {
	// byFold maps a case-folded, whitespace-normalised name to the canonical
	// spelling stored in the categories table, so "vegetarisch" from the model
	// resolves to the seeded "Vegetarisch" instead of creating a second row.
	byFold map[string]string
	// warned keeps the pass-through notice to once per run.
	warned bool
}

// categoryVocabulary reads the run's allowed category set. A missing lister or
// a failed read is not a reason to abort an import — the recipes are the point
// and the categories are an annotation — so both return nil and resolve says
// so once.
func (p *Pipeline) categoryVocabulary(ctx context.Context) *categoryVocabulary {
	if p.Categories == nil {
		return nil
	}
	rows, err := p.Categories.ListCategories(ctx)
	if err != nil {
		slog.Warn("import: could not read the category vocabulary; categories will not be checked this run",
			"error", err)
		return nil
	}
	v := &categoryVocabulary{byFold: make(map[string]string, len(rows))}
	for _, row := range rows {
		v.byFold[foldCategory(row.Name)] = row.Name
	}
	return v
}

// resolve maps an extraction's proposed categories onto the vocabulary,
// dropping and logging the ones that are not in it. source names the post so a
// run that drops a lot of categories can be traced back to the caption doing
// it.
func (v *categoryVocabulary) resolve(source string, proposed []string) []string {
	if len(proposed) == 0 {
		return nil
	}
	if v == nil {
		slog.Warn("import: no category vocabulary is wired; extraction categories are stored unchecked",
			"source", source, "categories", len(proposed))
		return proposed
	}
	out := make([]string, 0, len(proposed))
	var dropped []string
	for _, name := range proposed {
		if canonical, ok := v.byFold[foldCategory(name)]; ok {
			out = append(out, canonical)
			continue
		}
		dropped = append(dropped, sanitizeName(name, maxCategoryNameRunes))
	}
	if len(dropped) > 0 && !v.warned {
		// Once per run: a feed of non-German captions can legitimately propose
		// dozens, and a line per post would bury the rest of the run's log.
		v.warned = true
		slog.Info("import: dropped categories that are not in the vocabulary",
			"source", source, "dropped", dropped)
	}
	return out
}

// maxCategoryNameRunes bounds what a dropped category may put in the log — the
// name never reaches the database, but it does reach whatever reads the logs.
const maxCategoryNameRunes = 64

// foldCategory is the match key: case-insensitive and whitespace-normalised,
// so the model's "hauptgericht" and the seeded "Hauptgericht" are one category
// rather than two rows.
func foldCategory(name string) string {
	return strings.ToLower(sanitizeName(name, maxCategoryNameRunes))
}

// publishThreshold is PublishThreshold, or Threshold when it is unset.
func (p *Pipeline) publishThreshold() float64 {
	if p.PublishThreshold > 0 {
		return p.PublishThreshold
	}
	return p.Threshold
}
