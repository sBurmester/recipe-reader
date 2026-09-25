package extraction

import (
	"context"
	"errors"
	"fmt"
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
	// The fallback used to be invisible in both directions: the import tally
	// reported success while quality dropped, so an expired API key read as a
	// healthy run over weak captions.
	if !result.Degraded {
		t.Error("Degraded = false, want the fallback marked so the tally can report it")
	}
}

// Marking must not leak back into the rules extractor's own result, which the
// caller may hold a pointer to.
func TestHybridExtractor_DegradedMarkingDoesNotMutateTheRulesResult(t *testing.T) {
	rulesResult := &ExtractedRecipe{Name: "A", Confidence: 0.2}
	h := NewHybridExtractor(&stubExtractor{result: rulesResult}, &stubExtractor{err: errors.New("boom")}, 0.6)

	if _, err := h.Extract(context.Background(), "caption"); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if rulesResult.Degraded {
		t.Error("the rules extractor's own result was mutated")
	}
}

// A result good enough to skip the LLM is not degraded — nothing fell back.
func TestHybridExtractor_ConfidentRulesResultIsNotDegraded(t *testing.T) {
	h := NewHybridExtractor(&stubExtractor{result: &ExtractedRecipe{Name: "A", Confidence: 1.0}},
		&stubExtractor{err: errors.New("boom")}, 0.6)

	result, err := h.Extract(context.Background(), "caption")
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Degraded {
		t.Error("Degraded = true for a rules result that never needed the LLM")
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

// Drives the hybrid extractor with a real LLMExtractor backed by an
// OpenAI-compatible endpoint, proving the fallback path is provider-agnostic
// end to end rather than only where the seam is unit-tested.
func TestHybridExtractor_FallsBackThroughOpenAICompatibleProvider(t *testing.T) {
	srv := openAIChatStub(t, recordArgsJSON)

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
	if result.Degraded {
		t.Error("Degraded = true for a successful LLM extraction")
	}
}

// cancellingExtractor cancels the context it was given and then reports what an
// SDK reports for it, which is how a shutdown looks from inside an LLM call.
type cancellingExtractor struct{ cancel context.CancelFunc }

func (c cancellingExtractor) Extract(ctx context.Context, _ string) (*ExtractedRecipe, error) {
	c.cancel()
	<-ctx.Done()
	return nil, ctx.Err()
}

// A cancelled run is not an LLM hiccup. The fallback exists so that a failing
// provider never fails an import, but when the caller itself has been cancelled
// the weaker rules result is not wanted either: it used to come back as a
// success marked Degraded, and the pipeline then tried to store it on a context
// that was already done and counted the post as failed.
func TestHybridExtractor_CancellationIsNotAnLLMFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	rules := &stubExtractor{result: &ExtractedRecipe{Name: "A", Confidence: 0.3}}
	h := NewHybridExtractor(rules, cancellingExtractor{cancel: cancel}, 0.6)

	result, err := h.Extract(ctx, "caption")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Extract() error = %v, want it to wrap context.Canceled", err)
	}
	if result != nil {
		t.Errorf("Extract() = %+v, want no result for a cancelled run", result)
	}
}

// The counterpart, so the fix cannot over-correct: the LLM's own timeout is
// also a context error, but the caller's context is still alive, and that stays
// what the fallback is for.
func TestHybridExtractor_LLMTimeoutStillFallsBackWhileTheCallerIsAlive(t *testing.T) {
	rules := &stubExtractor{result: &ExtractedRecipe{Name: "A", Confidence: 0.3}}
	llm := &stubExtractor{err: fmt.Errorf("llm extract: %w", context.DeadlineExceeded)}
	h := NewHybridExtractor(rules, llm, 0.6)

	result, err := h.Extract(t.Context(), "caption")
	if err != nil {
		t.Fatalf("Extract() error = %v, want the rules result", err)
	}
	if result.Name != "A" || !result.Degraded {
		t.Errorf("Extract() = %+v, want the rules result marked Degraded", result)
	}
}
