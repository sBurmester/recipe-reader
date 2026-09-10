package api

import (
	"encoding/json"
	"net/http"

	"github.com/sBurmester/recipe-reader/internal/domain"
)

// writeJSON encodes v as the response body. The encode error is dropped on
// purpose: by the time it can happen the status line is already on the wire,
// so there is nothing left to tell the client.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError reports a failure in the one shape the frontend parses, so error
// handling there never has to branch on which endpoint failed.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// IngredientDTO is one ingredient line as the frontend sees it: free text for
// the ingredient and unit names rather than lookup-table IDs, because a recipe
// form should let the user type "Zimt" without first creating an ingredient
// row. The handlers resolve those names to IDs on write.
type IngredientDTO struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

// CategoryDTO is a category association on a recipe. ID is echoed for the
// frontend's benefit; on write only Name is consulted.
type CategoryDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// UnitDTO is a row of the units lookup table. It is deliberately separate from
// IngredientDTO's free-text Unit field: this one identifies an existing unit
// for the frontend's picker, that one is whatever the user typed.
type UnitDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// IngredientLookupDTO is a row of the ingredients lookup table, used to
// populate autocomplete. Distinct from IngredientDTO, which is an ingredient
// *with an amount* attached to one recipe.
type IngredientLookupDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// RecipeDTO is the JSON shape of a recipe on every endpoint that carries one,
// in both directions. It intentionally omits CreatedAt/UpdatedAt: the UI does
// not show them, and leaving them out keeps a round-tripped PUT body from
// pretending to set server-owned timestamps.
type RecipeDTO struct {
	ID           int64           `json:"id"`
	Name         string          `json:"name"`
	Instructions string          `json:"instructions"`
	ImageURL     string          `json:"image_url"`
	Source       string          `json:"source"`
	Status       string          `json:"status"`
	Ingredients  []IngredientDTO `json:"ingredients"`
	Categories   []CategoryDTO   `json:"categories"`
}

// toRecipeDTO projects a domain recipe onto the wire shape, reading the
// join-populated IngredientName/UnitName rather than the ID fields.
func toRecipeDTO(r domain.Recipe) RecipeDTO {
	dto := RecipeDTO{
		ID: r.ID, Name: r.Name, Instructions: r.Instructions,
		ImageURL: r.ImageURL, Source: r.Source, Status: string(r.Status),
	}
	for _, ri := range r.Ingredients {
		dto.Ingredients = append(dto.Ingredients, IngredientDTO{
			Name: ri.IngredientName, Amount: ri.Amount, Unit: ri.UnitName,
		})
	}
	for _, c := range r.Categories {
		dto.Categories = append(dto.Categories, CategoryDTO{ID: c.ID, Name: c.Name})
	}
	return dto
}
