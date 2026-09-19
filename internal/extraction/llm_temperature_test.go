package extraction

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recordingStub serves body as the response and hands back a pointer the test
// reads the captured request from. The two provider stubs in
// llm_provider_test.go throw the request away, which is exactly what a test
// about what we *send* cannot do.
func recordingStub(t *testing.T, body map[string]any) (*httptest.Server, *map[string]any) {
	t.Helper()
	captured := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if err := json.Unmarshal(raw, &captured); err != nil {
			t.Errorf("unmarshal request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

func anthropicToolUseResponse() map[string]any {
	return map[string]any{
		"id": "msg_01", "type": "message", "role": "assistant", "model": "claude-test",
		"stop_reason": "tool_use",
		"content": []map[string]any{{
			"type": "tool_use", "id": "toolu_01", "name": "record_recipe",
			"input": json.RawMessage(recordArgsJSON),
		}},
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 20},
	}
}

func openAIToolCallResponse() map[string]any {
	return map[string]any{
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
	}
}

// Both transports must pin the temperature, not just one. Left unset, each API
// samples at 1.0 and the same caption yields different ingredient splits run to
// run; setting it on one provider only would make extraction reproducible on
// that provider and not on the other, which is the drift the shared
// record_recipe schema exists to prevent.
func TestRecordRecipe_SendsZeroTemperature(t *testing.T) {
	tests := []struct {
		name     string
		response map[string]any
		cfg      LLMConfig
	}{
		{
			name:     "anthropic",
			response: anthropicToolUseResponse(),
			cfg:      LLMConfig{Provider: ProviderAnthropic, APIKey: "test"},
		},
		{
			name:     "openai",
			response: openAIToolCallResponse(),
			cfg:      LLMConfig{Provider: ProviderOpenAI, APIKey: "test", Model: "llama-3.3-70b"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, captured := recordingStub(t, tc.response)
			cfg := tc.cfg
			cfg.BaseURL = srv.URL

			e, err := NewLLMExtractor(cfg)
			if err != nil {
				t.Fatalf("NewLLMExtractor() error = %v", err)
			}
			if _, err := e.Extract(context.Background(), "irrelevant caption"); err != nil {
				t.Fatalf("Extract() error = %v", err)
			}

			temperature, ok := (*captured)["temperature"]
			if !ok {
				t.Fatalf("request carried no temperature field: %+v", *captured)
			}
			if temperature != float64(0) {
				t.Errorf("temperature = %v, want 0", temperature)
			}
		})
	}
}
