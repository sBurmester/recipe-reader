> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 2: Recipe Extraction Engine.
>
> **Status:** [x] done

# Task 8: LLM-Based Extractor (Anthropic API)

**Files:**
- Create: `internal/extraction/llm.go`
- Test: `internal/extraction/llm_test.go`

**Interfaces:**
- Consumes: `Extractor`, `ExtractedRecipe`, `ExtractedIngredient` (Task 6).
- Produces: `func NewLLMExtractor(apiKey, model string) *LLMExtractor` implementing `Extractor`. Uses a single forced tool call (`record_recipe`) so the model's structured JSON input *is* the parsed result — no free-text parsing.

Per this session's default-model policy, `model` defaults to `claude-opus-5` when empty. This is a background batch-extraction task on short captions, not a chat product, so cost-sensitive deployments may prefer swapping in `claude-sonnet-5` or `claude-haiku-4-5` via the `ANTHROPIC_MODEL` env var (Task 2) — that is the user's call to make at deploy time, not a default this code should silently apply.

- [x] **Step 1: Add the SDK dependency**

```bash
go get github.com/anthropics/anthropic-sdk-go
```

- [x] **Step 2: Write the failing test**

The live API is not called in this test — `parseToolInput` (the JSON→`ExtractedRecipe` mapping) is unit-tested directly against a fixture, since that is the only genuinely new logic; the API call itself is exercised manually per Step 5.

```go
// internal/extraction/llm_test.go
package extraction

import "testing"

func TestParseToolInput(t *testing.T) {
	raw := []byte(`{
		"name": "Kürbis-Risotto",
		"instructions": "Zwiebel andünsten, Kürbis zugeben, Reis garen.",
		"ingredients": [
			{"name": "Risottoreis", "amount": 300, "unit": "g"},
			{"name": "Zwiebel", "amount": 1, "unit": ""}
		],
		"categories": ["Hauptgericht", "Vegetarisch"]
	}`)

	result, err := parseToolInput(raw)
	if err != nil {
		t.Fatalf("parseToolInput() error = %v", err)
	}
	if result.Name != "Kürbis-Risotto" {
		t.Errorf("Name = %q", result.Name)
	}
	if len(result.Ingredients) != 2 || result.Ingredients[0].Amount != 300 {
		t.Errorf("Ingredients = %+v", result.Ingredients)
	}
	if len(result.Categories) != 2 {
		t.Errorf("Categories = %+v", result.Categories)
	}
	if result.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9", result.Confidence)
	}
}

func TestParseToolInput_NoRecipeFound(t *testing.T) {
	raw := []byte(`{"name": "n/a", "instructions": "NO_RECIPE_FOUND", "ingredients": [], "categories": []}`)
	result, err := parseToolInput(raw)
	if err != nil {
		t.Fatalf("parseToolInput() error = %v", err)
	}
	if result.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 for NO_RECIPE_FOUND", result.Confidence)
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/extraction/... -run TestParseToolInput -v`
Expected: FAIL — `parseToolInput` undefined.

- [x] **Step 4: Implement**

```go
// internal/extraction/llm.go
package extraction

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const defaultLLMModel = "claude-opus-5"

const systemPrompt = `You extract cooking recipes from Instagram captions (often German, sometimes English, with emoji and hashtags mixed in). Reply only by calling the record_recipe tool. If the caption contains no discernible recipe, call the tool with an empty ingredients list, categories set to [], and instructions set to exactly "NO_RECIPE_FOUND".`

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

type LLMExtractor struct {
	client anthropic.Client
	model  string
}

func NewLLMExtractor(apiKey, model string) *LLMExtractor {
	if model == "" {
		model = defaultLLMModel
	}
	return &LLMExtractor{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  model,
	}
}

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
		return parseToolInput([]byte(toolUse.JSON.Input.Raw()))
	}
	return nil, fmt.Errorf("llm extract: no tool_use block in response")
}

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
```

- [x] **Step 5: Run tests, then fix any SDK field-name mismatches against the compiler**

Run: `go test ./internal/extraction/... -v`

The Go SDK's exact field names for `ToolChoiceUnionParam` / `ToolChoiceToolParam` (forcing a specific tool) are not fully documented for Go in the reference material this plan was written from — the struct-literal shape above is the best-effort form. If `go build ./...` reports a compile error on the `ToolChoice:` line, run `go doc github.com/anthropics/anthropic-sdk-go ToolChoiceUnionParam` (and `ToolChoiceToolParam`) to see the installed SDK's actual field/type names, and fix the literal accordingly — the rest of the file (tool definition, message construction, `AsAny()` type switch, `JSON.Input.Raw()`) is grounded in verified SDK docs and should compile as written.

Expected once fixed: PASS for `TestParseToolInput` and `TestParseToolInput_NoRecipeFound`.

- [ ] **Step 6: Manual live-API smoke test (not part of `go test`, run once by hand)** — _deferred: needs a real `ANTHROPIC_API_KEY` and incurs cost; run once before first production import._

```bash
export ANTHROPIC_API_KEY=sk-ant-...
cat <<'GO' > /tmp/llm_smoke_test.go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sBurmester/recipe-reader/internal/extraction"
)

func main() {
	e := extraction.NewLLMExtractor(os.Getenv("ANTHROPIC_API_KEY"), "")
	result, err := e.Extract(context.Background(), "Zutaten: 2 Eier, 200g Mehl, 1 Prise Salz.\nZubereitung: Alles verrühren und in der Pfanne backen.")
	fmt.Printf("%+v %v\n", result, err)
}
GO
go run /tmp/llm_smoke_test.go
rm /tmp/llm_smoke_test.go
```

Expected: prints a populated `ExtractedRecipe` with `Confidence: 0.9` and no error. This confirms the live request/response shape against the real API; keep it as a manual check, not a CI test (it costs money and requires a real key).

- [x] **Step 7: Commit**

```bash
git add internal/extraction/llm.go internal/extraction/llm_test.go go.mod go.sum
git commit -m "$(cat <<'EOF'
feat: add LLM-based extractor using a forced Anthropic tool call

Uses claude-opus-5 by default per project convention; ANTHROPIC_MODEL
lets the operator swap in a cheaper model for this high-volume,
low-stakes batch extraction task.

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 7](07-rule-based-extractor.md) · [Task 9 →](09-hybrid-extractor.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
