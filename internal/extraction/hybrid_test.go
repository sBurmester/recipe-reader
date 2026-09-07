package extraction

import (
	"context"
	"errors"
	"testing"
)

type stubExtractor struct {
	result *ExtractedRecipe
	err    error
	calls  int
}

func (s *stubExtractor) Extract(_ context.Context, _ string) (*ExtractedRecipe, error) {
	s.calls++
	return s.result, s.err
}

func TestHybridExtractor_UsesRulesWhenConfident(t *testing.T) {
	rules := &stubExtractor{result: &ExtractedRecipe{Name: "A", Confidence: 1.0}}
	llm := &stubExtractor{result: &ExtractedRecipe{Name: "B", Confidence: 0.9}}
	h := NewHybridExtractor(rules, llm, 0.6)

	result, err := h.Extract(context.Background(), "caption")
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Name != "A" {
		t.Errorf("Name = %q, want A (rules result)", result.Name)
	}
	if llm.calls != 0 {
		t.Errorf("LLM.calls = %d, want 0 (should not fall back when confident)", llm.calls)
	}
}

func TestHybridExtractor_FallsBackToLLMWhenLowConfidence(t *testing.T) {
	rules := &stubExtractor{result: &ExtractedRecipe{Name: "A", Confidence: 0.5}}
	llm := &stubExtractor{result: &ExtractedRecipe{Name: "B", Confidence: 0.9}}
	h := NewHybridExtractor(rules, llm, 0.6)

	result, err := h.Extract(context.Background(), "caption")
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Name != "B" {
		t.Errorf("Name = %q, want B (LLM result)", result.Name)
	}
	if llm.calls != 1 {
		t.Errorf("LLM.calls = %d, want 1", llm.calls)
	}
}

func TestHybridExtractor_NilLLMStaysRulesOnly(t *testing.T) {
	rules := &stubExtractor{result: &ExtractedRecipe{Name: "A", Confidence: 0.1}}
	h := NewHybridExtractor(rules, nil, 0.6)

	result, err := h.Extract(context.Background(), "caption")
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Name != "A" {
		t.Errorf("Name = %q, want A (no LLM configured)", result.Name)
	}
}

func TestHybridExtractor_LLMErrorFallsBackToRulesResult(t *testing.T) {
	rules := &stubExtractor{result: &ExtractedRecipe{Name: "A", Confidence: 0.2}}
	llm := &stubExtractor{err: errors.New("rate limited")}
	h := NewHybridExtractor(rules, llm, 0.6)

	result, err := h.Extract(context.Background(), "caption")
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil (LLM failure should not fail the whole extraction)", err)
	}
	if result.Name != "A" {
		t.Errorf("Name = %q, want A (fell back to rules result on LLM error)", result.Name)
	}
}

// TestHybridExtractor_FallsBackThroughOpenAICompatibleProvider drives the
// hybrid extractor with a real LLMExtractor backed by an OpenAI-compatible
// endpoint (httptest), proving the LLM fallback path is provider-agnostic
// end to end and not tied to Anthropic.
func TestHybridExtractor_FallsBackThroughOpenAICompatibleProvider(t *testing.T) {
	srv := openAIChatStub(t)
	defer srv.Close()

	llm, err := NewLLMExtractor(LLMConfig{
		Provider: ProviderOpenAI, APIKey: "test", Model: "llama-3.3-70b", BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewLLMExtractor() error = %v", err)
	}
	rules := &stubExtractor{result: &ExtractedRecipe{Name: "weak", Confidence: 0.2}}
	h := NewHybridExtractor(rules, llm, 0.6)

	result, err := h.Extract(context.Background(), "caption")
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Name != "Kürbis-Risotto" {
		t.Errorf("Name = %q, want Kürbis-Risotto (OpenAI-compatible LLM result)", result.Name)
	}
}
