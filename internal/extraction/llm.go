package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// defaultLLMTimeout bounds one Extract call when LLMConfig.Timeout is unset —
// the round trip and the SDK's own retries together. A forced tool call with a
// 2048-token output cap finishes well inside it on a hosted API; a local model
// on CPU may not, which is why it is configurable (LLM_TIMEOUT).
const defaultLLMTimeout = 60 * time.Second

// ErrNoRecipe reports that a caption carried no recipe. It is an outcome, not
// a failure: the extractor did its job and the answer was "there is nothing
// here".
//
// It replaces the previous signalling — Confidence 0 plus the literal string
// "NO_RECIPE_FOUND" left in Instructions — which the pipeline did not act on,
// so a gym selfie was imported as a recipe whose instructions were the
// sentinel. A typed outcome also cannot be defeated by a model that paraphrases
// the marker.
var ErrNoRecipe = errors.New("extraction: caption contains no recipe")

// noRecipeSentinel is the instructions value the system prompt asks for when a
// caption has no recipe in it.
const noRecipeSentinel = "NO_RECIPE_FOUND"

// systemPrompt instructs the model to answer only through the record_recipe
// tool/function. It is shared by every transport (see llm_provider.go).
const systemPrompt = `You extract cooking recipes from Instagram captions (often German, sometimes English, with emoji and hashtags mixed in). Reply only by calling the record_recipe tool. If the caption contains no discernible recipe, call the tool with an empty ingredients list, categories set to [], confidence set to 0, and instructions set to exactly "NO_RECIPE_FOUND".`

// LLMExtractor implements Extractor by making a single forced record_recipe
// call through a provider-specific transport (llm_provider.go). It is safe for
// concurrent use.
type LLMExtractor struct {
	client  llmClient
	timeout time.Duration
}

// NewLLMExtractor builds an LLMExtractor for cfg.Provider ("" => anthropic). It
// returns an error for an unsupported provider, or for the openai provider with
// no model id. The record_recipe schema and parseToolInput are the same
// regardless of provider. A non-positive cfg.Timeout means defaultLLMTimeout.
func NewLLMExtractor(cfg LLMConfig) (*LLMExtractor, error) {
	c, err := newLLMClient(cfg)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultLLMTimeout
	}
	return &LLMExtractor{client: c, timeout: timeout}, nil
}

// Extract obtains the raw record_recipe arguments from the configured provider
// and maps them to an ExtractedRecipe. A caption with no recipe in it comes
// back as ErrNoRecipe.
//
// Each call is bounded by the extractor's timeout, applied here once because
// every transport passes through this point. Nothing else bounds it: the
// pipeline's context is the process's signal context, or for an API-triggered
// run a context.WithoutCancel with no deadline at all, and the SDKs' retries
// only cover a server that answers. One stalled request used to stall the
// whole import — and behind Worker's single-flight guard, every import after
// it. A timed-out call returns an error wrapping context.DeadlineExceeded; the
// hybrid extractor then falls back to the rules for that post and the run
// moves on.
func (e *LLMExtractor) Extract(ctx context.Context, caption string) (*ExtractedRecipe, error) {
	if e.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}
	raw, err := e.client.recordRecipe(ctx, caption)
	if err != nil {
		return nil, err
	}
	return parseToolInput(raw)
}

// parseToolInput turns the record_recipe arguments (raw JSON) into an
// ExtractedRecipe, or ErrNoRecipe when the model reported no recipe.
//
// Confidence used to be the constant 0.9 here. Against the 0.6 threshold that
// made every LLM extraction publish unreviewed: needs_review was unreachable
// for any LLM result that contained a recipe at all, so a hallucinated
// ingredient list from a marginal caption was indistinguishable from a clean
// one. The score is now the model's own estimate tempered by what the result
// structurally looks like.
func parseToolInput(raw []byte) (*ExtractedRecipe, error) {
	var parsed struct {
		Name         string `json:"name"`
		Instructions string `json:"instructions"`
		Ingredients  []struct {
			Name   string  `json:"name"`
			Amount float64 `json:"amount"`
			Unit   string  `json:"unit"`
		} `json:"ingredients"`
		Categories []string `json:"categories"`
		Confidence float64  `json:"confidence"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("llm extract: parse tool input: %w", err)
	}

	out := &ExtractedRecipe{
		Name:         parsed.Name,
		Instructions: parsed.Instructions,
		Categories:   parsed.Categories,
	}
	for _, ing := range parsed.Ingredients {
		out.Ingredients = append(out.Ingredients, ExtractedIngredient{
			Name: ing.Name, Amount: ing.Amount, Unit: ing.Unit,
		})
	}

	// Both signals for "no recipe" are honoured: the sentinel the prompt asks
	// for, and a zero confidence on its own.
	if out.Instructions == noRecipeSentinel || parsed.Confidence <= 0 {
		return nil, ErrNoRecipe
	}

	// min, not an average: a confident model and a threadbare result should
	// produce a low score, and a rich result cannot talk an uncertain model up.
	out.Confidence = math.Min(clamp01(parsed.Confidence), structuralConfidence(out))
	if out.Confidence <= 0 {
		return nil, ErrNoRecipe
	}
	return out, nil
}

// structuralConfidence scores what the extraction physically contains, which
// is the half of the signal that does not depend on the model's self-report.
//
// It is three independent checks, each of which a real recipe passes and a
// hallucinated or half-read one usually does not: that ingredients were found
// at all, that the instructions are more than a fragment, and that the
// amounts were actually read off the caption rather than left blank.
func structuralConfidence(r *ExtractedRecipe) float64 {
	if len(r.Ingredients) == 0 || strings.TrimSpace(r.Instructions) == "" {
		return 0
	}

	score := 0.5
	// Four ingredients is where a list stops looking like a fragment; below
	// that the score scales with what was found.
	score += 0.2 * math.Min(1, float64(len(r.Ingredients))/4)
	// A single short sentence is rarely a whole method.
	if len(strings.TrimSpace(r.Instructions)) >= 80 {
		score += 0.15
	}

	withAmounts := 0
	for _, ing := range r.Ingredients {
		if ing.Amount > 0 {
			withAmounts++
		}
	}
	score += 0.15 * (float64(withAmounts) / float64(len(r.Ingredients)))

	return clamp01(score)
}

func clamp01(v float64) float64 {
	switch {
	case math.IsNaN(v), v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
