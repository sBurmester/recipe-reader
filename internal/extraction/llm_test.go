// internal/extraction/llm_test.go
package extraction

import "testing"

func TestParseToolInput(t *testing.T) {
	raw := []byte(`{
		"name": "Kürbis-Risotto",
		"instructions": "Zwiebel andünsten, Kürbis zugeben, Reis garen.",
		"ingredients": [
			{"name": "Risottoreis", "amount": 300, "unit": "g"},
			{"name": "Zwiebel", "amount": 1, "unit": ""}
		],
		"categories": ["Hauptgericht", "Vegetarisch"]
	}`)

	result, err := parseToolInput(raw)
	if err != nil {
		t.Fatalf("parseToolInput() error = %v", err)
	}
	if result.Name != "Kürbis-Risotto" {
		t.Errorf("Name = %q", result.Name)
	}
	if len(result.Ingredients) != 2 || result.Ingredients[0].Amount != 300 {
		t.Errorf("Ingredients = %+v", result.Ingredients)
	}
	if len(result.Categories) != 2 {
		t.Errorf("Categories = %+v", result.Categories)
	}
	if result.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9", result.Confidence)
	}
}

func TestParseToolInput_NoRecipeFound(t *testing.T) {
	raw := []byte(`{"name": "n/a", "instructions": "NO_RECIPE_FOUND", "ingredients": [], "categories": []}`)
	result, err := parseToolInput(raw)
	if err != nil {
		t.Fatalf("parseToolInput() error = %v", err)
	}
	if result.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 for NO_RECIPE_FOUND", result.Confidence)
	}
}
