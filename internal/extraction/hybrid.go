package extraction

import (
	"context"
	"errors"
	"log/slog"
)

// HybridExtractor runs the rule-based extractor first and only falls back to the
// LLM extractor when the rules result's Confidence is below Threshold. It is the
// Extractor the import pipeline uses in production.
type HybridExtractor struct {
	Rules     Extractor
	LLM       Extractor // nil disables LLM fallback entirely (rules-only)
	Threshold float64
}

// NewHybridExtractor wires rules and llm together with the given confidence
// threshold. Pass llm == nil (e.g. when no ANTHROPIC_API_KEY is configured) to
// run rules-only: Extract then always returns the rules result.
func NewHybridExtractor(rules, llm Extractor, threshold float64) *HybridExtractor {
	return &HybridExtractor{Rules: rules, LLM: llm, Threshold: threshold}
}

// Extract runs the rules extractor, and if it errored returns that error. When
// the LLM is disabled or the rules result already meets Threshold, that result
// is returned as-is. Otherwise the LLM extractor is tried; a low-confidence
// rules result is still returned if the LLM call fails, so an LLM hiccup never
// fails the whole import (the pipeline flags the weak result as needs_review).
//
// ErrNoRecipe is the exception to that fallback. It is not a hiccup — it is the
// LLM having read the caption and found no recipe, which is a better-informed
// answer than a rules pass that scraped a couple of lines out of a gym selfie.
// Returning the rules result there is precisely how junk rows got imported.
func (h *HybridExtractor) Extract(ctx context.Context, caption string) (*ExtractedRecipe, error) {
	result, err := h.Rules.Extract(ctx, caption)
	if err != nil {
		return nil, err
	}
	if h.LLM == nil || result.Confidence >= h.Threshold {
		return result, nil
	}
	llmResult, err := h.LLM.Extract(ctx, caption)
	if errors.Is(err, ErrNoRecipe) {
		return nil, err
	}
	if err != nil {
		// Say so, and mark the result. This was the worst of the three silent
		// sites: the fallback is invisible in both directions, because the
		// import tally reports success while quality degrades — so an expired
		// API key, a wrong model id or a throttled provider all look exactly
		// like a healthy run that happened to find weak captions.
		slog.Warn("extraction: LLM fallback failed; returning the weaker rules result",
			"error", err, "rules_confidence", result.Confidence)
		degraded := *result
		degraded.Degraded = true
		return &degraded, nil
	}
	return llmResult, nil
}
