package extraction

import (
	"context"
	"encoding/json"
	"fmt"
)

// systemPrompt instructs the model to answer only through the record_recipe
// tool/function. It is shared by every transport (see llm_provider.go).
const systemPrompt = `You extract cooking recipes from Instagram captions (often German, sometimes English, with emoji and hashtags mixed in). Reply only by calling the record_recipe tool. If the caption contains no discernible recipe, call the tool with an empty ingredients list, categories set to [], and instructions set to exactly "NO_RECIPE_FOUND".`

// LLMExtractor implements Extractor by making a single forced record_recipe
// call through a provider-specific transport (llm_provider.go). It is safe for
// concurrent use.
type LLMExtractor struct {
	client llmClient
}

// NewLLMExtractor builds an LLMExtractor for cfg.Provider ("" => anthropic). It
// returns an error for an unsupported provider, or for the openai provider with
// no model id. The record_recipe schema, parseToolInput, and the fixed
// Confidence values are the same regardless of provider.
func NewLLMExtractor(cfg LLMConfig) (*LLMExtractor, error) {
	c, err := newLLMClient(cfg)
	if err != nil {
		return nil, err
	}
	return &LLMExtractor{client: c}, nil
}

// Extract obtains the raw record_recipe arguments from the configured provider
// and maps them to an ExtractedRecipe.
func (e *LLMExtractor) Extract(ctx context.Context, caption string) (*ExtractedRecipe, error) {
	raw, err := e.client.recordRecipe(ctx, caption)
	if err != nil {
		return nil, err
	}
	return parseToolInput(raw)
}

// parseToolInput turns the record_recipe arguments (raw JSON) into an
// ExtractedRecipe. Confidence is fixed: 0.9 for a normal result, 0 when the
// model reports no recipe via the "NO_RECIPE_FOUND" instructions sentinel.
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
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("llm extract: parse tool input: %w", err)
	}

	out := &ExtractedRecipe{
		Name:         parsed.Name,
		Instructions: parsed.Instructions,
		Categories:   parsed.Categories,
		Confidence:   0.9,
	}
	for _, ing := range parsed.Ingredients {
		out.Ingredients = append(out.Ingredients, ExtractedIngredient{
			Name: ing.Name, Amount: ing.Amount, Unit: ing.Unit,
		})
	}
	if out.Instructions == "NO_RECIPE_FOUND" {
		out.Confidence = 0
	}
	return out, nil
}
