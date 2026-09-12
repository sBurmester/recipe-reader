package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// defaultLLMModel is used when NewLLMExtractor is called with an empty model.
// The project default is Claude Opus 5; operators can override it (e.g. via an
// ANTHROPIC_MODEL flag) with a cheaper model for this high-volume, low-stakes
// batch extraction task.
const defaultLLMModel = "claude-opus-5"

// ErrNoRecipe reports that a caption carried no recipe. It is an outcome, not
// a failure: the extractor did its job and the answer was "there is nothing
// here".
//
// It replaces the previous signalling — Confidence 0 plus the literal string
// "NO_RECIPE_FOUND" left in Instructions — which the pipeline did not act on,
// so a gym selfie was imported as a recipe whose instructions were the
// sentinel. A typed error also cannot be defeated by a model that paraphrases
// the marker.
var ErrNoRecipe = errors.New("extraction: caption contains no recipe")

const systemPrompt = `You extract cooking recipes from Instagram captions (often German, sometimes English, with emoji and hashtags mixed in). Reply only by calling the record_recipe tool. If the caption contains no discernible recipe, call the tool with an empty ingredients list, categories set to [], confidence set to 0, and instructions set to exactly "NO_RECIPE_FOUND".`

// recordRecipeTool is the single tool the model is forced to call. Its JSON
// input schema mirrors ExtractedRecipe, confidence included: the model's own
// estimate is one of the two inputs to the score the pipeline publishes on.
var recordRecipeTool = anthropic.ToolParam{
	Name:        "record_recipe",
	Description: anthropic.String("Record the structured recipe extracted from the caption."),
	InputSchema: anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"name":         map[string]any{"type": "string"},
			"instructions": map[string]any{"type": "string"},
			"ingredients": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":   map[string]any{"type": "string"},
						"amount": map[string]any{"type": "number"},
						"unit":   map[string]any{"type": "string"},
					},
					"required": []string{"name"},
				},
			},
			"categories": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"confidence": map[string]any{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
				"description": "How certain you are that this caption contained a complete, real recipe " +
					"and that you transcribed it faithfully. Use 0 when there is no recipe. Report low " +
					"confidence freely — a low score sends the result to a human rather than discarding it.",
			},
		},
		Required: []string{"name", "instructions", "ingredients", "categories", "confidence"},
	},
}

// LLMExtractor implements Extractor by making a single forced record_recipe tool
// call against the Anthropic Messages API. It is safe for concurrent use.
type LLMExtractor struct {
	client anthropic.Client
	model  string
}

// NewLLMExtractor returns an LLMExtractor that authenticates with apiKey and
// talks to model. An empty model falls back to defaultLLMModel ("claude-opus-5").
func NewLLMExtractor(apiKey, model string) *LLMExtractor {
	if model == "" {
		model = defaultLLMModel
	}
	return &LLMExtractor{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  model,
	}
}

// Extract sends the caption to the model with tool_choice forced to
// record_recipe, so the model's structured tool input is the parsed result and
// no free-text parsing is needed.
//
// A caption with no recipe in it comes back as ErrNoRecipe.
func (e *LLMExtractor) Extract(ctx context.Context, caption string) (*ExtractedRecipe, error) {
	resp, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     e.model,
		MaxTokens: 2048,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Tools:     []anthropic.ToolUnionParam{{OfTool: &recordRecipeTool}},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{Name: "record_recipe"},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(caption)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("llm extract: %w", err)
	}

	for _, block := range resp.Content {
		toolUse, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok {
			continue
		}
		return parseToolInput(toolUse.Input)
	}
	return nil, fmt.Errorf("llm extract: no tool_use block in response")
}

// parseToolInput turns the record_recipe tool arguments (raw JSON) into an
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

// noRecipeSentinel is the instructions value the system prompt asks for when a
// caption has no recipe in it.
const noRecipeSentinel = "NO_RECIPE_FOUND"

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
