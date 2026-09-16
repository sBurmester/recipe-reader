package extraction

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

const richCaption = `{
	"name": "Kürbis-Risotto",
	"instructions": "Zwiebel andünsten, Kürbis zugeben, Reis garen und mit Brühe aufgießen, bis er cremig ist.",
	"ingredients": [
		{"name": "Risottoreis", "amount": 300, "unit": "g"},
		{"name": "Zwiebel", "amount": 1, "unit": ""},
		{"name": "Kürbis", "amount": 400, "unit": "g"},
		{"name": "Brühe", "amount": 1, "unit": "l"}
	],
	"categories": ["Hauptgericht", "Vegetarisch"],
	"confidence": %v
}`

// toolInput renders the complete-recipe fixture with a given self-reported
// confidence, so tests vary only the number under test.
func toolInput(confidence float64) []byte {
	return fmt.Appendf(nil, richCaption, confidence)
}

func TestParseToolInput(t *testing.T) {
	result, err := parseToolInput(toolInput(0.95))
	if err != nil {
		t.Fatalf("parseToolInput() error = %v", err)
	}
	if result.Name != "Kürbis-Risotto" {
		t.Errorf("Name = %q", result.Name)
	}
	if len(result.Ingredients) != 4 || result.Ingredients[0].Amount != 300 {
		t.Errorf("Ingredients = %+v", result.Ingredients)
	}
	if len(result.Categories) != 2 {
		t.Errorf("Categories = %+v", result.Categories)
	}
	// A complete extraction scores high, but nothing here can reach the old
	// flat 0.9 by assertion — the number now describes this result.
	if result.Confidence < 0.8 || result.Confidence > 1 {
		t.Errorf("Confidence = %v, want a high score for a complete recipe", result.Confidence)
	}
}

// The defect: Confidence was the constant 0.9 regardless of what came back, so
// against the 0.6 threshold every LLM result published unreviewed. A model
// that reports its own doubt must now be able to route a result to review.
func TestParseToolInput_LowModelConfidenceSurvives(t *testing.T) {
	result, err := parseToolInput(toolInput(0.3))
	if err != nil {
		t.Fatalf("parseToolInput() error = %v", err)
	}
	if result.Confidence > 0.3 {
		t.Errorf("Confidence = %v, want no more than the model's own 0.3", result.Confidence)
	}
}

// ...and the reverse: a confident model cannot publish a threadbare result.
func TestParseToolInput_StructureCapsAConfidentModel(t *testing.T) {
	raw := []byte(`{
		"name": "Irgendwas",
		"instructions": "Kochen.",
		"ingredients": [{"name": "Mehl"}],
		"categories": [],
		"confidence": 1.0
	}`)

	result, err := parseToolInput(raw)
	if err != nil {
		t.Fatalf("parseToolInput() error = %v", err)
	}
	if result.Confidence >= 0.8 {
		t.Errorf("Confidence = %v, want a score low enough to route one ingredient and a three-word method to review", result.Confidence)
	}
}

func TestParseToolInput_NoRecipeFound(t *testing.T) {
	raw := []byte(`{"name": "n/a", "instructions": "NO_RECIPE_FOUND", "ingredients": [], "categories": [], "confidence": 0}`)
	if _, err := parseToolInput(raw); !errors.Is(err, ErrNoRecipe) {
		t.Errorf("parseToolInput() error = %v, want ErrNoRecipe", err)
	}
}

// A model that paraphrases the sentinel but reports zero confidence must still
// be understood — which is why the typed outcome does not depend on the string.
func TestParseToolInput_ZeroConfidenceIsNoRecipe(t *testing.T) {
	raw := []byte(`{"name": "Selfie", "instructions": "Kein Rezept hier.", "ingredients": [], "categories": [], "confidence": 0}`)
	if _, err := parseToolInput(raw); !errors.Is(err, ErrNoRecipe) {
		t.Errorf("parseToolInput() error = %v, want ErrNoRecipe", err)
	}
}

// An extraction with no ingredients or no instructions scores zero
// structurally, so it is no recipe however sure the model claims to be.
func TestParseToolInput_EmptyExtractionIsNoRecipe(t *testing.T) {
	for name, raw := range map[string]string{
		"no ingredients":  `{"name":"X","instructions":"Backen und servieren.","ingredients":[],"categories":[],"confidence":0.9}`,
		"no instructions": `{"name":"X","instructions":"  ","ingredients":[{"name":"Mehl","amount":200}],"categories":[],"confidence":0.9}`,
	} {
		if _, err := parseToolInput([]byte(raw)); !errors.Is(err, ErrNoRecipe) {
			t.Errorf("%s: error = %v, want ErrNoRecipe", name, err)
		}
	}
}

// A model reporting a nonsense confidence must not produce a nonsense score.
func TestClamp01(t *testing.T) {
	for in, want := range map[float64]float64{-1: 0, 0: 0, 0.5: 0.5, 1: 1, 7: 1} {
		if got := clamp01(in); got != want {
			t.Errorf("clamp01(%v) = %v, want %v", in, got, want)
		}
	}
	if got := clamp01(math.NaN()); got != 0 {
		t.Errorf("clamp01(NaN) = %v, want 0", got)
	}
}
