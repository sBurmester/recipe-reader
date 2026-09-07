> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 2: Recipe Extraction Engine (follow-up, added after Tasks 6–9 shipped).
>
> **Status:** [ ] not started

# Task 25: Provider-Agnostic LLM Extractor (OpenAI-compatible + Anthropic)

**Requirement:** the LLM extractor **and** the hybrid extractor MUST work with LLM providers other than Anthropic. The `HybridExtractor` (Task 9) is already provider-agnostic — it depends only on the `Extractor` interface — so the real work is making `LLMExtractor` (Task 8) pluggable: keep the native Anthropic path, and add an OpenAI-compatible path that also covers Groq, Together, OpenRouter, Fireworks, a local Ollama (`/v1`), vLLM, and LM Studio, since they all speak the OpenAI Chat Completions + tool-calling dialect.

The `Extractor` interface, `ExtractedRecipe`, `ExtractedIngredient` (Task 6), `parseToolInput`, the `record_recipe` schema, the fixed `Confidence` values (0.9 / 0 on `NO_RECIPE_FOUND`), and the `HybridExtractor` contract all stay **exactly** as defined — this task changes only how the raw structured arguments are obtained from a model.

**Files:**
- Create: `internal/extraction/llm_provider.go` — the `llmClient` seam plus its Anthropic and OpenAI-compatible implementations.
- Modify: `internal/extraction/llm.go` — `LLMExtractor` delegates to an `llmClient`; new config-struct constructor.
- Modify: `internal/extraction/llm_test.go` — table-drive the parse test across both provider response shapes.
- Create: `internal/extraction/llm_provider_test.go` — httptest-backed round-trip tests for each provider, no live API.
- Modify: `internal/extraction/hybrid_test.go` — add a fallback test that drives `HybridExtractor` through a real OpenAI-compatible `LLMExtractor` (httptest server).
- Modify: `internal/config/config.go` (amends Task 2) — provider-selection fields.
- Modify: `docs/superpowers/plans/2026-09-05-recipe-reader-tasks/08-llm-based-extractor-anthropic-api.md` and the plan's Task 8/9/18 — cross-reference notes.
- Modify: `.env.example`, `README.md` — document the new env vars.

**Interfaces:**

- Consumes: `Extractor`, `ExtractedRecipe`, `ExtractedIngredient` (Task 6); `parseToolInput`, `recordRecipeTool`, `systemPrompt`, `defaultLLMModel` (Task 8).
- Introduces (package-private seam):

```go
// llmClient performs one structured-extraction round-trip: given the caption,
// it returns the raw JSON object the model produced as arguments to the
// record_recipe tool/function. The transport (Anthropic Messages API vs.
// OpenAI-compatible Chat Completions) is the only thing that varies; the
// JSON contract is identical, so parseToolInput handles both.
type llmClient interface {
	recordRecipe(ctx context.Context, caption string) (rawArgs []byte, err error)
}
```

- Produces:

```go
// LLMProvider selects the transport used by the LLM extractor.
type LLMProvider string

const (
	ProviderAnthropic LLMProvider = "anthropic" // native Anthropic Messages API (default)
	ProviderOpenAI    LLMProvider = "openai"    // any OpenAI-compatible /chat/completions endpoint
)

type LLMConfig struct {
	Provider LLMProvider // "" => ProviderAnthropic
	APIKey   string
	Model    string // "" => provider default (Anthropic: defaultLLMModel; OpenAI: required, error if empty)
	BaseURL  string // "" => provider default; set to target Groq/Together/OpenRouter/Ollama/vLLM/LM Studio
}

func NewLLMExtractor(cfg LLMConfig) (*LLMExtractor, error)
```

`LLMExtractor` keeps its `Extract(ctx, caption) (*ExtractedRecipe, error)` method and its `Extractor` conformance unchanged; internally it now holds an `llmClient` and does `raw, err := e.client.recordRecipe(ctx, caption)` then `return parseToolInput(raw)`.

> **Breaking change to Task 8's shipped API:** `NewLLMExtractor(apiKey, model string) *LLMExtractor` becomes `NewLLMExtractor(cfg LLMConfig) (*LLMExtractor, error)`. Nothing consumes the old signature yet — Task 18 (`main.go` wiring) is not built — so the blast radius is `llm.go` + `llm_test.go` only. Task 18 must construct an `LLMConfig` from `config.Config` (see Step 6).

- **Config additions** (`internal/config/config.go`, amends Task 2 — do not remove the existing `AnthropicAPIKey` / `AnthropicModel` fields, they remain the fallback):

```go
LLMProvider string `name:"llm-provider" env:"LLM_PROVIDER" default:"anthropic" help:"LLM extractor transport: anthropic or openai (openai = any OpenAI-compatible endpoint)."`
LLMAPIKey   string `name:"llm-api-key" env:"LLM_API_KEY" help:"API key for the LLM extractor; falls back to ANTHROPIC_API_KEY when provider is anthropic."`
LLMModel    string `name:"llm-model" env:"LLM_MODEL" help:"Model id for the LLM extractor; falls back to ANTHROPIC_MODEL."`
LLMBaseURL  string `name:"llm-base-url" env:"LLM_BASE_URL" help:"Override the LLM endpoint base URL (e.g. https://api.groq.com/openai/v1, http://localhost:11434/v1)."`
```

Resolution helper on `Config` (keeps "no key configured → hybrid runs rules-only" from Global Constraints intact for every provider):

```go
// LLMExtractorConfig resolves the effective LLM settings, applying the
// ANTHROPIC_* fallbacks. ok is false when no API key is configured, in
// which case main.go passes llm = nil to NewHybridExtractor.
func (c Config) LLMExtractorConfig() (provider, apiKey, model, baseURL string, ok bool) {
	provider = c.LLMProvider
	if provider == "" {
		provider = "anthropic"
	}
	apiKey = c.LLMAPIKey
	model = c.LLMModel
	if provider == "anthropic" {
		if apiKey == "" {
			apiKey = c.AnthropicAPIKey
		}
		if model == "" {
			model = c.AnthropicModel
		}
	}
	return provider, apiKey, model, c.LLMBaseURL, apiKey != ""
}
```

---

- [ ] **Step 1: Add the OpenAI SDK dependency**

```bash
go get github.com/openai/openai-go
```

Rationale: the project already depends on official vendor SDKs (`anthropic-sdk-go`). `openai-go` handles auth, retries, typed errors, and `option.WithBaseURL` for every OpenAI-compatible host. A hand-rolled `net/http` client against `/chat/completions` is an acceptable alternative if avoiding the dependency matters — the request/response shape used here (one forced function call) is small and stable — but prefer the SDK for parity with the Anthropic path.

- [ ] **Step 2: Write the failing tests**

Extend `llm_test.go` to prove `parseToolInput` is provider-neutral (it already is — this just pins it), and add `llm_provider_test.go` for the two transports. No live API: each provider client is pointed at an `httptest.Server` returning a canned response in that provider's wire format.

```go
// internal/extraction/llm_provider_test.go
package extraction

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const recordArgsJSON = `{
	"name": "Kürbis-Risotto",
	"instructions": "Zwiebel andünsten, Kürbis zugeben, Reis garen.",
	"ingredients": [{"name": "Risottoreis", "amount": 300, "unit": "g"}],
	"categories": ["Hauptgericht"]
}`

// anthropicMessagesStub returns a Messages API response whose single content
// block is a tool_use for record_recipe with recordArgsJSON as its input.
func anthropicMessagesStub(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "test",
			"stop_reason": "tool_use",
			"content": []map[string]any{{
				"type": "tool_use", "id": "toolu_1", "name": "record_recipe",
				"input": json.RawMessage(recordArgsJSON),
			}},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
}

// openAIChatStub returns a Chat Completions response whose choice has a single
// tool_calls entry for record_recipe with recordArgsJSON as the arguments string.
func openAIChatStub(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl_1", "object": "chat.completion", "model": "test",
			"choices": []map[string]any{{
				"index": 0, "finish_reason": "tool_calls",
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{{
						"id": "call_1", "type": "function",
						"function": map[string]any{"name": "record_recipe", "arguments": recordArgsJSON},
					}},
				},
			}},
		})
	}))
}

func TestLLMExtractor_AnthropicProvider(t *testing.T) {
	srv := anthropicMessagesStub(t)
	defer srv.Close()

	e, err := NewLLMExtractor(LLMConfig{Provider: ProviderAnthropic, APIKey: "test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewLLMExtractor() error = %v", err)
	}
	got, err := e.Extract(context.Background(), "irrelevant caption")
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if got.Name != "Kürbis-Risotto" || len(got.Ingredients) != 1 || got.Confidence != 0.9 {
		t.Errorf("got %+v", got)
	}
}

func TestLLMExtractor_OpenAICompatibleProvider(t *testing.T) {
	srv := openAIChatStub(t)
	defer srv.Close()

	e, err := NewLLMExtractor(LLMConfig{Provider: ProviderOpenAI, APIKey: "test", Model: "llama-3.3-70b", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewLLMExtractor() error = %v", err)
	}
	got, err := e.Extract(context.Background(), "irrelevant caption")
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if got.Name != "Kürbis-Risotto" || len(got.Ingredients) != 1 || got.Confidence != 0.9 {
		t.Errorf("got %+v", got)
	}
}

func TestNewLLMExtractor_OpenAIRequiresModel(t *testing.T) {
	if _, err := NewLLMExtractor(LLMConfig{Provider: ProviderOpenAI, APIKey: "test"}); err == nil {
		t.Fatal("expected an error when Model is empty for the openai provider")
	}
}

func TestNewLLMExtractor_UnknownProvider(t *testing.T) {
	if _, err := NewLLMExtractor(LLMConfig{Provider: "gemini", APIKey: "test"}); err == nil {
		t.Fatal("expected an error for an unsupported provider")
	}
}
```

Add to `hybrid_test.go`:

```go
func TestHybridExtractor_FallsBackThroughOpenAICompatibleProvider(t *testing.T) {
	srv := openAIChatStub(t)
	defer srv.Close()

	llm, err := NewLLMExtractor(LLMConfig{Provider: ProviderOpenAI, APIKey: "test", Model: "llama-3.3-70b", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewLLMExtractor() error = %v", err)
	}
	rules := &stubExtractor{result: &ExtractedRecipe{Name: "weak", Confidence: 0.2}}
	h := NewHybridExtractor(rules, llm, 0.6)

	got, err := h.Extract(context.Background(), "caption")
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if got.Name != "Kürbis-Risotto" {
		t.Errorf("Name = %q, want the OpenAI-provider LLM result", got.Name)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/extraction/... -run 'LLMExtractor|HybridExtractor_FallsBackThrough|NewLLMExtractor' -v`
Expected: FAIL — `LLMConfig` / `ProviderOpenAI` / new `NewLLMExtractor` signature undefined.

- [ ] **Step 4: Implement `llm_provider.go`**

```go
// internal/extraction/llm_provider.go
package extraction

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go"
	openaioption "github.com/openai/openai-go/option"
)

type llmClient interface {
	recordRecipe(ctx context.Context, caption string) ([]byte, error)
}

// --- Anthropic transport (native Messages API, forced tool call) ------------

type anthropicClient struct {
	client anthropic.Client
	model  string
}

func newAnthropicClient(cfg LLMConfig) (*anthropicClient, error) {
	model := cfg.Model
	if model == "" {
		model = defaultLLMModel
	}
	opts := []anthropicoption.RequestOption{anthropicoption.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, anthropicoption.WithBaseURL(cfg.BaseURL))
	}
	return &anthropicClient{client: anthropic.NewClient(opts...), model: model}, nil
}

func (c *anthropicClient) recordRecipe(ctx context.Context, caption string) ([]byte, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
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

// --- OpenAI-compatible transport (Chat Completions, forced function call) ---

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

// recordRecipeParameters is the record_recipe JSON Schema in the shape the
// OpenAI SDK wants (openai.FunctionParameters == map[string]any). It mirrors
// recordRecipeTool.InputSchema field-for-field.
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
		Model: c.model,
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
		ToolChoice: openai.ChatCompletionToolChoiceOptionUnionParam{
			OfChatCompletionNamedToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
				Function: openai.ChatCompletionNamedToolChoiceFunctionParam{Name: "record_recipe"},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("llm extract (openai): %w", err)
	}
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		return nil, fmt.Errorf("llm extract (openai): no tool call in response")
	}
	return []byte(resp.Choices[0].Message.ToolCalls[0].Function.Arguments), nil
}

// newLLMClient builds the transport for cfg.Provider.
func newLLMClient(cfg LLMConfig) (llmClient, error) {
	switch cfg.Provider {
	case "", ProviderAnthropic:
		return newAnthropicClient(cfg)
	case ProviderOpenAI:
		return newOpenAIClient(cfg)
	default:
		return nil, fmt.Errorf("llm extract: unsupported provider %q (want %q or %q)", cfg.Provider, ProviderAnthropic, ProviderOpenAI)
	}
}

// compile-time guard: json is used indirectly via parseToolInput; keep the
// import if a future provider needs to pre-massage arguments here.
var _ = json.Valid
```

> **SDK surface caveat (same approach as Task 8 Step 5):** the exact `openai-go` type names for a *forced* function call (`ChatCompletionToolChoiceOptionUnionParam` / `ChatCompletionNamedToolChoiceParam` / `ChatCompletionToolParam.Function`) and for reading `resp.Choices[0].Message.ToolCalls[0].Function.Arguments` are the best-effort shape from the SDK's documented Chat Completions examples. If `go build ./...` fails on any of those lines, run `go doc github.com/openai/openai-go ChatCompletionNewParams` / `go doc github.com/openai/openai-go ChatCompletionToolChoiceOptionUnionParam` against the installed version and adjust the literal. The Anthropic half is unchanged from the shipped Task 8 code and compiles as-is. Drop the `json.Valid` guard line if `llm_provider.go` ends up importing `encoding/json` for a real reason.

- [ ] **Step 5: Rewrite `llm.go` to delegate**

`llm.go` keeps `defaultLLMModel`, `systemPrompt`, `recordRecipeTool`, and `parseToolInput` **unchanged**. Replace the `LLMExtractor` struct, its constructor, and `Extract`:

```go
// LLMExtractor implements Extractor by making a single forced record_recipe
// call through a provider-specific transport (see llm_provider.go). Safe for
// concurrent use.
type LLMExtractor struct {
	client llmClient
}

// NewLLMExtractor builds an LLMExtractor for cfg.Provider ("" => anthropic).
// It returns an error for an unsupported provider, or for the openai provider
// with no model id.
func NewLLMExtractor(cfg LLMConfig) (*LLMExtractor, error) {
	c, err := newLLMClient(cfg)
	if err != nil {
		return nil, err
	}
	return &LLMExtractor{client: c}, nil
}

func (e *LLMExtractor) Extract(ctx context.Context, caption string) (*ExtractedRecipe, error) {
	raw, err := e.client.recordRecipe(ctx, caption)
	if err != nil {
		return nil, err
	}
	return parseToolInput(raw)
}
```

Delete the now-unused `anthropic` / `option` imports from `llm.go` (they moved to `llm_provider.go`). Update `llm_test.go`'s existing `TestParseToolInput` / `TestParseToolInput_NoRecipeFound` only if they referenced the old constructor — they call `parseToolInput` directly, so they should still pass untouched.

- [ ] **Step 6: Wire it through config and `main.go` (amends Task 2 & Task 18)**

Add the four fields and `LLMExtractorConfig()` from the **Interfaces** section to `internal/config/config.go`. In Task 18's `run()`, replace the LLM construction block:

```go
rules := extraction.NewRuleBasedExtractor()
var llm extraction.Extractor
if cfg.ExtractionMode == "hybrid" {
	if provider, apiKey, model, baseURL, ok := cfg.LLMExtractorConfig(); ok {
		l, err := extraction.NewLLMExtractor(extraction.LLMConfig{
			Provider: extraction.LLMProvider(provider),
			APIKey:   apiKey, Model: model, BaseURL: baseURL,
		})
		if err != nil {
			return fmt.Errorf("llm extractor: %w", err)
		}
		llm = l
	}
}
extractor := extraction.NewHybridExtractor(rules, llm, cfg.ExtractionThreshold)
```

This preserves the Global Constraints rule: no API key configured (for any provider) ⇒ `llm` stays `nil` ⇒ hybrid behaves as rules-only. A misconfigured provider now fails fast at startup instead of silently disabling the LLM.

- [ ] **Step 7: Run the full extraction suite**

Run: `go test ./internal/extraction/... ./internal/config/... -v`
Expected: PASS — `units`, `rules`, `llm` parsing, both provider round-trips, hybrid (including the new OpenAI-compatible fallback), and config resolution.

- [ ] **Step 8: Update docs**

- `.env.example`: add `LLM_PROVIDER=anthropic`, `LLM_API_KEY=`, `LLM_MODEL=`, `LLM_BASE_URL=` with a comment that `LLM_PROVIDER=openai` + `LLM_BASE_URL` targets Groq / Together / OpenRouter / Ollama / vLLM / LM Studio, and that `ANTHROPIC_API_KEY` / `ANTHROPIC_MODEL` still work as the anthropic-provider fallback.
- `README.md`: in the setup/extraction notes, state that the LLM fallback works with Anthropic or any OpenAI-compatible endpoint, selected by `LLM_PROVIDER`.
- Plan Global Constraints "Extraction" bullet: change "fall back to an LLM call (Anthropic API)" → "fall back to an LLM call (Anthropic, or any OpenAI-compatible endpoint — see Task 25)".

- [ ] **Step 9: Pre-commit gate & commit**

Run: `gofmt -w . && go vet ./... && golangci-lint run ./... && govulncheck ./... && go test ./...`

```bash
git add internal/extraction/ internal/config/config.go go.mod go.sum .env.example README.md \
  docs/superpowers/plans/2026-09-05-recipe-reader-implementation.md \
  docs/superpowers/plans/2026-09-05-recipe-reader-tasks/
git commit -m "$(cat <<'EOF'
feat: make the LLM and hybrid extractors provider-agnostic

Adds an OpenAI-compatible transport alongside the native Anthropic one,
selected by LLM_PROVIDER / LLM_BASE_URL. Covers Groq, Together, OpenRouter,
Ollama, vLLM, and LM Studio. HybridExtractor is unchanged — it already
depends only on the Extractor interface.

Assisted-by: Claude Sonnet 5 (Max) via Claude Code
EOF
)"
```

- [ ] **Step 10: Manual live smoke test (deferred, run once per provider before production)** — needs real keys and incurs cost; not part of `go test`.

```bash
LLM_PROVIDER=openai LLM_BASE_URL=https://api.groq.com/openai/v1 LLM_API_KEY=gsk_... LLM_MODEL=llama-3.3-70b-versatile \
  go run ./cmd/recipe-reader   # then trigger an import and confirm recipes extract
```

---

[← Task 9](09-hybrid-extractor.md) · [Task 10 →](10-instagram-client-wrapper-login-session-persistence.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
