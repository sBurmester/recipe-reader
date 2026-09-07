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
