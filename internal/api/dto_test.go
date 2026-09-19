package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/domain"
)

// A recipe with no ingredients and no categories must still marshal both as
// [], never null. The frontend types these as arrays and calls .map on them
// directly, so a null takes the whole list page down rather than rendering a
// recipe with nothing attached — and a recipe with no categories is ordinary,
// since the rule-based extractor often finds none.
func TestToRecipeDTO_EmptyAssociationsMarshalAsArrays(t *testing.T) {
	body, err := json.Marshal(toRecipeDTO(domain.Recipe{ID: 1, Name: "Ohne Zutaten"}))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	got := string(body)
	for _, want := range []string{`"ingredients":[]`, `"categories":[]`} {
		if !strings.Contains(got, want) {
			t.Errorf("marshalled recipe = %s, want it to contain %s", got, want)
		}
	}
}
