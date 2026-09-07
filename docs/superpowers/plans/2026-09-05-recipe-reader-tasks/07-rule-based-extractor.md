> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 2: Recipe Extraction Engine.
>
> **Status:** [x] done

# Task 7: Rule-Based Extractor

**Files:**
- Create: `internal/extraction/rules.go`
- Test: `internal/extraction/rules_test.go`

**Interfaces:**
- Consumes: `Extractor`, `ExtractedRecipe`, `ExtractedIngredient`, `IsKnownUnit`, `NormalizeUnit` (Task 6).
- Produces: `func NewRuleBasedExtractor() *RuleBasedExtractor` implementing `Extractor`. Parses German `Zutaten:` / `Zubereitung:` sections; assigns `Confidence` 0.5 per non-empty ingredients list and 0.5 per non-empty instructions (max 1.0) — Task 9's hybrid extractor and Task 12's pipeline both key off this exact scoring to decide LLM fallback / `needs_review` status.

- [x] **Step 1: Write the failing test**

```go
// internal/extraction/rules_test.go
package extraction

import (
	"context"
	"testing"
)

const sampleCaption = `Cremiger Kürbis-Risotto 🎃

Zutaten:
- 300 g Risottoreis
- 1 Zwiebel
- 400 g Kürbis
2 EL Olivenöl
1 Prise Salz

Zubereitung:
Zwiebel fein hacken und in Olivenöl andünsten.
Kürbis würfeln und dazugeben.
Reis zugeben und mit Brühe ablöschen, unter Rühren garen.
Mit Salz abschmecken.

#rezept #herbstküche`

func TestRuleBasedExtractor_FullCaption(t *testing.T) {
	e := NewRuleBasedExtractor()
	result, err := e.Extract(context.Background(), sampleCaption)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Name == "" {
		t.Error("expected a non-empty recipe name")
	}
	if len(result.Ingredients) < 4 {
		t.Errorf("len(Ingredients) = %d, want >= 4: %+v", len(result.Ingredients), result.Ingredients)
	}
	foundKuerbis := false
	for _, ing := range result.Ingredients {
		if ing.Unit == "g" && ing.Amount == 400 {
			foundKuerbis = true
		}
	}
	if !foundKuerbis {
		t.Errorf("expected an ingredient '400 g ...', got %+v", result.Ingredients)
	}
	if result.Instructions == "" {
		t.Error("expected non-empty instructions")
	}
	if result.Confidence != 1.0 {
		t.Errorf("Confidence = %v, want 1.0 (both sections present)", result.Confidence)
	}
}

func TestRuleBasedExtractor_NoRecipeSections(t *testing.T) {
	e := NewRuleBasedExtractor()
	result, err := e.Extract(context.Background(), "Schönes Foto vom Urlaub! #travel #sunset")
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 for a non-recipe caption", result.Confidence)
	}
	if len(result.Ingredients) != 0 {
		t.Errorf("expected no ingredients, got %+v", result.Ingredients)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extraction/... -run TestRuleBasedExtractor -v`
Expected: FAIL — `NewRuleBasedExtractor` undefined.

- [x] **Step 3: Implement**

```go
// internal/extraction/rules.go
package extraction

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

var (
	ingredientsHeaderRe   = regexp.MustCompile(`(?i)^\s*(zutaten|ingredients)\s*[:\-]?\s*$`)
	instructionsHeaderRe  = regexp.MustCompile(`(?i)^\s*(zubereitung|anleitung|schritte|instructions)\s*[:\-]?\s*$`)
	ingredientLineRe      = regexp.MustCompile(`^\s*[-*•]?\s*(\d+(?:[.,]\d+)?)?\s*([[:alpha:]äöüÄÖÜß.]*)\s*(.+)$`)
)

type RuleBasedExtractor struct{}

func NewRuleBasedExtractor() *RuleBasedExtractor { return &RuleBasedExtractor{} }

func (e *RuleBasedExtractor) Extract(_ context.Context, caption string) (*ExtractedRecipe, error) {
	lines := strings.Split(caption, "\n")

	var ingredientLines, instructionLines []string
	section := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case ingredientsHeaderRe.MatchString(line):
			section = "ingredients"
			continue
		case instructionsHeaderRe.MatchString(line):
			section = "instructions"
			continue
		}
		if line == "" {
			continue
		}
		switch section {
		case "ingredients":
			ingredientLines = append(ingredientLines, line)
		case "instructions":
			instructionLines = append(instructionLines, line)
		}
	}

	result := &ExtractedRecipe{
		Name:         firstNonEmptyLine(lines),
		Instructions: strings.Join(instructionLines, "\n"),
	}
	for _, line := range ingredientLines {
		if ing, ok := parseIngredientLine(line); ok {
			result.Ingredients = append(result.Ingredients, ing)
		}
	}
	result.Confidence = confidenceFor(result)
	return result, nil
}

func firstNonEmptyLine(lines []string) string {
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return "Unbenanntes Rezept"
}

func parseIngredientLine(line string) (ExtractedIngredient, bool) {
	m := ingredientLineRe.FindStringSubmatch(line)
	if m == nil {
		return ExtractedIngredient{}, false
	}
	amountStr, unitCandidate, rest := m[1], strings.TrimSpace(m[2]), strings.TrimSpace(m[3])

	var amount float64
	if amountStr != "" {
		amount, _ = strconv.ParseFloat(strings.ReplaceAll(amountStr, ",", "."), 64)
	}

	unit, name := "", rest
	if unitCandidate != "" && IsKnownUnit(unitCandidate) {
		unit = NormalizeUnit(unitCandidate)
	} else if unitCandidate != "" {
		name = strings.TrimSpace(unitCandidate + " " + rest)
	}
	name = strings.TrimSpace(strings.TrimSuffix(name, "."))
	if name == "" || strings.HasPrefix(name, "#") {
		return ExtractedIngredient{}, false
	}
	return ExtractedIngredient{Name: name, Amount: amount, Unit: unit}, true
}

func confidenceFor(r *ExtractedRecipe) float64 {
	score := 0.0
	if len(r.Ingredients) > 0 {
		score += 0.5
	}
	if strings.TrimSpace(r.Instructions) != "" {
		score += 0.5
	}
	return score
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/extraction/... -v`
Expected: PASS. If `TestRuleBasedExtractor_FullCaption` fails on the ingredient count or the `400 g` match, print `result.Ingredients` with `t.Logf("%+v", result.Ingredients)` and adjust `ingredientLineRe` / `parseIngredientLine` — regex-based parsing of freeform captions is inherently approximate; iterate against the test fixtures until they pass, then add any caption shape you find failing in real usage as a new test case.

- [x] **Step 5: Commit**

```bash
git add internal/extraction/rules.go internal/extraction/rules_test.go
git commit -m "$(cat <<'EOF'
feat: add rule-based recipe extractor for German Zutaten/Zubereitung captions

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 6](06-extractor-interface-unit-normalization.md) · [Task 8 →](08-llm-based-extractor-anthropic-api.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
