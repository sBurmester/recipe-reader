// Package extraction turns freeform Instagram captions into structured recipes.
//
// The engine is layered: a fast rule-based parser (rules.go) handles captions
// that follow the common German "Zutaten:/Zubereitung:" shape, an LLM-backed
// extractor (llm.go) handles everything else, and a hybrid extractor (hybrid.go)
// runs the rules first and only falls back to the LLM when confidence is low.
package extraction

import "context"

// ExtractedIngredient is one parsed ingredient line. Unit is a normalized unit
// token (see NormalizeUnit) or "" when the line carried no recognizable unit.
type ExtractedIngredient struct {
	Name   string
	Amount float64
	Unit   string
}

// ExtractedRecipe is the structured result of running an Extractor over a
// caption. Confidence is a 0..1 estimate of how sure the extractor is that the
// caption actually described a real, complete recipe; the pipeline uses it to
// decide LLM fallback and needs_review status.
type ExtractedRecipe struct {
	Name         string
	Ingredients  []ExtractedIngredient
	Instructions string
	Categories   []string
	Confidence   float64

	// Degraded marks a result produced by a weaker path than the one that was
	// asked for — in practice, rules output returned because the LLM call
	// failed. Without it the import tally counts such a result as an ordinary
	// success, so an expired API key or a rate-limited provider reads as a
	// healthy run while extraction quality quietly drops.
	Degraded bool
}

// Extractor parses a caption into a recipe. Implementations must be safe for
// concurrent use.
type Extractor interface {
	Extract(ctx context.Context, caption string) (*ExtractedRecipe, error)
}
