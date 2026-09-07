> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 2: Recipe Extraction Engine.
>
> **Status:** [x] done

# Task 9: Hybrid Extractor

**Files:**
- Create: `internal/extraction/hybrid.go`
- Test: `internal/extraction/hybrid_test.go`

**Interfaces:**
- Consumes: `Extractor` (Task 6), `RuleBasedExtractor` (Task 7), `LLMExtractor` (Task 8).
- Produces:

```go
type HybridExtractor struct {
	Rules     Extractor
	LLM       Extractor // nil disables LLM fallback entirely
	Threshold float64
}

func NewHybridExtractor(rules, llm Extractor, threshold float64) *HybridExtractor
```

Implements `Extractor`. This is what Task 12 (pipeline) and Task 18 (`main.go` wiring) instantiate and use — `main.go` passes `llm = nil` when `config.AnthropicAPIKey == ""`.

- [x] **Step 1: Write the failing test**

```go
// internal/extraction/hybrid_test.go
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
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extraction/... -run TestHybridExtractor -v`
Expected: FAIL — `NewHybridExtractor` undefined.

- [x] **Step 3: Implement**

```go
// internal/extraction/hybrid.go
package extraction

import "context"

type HybridExtractor struct {
	Rules     Extractor
	LLM       Extractor
	Threshold float64
}

func NewHybridExtractor(rules, llm Extractor, threshold float64) *HybridExtractor {
	return &HybridExtractor{Rules: rules, LLM: llm, Threshold: threshold}
}

func (h *HybridExtractor) Extract(ctx context.Context, caption string) (*ExtractedRecipe, error) {
	result, err := h.Rules.Extract(ctx, caption)
	if err != nil {
		return nil, err
	}
	if h.LLM == nil || result.Confidence >= h.Threshold {
		return result, nil
	}
	llmResult, err := h.LLM.Extract(ctx, caption)
	if err != nil {
		// Don't fail the whole import over an LLM hiccup — keep the
		// low-confidence rules result; the pipeline marks it needs_review.
		return result, nil
	}
	return llmResult, nil
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/extraction/... -v`
Expected: PASS (all extraction package tests: units, rules, llm parsing, hybrid)

- [x] **Step 5: Commit**

```bash
git add internal/extraction/hybrid.go internal/extraction/hybrid_test.go
git commit -m "$(cat <<'EOF'
feat: add hybrid extractor (rules first, LLM fallback below confidence threshold)

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 8](08-llm-based-extractor-anthropic-api.md) · [Task 10 →](10-instagram-client-wrapper-login-session-persistence.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
