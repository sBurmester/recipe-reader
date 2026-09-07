package extraction

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go"
	openaioption "github.com/openai/openai-go/option"
)

// llmClient performs one structured-extraction round-trip: given the caption it
// returns the raw JSON object the model produced as arguments to the
// record_recipe tool/function. Only the transport varies between
// implementations (the native Anthropic Messages API vs. an OpenAI-compatible
// /chat/completions endpoint); the JSON contract is identical, so
// parseToolInput consumes either one.
type llmClient interface {
	recordRecipe(ctx context.Context, caption string) ([]byte, error)
}

// LLMProvider selects the transport used by the LLM extractor.
type LLMProvider string

const (
	// ProviderAnthropic talks to the native Anthropic Messages API. It is the
	// default when LLMConfig.Provider is empty.
	ProviderAnthropic LLMProvider = "anthropic"
	// ProviderOpenAI talks to any OpenAI-compatible /chat/completions endpoint
	// (OpenAI, Groq, Together, OpenRouter, Fireworks, a local Ollama on /v1,
	// vLLM, LM Studio). Set LLMConfig.BaseURL to point at one of those.
	ProviderOpenAI LLMProvider = "openai"
)

// LLMConfig configures a provider-backed LLMExtractor.
type LLMConfig struct {
	Provider LLMProvider // "" => ProviderAnthropic
	APIKey   string
	Model    string // "" => Anthropic: defaultLLMModel; OpenAI: required (error if empty)
	BaseURL  string // "" => the provider SDK's default endpoint
}

// newLLMClient builds the transport for cfg.Provider. It errors for an unknown
// provider, or for ProviderOpenAI with no model id.
func newLLMClient(cfg LLMConfig) (llmClient, error) {
	switch cfg.Provider {
	case "", ProviderAnthropic:
		return newAnthropicClient(cfg), nil
	case ProviderOpenAI:
		return newOpenAIClient(cfg)
	default:
		return nil, fmt.Errorf("llm extract: unsupported provider %q (want %q or %q)",
			cfg.Provider, ProviderAnthropic, ProviderOpenAI)
	}
}

// --- Anthropic transport ---------------------------------------------------

// defaultLLMModel is used when LLMConfig.Model is empty and the provider is
// Anthropic. The project default is Claude Opus 5; operators can override it
// (LLM_MODEL / ANTHROPIC_MODEL) with a cheaper model for this high-volume,
// low-stakes batch extraction task.
const defaultLLMModel = "claude-opus-5"

// recordRecipeTool is the single tool the Anthropic model is forced to call.
// Its input schema mirrors ExtractedRecipe (minus Confidence, which the client
// derives) so the tool arguments unmarshal straight into a result.
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

// anthropicClient calls the Anthropic Messages API with tool_choice forced to
// record_recipe, so the model's structured tool input is the parsed result.
type anthropicClient struct {
	client anthropic.Client
	model  string
}

func newAnthropicClient(cfg LLMConfig) *anthropicClient {
	model := cfg.Model
	if model == "" {
		model = defaultLLMModel
	}
	opts := []anthropicoption.RequestOption{anthropicoption.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, anthropicoption.WithBaseURL(cfg.BaseURL))
	}
	return &anthropicClient{client: anthropic.NewClient(opts...), model: model}
}

func (c *anthropicClient) recordRecipe(ctx context.Context, caption string) ([]byte, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
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
		return nil, fmt.Errorf("llm extract (anthropic): %w", err)
	}
	for _, block := range resp.Content {
		if tu, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
			return tu.Input, nil
		}
	}
	return nil, fmt.Errorf("llm extract (anthropic): no tool_use block in response")
}

// --- OpenAI-compatible transport -----------------------------------------

// openAIClient calls an OpenAI-compatible /chat/completions endpoint with
// tool_choice forced to the record_recipe function. The function arguments the
// model returns are a JSON string that is itself the parsed result.
type openAIClient struct {
	client openai.Client
	model  string
}

func newOpenAIClient(cfg LLMConfig) (*openAIClient, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("llm extract (openai): a model id is required (set LLM_MODEL)")
	}
	opts := []openaioption.RequestOption{openaioption.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, openaioption.WithBaseURL(cfg.BaseURL))
	}
	return &openAIClient{client: openai.NewClient(opts...), model: cfg.Model}, nil
}

// recordRecipeParameters mirrors recordRecipeTool.InputSchema in the shape the
// OpenAI SDK wants: a JSON-Schema object as map[string]any.
var recordRecipeParameters = openai.FunctionParameters{
	"type": "object",
	"properties": map[string]any{
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
		"categories": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required": []string{"name", "instructions", "ingredients", "categories"},
}

func (c *openAIClient) recordRecipe(ctx context.Context, caption string) ([]byte, error) {
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:     c.model,
		MaxTokens: openai.Int(2048),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(caption),
		},
		Tools: []openai.ChatCompletionToolParam{{
			Function: openai.FunctionDefinitionParam{
				Name:        "record_recipe",
				Description: openai.String("Record the structured recipe extracted from the caption."),
				Parameters:  recordRecipeParameters,
			},
		}},
		ToolChoice: openai.ChatCompletionToolChoiceOptionParamOfChatCompletionNamedToolChoice(
			openai.ChatCompletionNamedToolChoiceFunctionParam{Name: "record_recipe"},
		),
	})
	if err != nil {
		return nil, fmt.Errorf("llm extract (openai): %w", err)
	}
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		return nil, fmt.Errorf("llm extract (openai): no tool call in response")
	}
	return []byte(resp.Choices[0].Message.ToolCalls[0].Function.Arguments), nil
}
