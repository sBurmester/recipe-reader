package extraction

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
)

// llmClient performs one structured-extraction round-trip: given the caption it
// returns the raw JSON object the model produced as arguments to the
// record_recipe tool/function. Only the transport varies between
// implementations (the native Anthropic Messages API vs. an OpenAI-compatible
// /chat/completions endpoint); the JSON contract is identical, so
// parseToolInput consumes either one.
//
// This interface is also the seam the extractor previously lacked: the HTTP
// client used to be built inside the constructor, so Extract could not be
// driven end to end without a live paid API call, and the riskiest parsing
// code in the package went untested.
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
	// Timeout bounds one Extract call, the SDK's retries included.
	// <= 0 => defaultLLMTimeout.
	Timeout time.Duration
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

// --- the shared record_recipe schema ---------------------------------------

// recordRecipeProperties is the record_recipe property set. Both transports
// build their schema from this one value rather than each carrying a copy: the
// two wire formats differ, the contract parseToolInput relies on does not, and
// a confidence field present in one schema and missing from the other would
// silently change what the model reports.
var recordRecipeProperties = map[string]any{
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
}

// recordRecipeRequired lists the fields the model must fill in. confidence is
// required: an optional one would be omitted exactly when the model is unsure,
// which is the case the field exists for.
var recordRecipeRequired = []string{"name", "instructions", "ingredients", "categories", "confidence"}

// extractionTemperature is sent by both transports. Left unset, both APIs
// default to 1.0, which is the wrong end of the range for this job: the tool
// schema already fixes the output shape, and what is wanted inside it is
// faithful transcription, not variety. At 1.0 the same caption yields different
// ingredient splits from one run to the next, so a re-import disagrees with
// itself and any eval built on the extractor measures the sampler as much as
// the model.
//
// Set in both transports rather than one. Setting it in only one would make
// extraction reproducible on Anthropic and not on an OpenAI-compatible
// endpoint — exactly the divergence the shared record_recipe schema exists to
// prevent.
const extractionTemperature = 0

// --- Anthropic transport ---------------------------------------------------

// defaultLLMModel is used when LLMConfig.Model is empty and the provider is
// Anthropic. The project default is Claude Opus 5; operators can override it
// (LLM_MODEL / ANTHROPIC_MODEL) with a cheaper model for this high-volume,
// low-stakes batch extraction task.
const defaultLLMModel = "claude-opus-5"

// recordRecipeTool is the single tool the Anthropic model is forced to call.
var recordRecipeTool = anthropic.ToolParam{
	Name:        "record_recipe",
	Description: anthropic.String("Record the structured recipe extracted from the caption."),
	InputSchema: anthropic.ToolInputSchemaParam{
		Properties: recordRecipeProperties,
		Required:   recordRecipeRequired,
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
		Model:       anthropic.Model(c.model),
		MaxTokens:   2048,
		Temperature: anthropic.Float(extractionTemperature),
		System:      []anthropic.TextBlockParam{{Text: systemPrompt}},
		Tools:       []anthropic.ToolUnionParam{{OfTool: &recordRecipeTool}},
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
// OpenAI SDK wants: a JSON-Schema object as map[string]any, built from the same
// shared property set.
var recordRecipeParameters = openai.FunctionParameters{
	"type":       "object",
	"properties": recordRecipeProperties,
	"required":   recordRecipeRequired,
}

func (c *openAIClient) recordRecipe(ctx context.Context, caption string) ([]byte, error) {
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:       c.model,
		MaxTokens:   openai.Int(2048),
		Temperature: openai.Float(extractionTemperature),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(caption),
		},
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
				Name:        "record_recipe",
				Description: openai.String("Record the structured recipe extracted from the caption."),
				Parameters:  recordRecipeParameters,
			}),
		},
		ToolChoice: openai.ToolChoiceOptionFunctionToolChoice(
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
