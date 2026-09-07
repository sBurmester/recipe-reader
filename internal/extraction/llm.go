package extraction

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// defaultLLMModel is used when NewLLMExtractor is called with an empty model.
// The project default is Claude Opus 5; operators can override it (e.g. via an
// ANTHROPIC_MODEL flag) with a cheaper model for this high-volume, low-stakes
// batch extraction task.
const defaultLLMModel = "claude-opus-5"

const systemPrompt = `You extract cooking recipes from Instagram captions (often German, sometimes English, with emoji and hashtags mixed in). Reply only by calling the record_recipe tool. If the caption contains no discernible recipe, call the tool with an empty ingredients list, categories set to [], and instructions set to exactly "NO_RECIPE_FOUND".`

// recordRecipeTool is the single tool the model is forced to call. Its JSON
// input schema mirrors ExtractedRecipe (minus Confidence, which the client
// derives) so the tool arguments can be unmarshalled straight into a result.
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
		},
		Required: []string{"name", "instructions", "ingredients", "categories"},
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
