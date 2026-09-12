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

// ErrNoRecipe is not a hiccup to fall back from. The LLM has read the caption
// and found no recipe, which beats a rules pass that scraped two lines out of
// a gym selfie — returning the rules result here is exactly how junk rows got
// imported.
func TestHybridExtractor_NoRecipeIsNotSwallowedAsAnLLMFailure(t *testing.T) {
	rules := &stubExtractor{result: &ExtractedRecipe{Name: "Erste Zeile der Bildunterschrift", Confidence: 0.5}}
	llm := &stubExtractor{err: ErrNoRecipe}
	h := NewHybridExtractor(rules, llm, 0.6)

	result, err := h.Extract(context.Background(), "caption")
	if !errors.Is(err, ErrNoRecipe) {
		t.Fatalf("Extract() error = %v, want ErrNoRecipe", err)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}
}
