package extraction

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recordArgsJSON is the record_recipe argument object both provider stubs
// return. Feeding the identical payload through each transport proves the two
// wire shapes collapse to the same ExtractedRecipe via parseToolInput.
const recordArgsJSON = `{"name":"Kürbis-Risotto",` +
	`"instructions":"Zwiebel andünsten, Kürbis zugeben, Reis garen.",` +
	`"ingredients":[{"name":"Risottoreis","amount":300,"unit":"g"}],` +
	`"categories":["Hauptgericht"]}`

// anthropicMessagesStub serves a Messages API response whose single content
// block is a record_recipe tool_use carrying recordArgsJSON as its input.
func anthropicMessagesStub(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_01", "type": "message", "role": "assistant", "model": "claude-test",
			"stop_reason": "tool_use",
			"content": []map[string]any{{
				"type": "tool_use", "id": "toolu_01", "name": "record_recipe",
				"input": json.RawMessage(recordArgsJSON),
			}},
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 20},
		})
	}))
}

// openAIChatStub serves a Chat Completions response whose only choice has a
// single record_recipe tool call with recordArgsJSON as the arguments string.
func openAIChatStub(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl_01", "object": "chat.completion", "created": 0, "model": "test",
			"choices": []map[string]any{{
				"index": 0, "finish_reason": "tool_calls",
				"message": map[string]any{
					"role": "assistant", "content": "",
					"tool_calls": []map[string]any{{
						"id": "call_01", "type": "function",
						"function": map[string]any{"name": "record_recipe", "arguments": recordArgsJSON},
					}},
				},
			}},
		})
	}))
}

func assertKuerbisRisotto(t *testing.T, got *ExtractedRecipe) {
	t.Helper()
	if got.Name != "Kürbis-Risotto" {
		t.Errorf("Name = %q, want Kürbis-Risotto", got.Name)
	}
	if len(got.Ingredients) != 1 || got.Ingredients[0].Amount != 300 || got.Ingredients[0].Unit != "g" {
		t.Errorf("Ingredients = %+v", got.Ingredients)
	}
	if len(got.Categories) != 1 {
		t.Errorf("Categories = %+v", got.Categories)
	}
	if got.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9", got.Confidence)
	}
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
	assertKuerbisRisotto(t, got)
}

func TestLLMExtractor_EmptyProviderUsesAnthropic(t *testing.T) {
	srv := anthropicMessagesStub(t)
	defer srv.Close()

	e, err := NewLLMExtractor(LLMConfig{APIKey: "test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewLLMExtractor() error = %v", err)
	}
	got, err := e.Extract(context.Background(), "irrelevant caption")
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	assertKuerbisRisotto(t, got)
}

func TestLLMExtractor_OpenAICompatibleProvider(t *testing.T) {
	srv := openAIChatStub(t)
	defer srv.Close()

	e, err := NewLLMExtractor(LLMConfig{
		Provider: ProviderOpenAI, APIKey: "test", Model: "llama-3.3-70b", BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewLLMExtractor() error = %v", err)
	}
	got, err := e.Extract(context.Background(), "irrelevant caption")
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	assertKuerbisRisotto(t, got)
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
