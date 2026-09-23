package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recordArgsJSON is the record_recipe argument object both provider stubs
// return. Feeding the identical payload through each transport proves the two
// wire shapes collapse to the same ExtractedRecipe via parseToolInput.
const recordArgsJSON = `{"name":"Kürbis-Risotto",` +
	`"instructions":"Zwiebel andünsten, Kürbis zugeben, Reis garen und nach und nach mit heißer Brühe aufgießen, bis er cremig ist.",` +
	`"ingredients":[{"name":"Risottoreis","amount":300,"unit":"g"},` +
	`{"name":"Zwiebel","amount":1,"unit":""},` +
	`{"name":"Kürbis","amount":400,"unit":"g"},` +
	`{"name":"Brühe","amount":1,"unit":"l"}],` +
	`"categories":["Hauptgericht"],"confidence":0.95}`

// noRecipeArgsJSON is what the prompt asks a model to send for a caption with
// no recipe in it.
const noRecipeArgsJSON = `{"name":"n/a","instructions":"NO_RECIPE_FOUND",` +
	`"ingredients":[],"categories":[],"confidence":0}`

// anthropicMessagesStub serves a Messages API response whose single content
// block is a record_recipe tool_use carrying args as its input.
func anthropicMessagesStub(t *testing.T, args string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_01", "type": "message", "role": "assistant", "model": "claude-test",
			"stop_reason": "tool_use",
			"content": []map[string]any{{
				"type": "tool_use", "id": "toolu_01", "name": "record_recipe",
				"input": json.RawMessage(args),
			}},
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 20},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// openAIChatStub serves a Chat Completions response whose only choice has a
// single record_recipe tool call with args as the arguments string.
func openAIChatStub(t *testing.T, args string) *httptest.Server {
	t.Helper()
	return openAIChatStubRecording(t, args, nil)
}

// openAIChatStubRecording is openAIChatStub that also decodes the request body
// into *req, when req is not nil.
func openAIChatStubRecording(t *testing.T, args string, req *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if req != nil {
			if err := json.NewDecoder(r.Body).Decode(req); err != nil {
				t.Errorf("decode the chat completion request: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl_01", "object": "chat.completion", "created": 0, "model": "test",
			"choices": []map[string]any{{
				"index": 0, "finish_reason": "tool_calls",
				"message": map[string]any{
					"role": "assistant", "content": "",
					"tool_calls": []map[string]any{{
						"id": "call_01", "type": "function",
						"function": map[string]any{"name": "record_recipe", "arguments": args},
					}},
				},
			}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func assertKuerbisRisotto(t *testing.T, got *ExtractedRecipe) {
	t.Helper()
	if got.Name != "Kürbis-Risotto" {
		t.Errorf("Name = %q, want Kürbis-Risotto", got.Name)
	}
	if len(got.Ingredients) != 4 || got.Ingredients[0].Amount != 300 || got.Ingredients[0].Unit != "g" {
		t.Errorf("Ingredients = %+v", got.Ingredients)
	}
	if len(got.Categories) != 1 {
		t.Errorf("Categories = %+v", got.Categories)
	}
	// Derived, not asserted: a complete extraction from a confident model
	// scores high, but the number describes this result rather than being a
	// constant the extractor stamps on everything.
	if got.Confidence < 0.8 || got.Confidence > 1 {
		t.Errorf("Confidence = %v, want a high derived score", got.Confidence)
	}
}

func TestLLMExtractor_AnthropicProvider(t *testing.T) {
	srv := anthropicMessagesStub(t, recordArgsJSON)

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
	srv := anthropicMessagesStub(t, recordArgsJSON)

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
	srv := openAIChatStub(t, recordArgsJSON)

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

// The response stubs above answer whatever they are sent, so nothing else
// notices when the request changes shape — and a major version of the OpenAI
// SDK (v1 to v3) rebuilt exactly the part that says which tool the model must
// call. A provider that receives no record_recipe tool, or no tool_choice
// forcing it, answers in prose and every extraction fails at the tool-call
// check. This pins the wire shape instead of the SDK's Go types.
func TestLLMExtractor_OpenAIRequestForcesTheRecordRecipeTool(t *testing.T) {
	var req map[string]any
	srv := openAIChatStubRecording(t, recordArgsJSON, &req)

	e, err := NewLLMExtractor(LLMConfig{
		Provider: ProviderOpenAI, APIKey: "test", Model: "llama-3.3-70b", BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewLLMExtractor() error = %v", err)
	}
	if _, err := e.Extract(context.Background(), "irrelevant caption"); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	tools, _ := req["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v, want exactly one", req["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	fn, _ := tool["function"].(map[string]any)
	if tool["type"] != "function" || fn["name"] != "record_recipe" || fn["parameters"] == nil {
		t.Errorf("tools[0] = %v, want a function tool record_recipe with parameters", tool)
	}
	choice, _ := req["tool_choice"].(map[string]any)
	choiceFn, _ := choice["function"].(map[string]any)
	if choice["type"] != "function" || choiceFn["name"] != "record_recipe" {
		t.Errorf("tool_choice = %v, want the record_recipe function forced", req["tool_choice"])
	}
}

// The no-recipe outcome has to survive both transports, since it is what keeps
// a non-recipe caption from becoming a row.
func TestLLMExtractor_NoRecipeThroughEitherTransport(t *testing.T) {
	anthropic := anthropicMessagesStub(t, noRecipeArgsJSON)
	openAI := openAIChatStub(t, noRecipeArgsJSON)

	for name, cfg := range map[string]LLMConfig{
		"anthropic": {Provider: ProviderAnthropic, APIKey: "test", BaseURL: anthropic.URL},
		"openai":    {Provider: ProviderOpenAI, APIKey: "test", Model: "m", BaseURL: openAI.URL},
	} {
		e, err := NewLLMExtractor(cfg)
		if err != nil {
			t.Fatalf("%s: NewLLMExtractor() error = %v", name, err)
		}
		if _, err := e.Extract(context.Background(), "gym selfie"); !errors.Is(err, ErrNoRecipe) {
			t.Errorf("%s: Extract() error = %v, want ErrNoRecipe", name, err)
		}
	}
}

// A transport error must reach the caller rather than being mistaken for an
// empty extraction — the hybrid extractor branches on exactly this.
func TestLLMExtractor_TransportErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`, http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	e, err := NewLLMExtractor(LLMConfig{APIKey: "test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewLLMExtractor() error = %v", err)
	}
	if _, err := e.Extract(context.Background(), "caption"); err == nil {
		t.Error("Extract() error = nil, want the transport failure")
	} else if errors.Is(err, ErrNoRecipe) {
		t.Errorf("Extract() error = %v, want a transport error rather than ErrNoRecipe", err)
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

// Both transports must ask for the same fields. A confidence property present
// in one schema and missing from the other would silently change what the
// model reports depending on which provider is configured.
func TestRecordRecipeSchema_IsSharedByBothTransports(t *testing.T) {
	if _, ok := recordRecipeProperties["confidence"]; !ok {
		t.Fatal("the shared schema is missing the confidence property")
	}
	anthropicProps, ok := any(recordRecipeTool.InputSchema.Properties).(map[string]any)
	if !ok {
		t.Fatalf("unexpected Anthropic schema property type %T", recordRecipeTool.InputSchema.Properties)
	}
	openAIProps, ok := recordRecipeParameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected OpenAI schema property type %T", recordRecipeParameters["properties"])
	}
	if len(anthropicProps) != len(openAIProps) {
		t.Errorf("schemas differ: anthropic has %d properties, openai has %d", len(anthropicProps), len(openAIProps))
	}
	for name := range anthropicProps {
		if _, ok := openAIProps[name]; !ok {
			t.Errorf("property %q is in the Anthropic schema but not the OpenAI one", name)
		}
	}
}
