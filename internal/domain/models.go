// Package domain holds the plain data structures shared across the
// application. They carry no persistence, transport, or ORM concerns —
// repositories map them to and from sqlc-generated row types, and the API
// layer maps them to and from DTOs.
package domain

import "time"

// RecipeStatus is the editorial state of a recipe.
type RecipeStatus string

const (
	// StatusNeedsReview marks a recipe whose extracted data has not yet been
	// confirmed by a human.
	StatusNeedsReview RecipeStatus = "needs_review"
	// StatusPublished marks a recipe that has been reviewed and is visible.
	StatusPublished RecipeStatus = "published"
)

// Unit is a measurement unit (e.g. "g", "EL").
type Unit struct {
	ID   int64
	Name string
}

// Category is a recipe grouping (e.g. "Hauptgericht").
type Category struct {
	ID   int64
	Name string
}

// Ingredient is a canonical ingredient name (e.g. "Mehl").
type Ingredient struct {
	ID   int64
	Name string
}

// RecipeIngredient carries write-side fields (IngredientID/UnitID, set by
// callers via LookupRepository.FindOrCreate* before Create/Update) and
// read-side fields (IngredientName/UnitName, populated from a join by
// RecipeRepository reads) in the same struct — simpler than two types for
// what's fundamentally one row.
type RecipeIngredient struct {
	IngredientID   int64
	IngredientName string
	Amount         float64
	UnitID         *int64
	UnitName       string
}

// Recipe is a full recipe with its ingredient and category associations.
type Recipe struct {
	ID           int64
	Name         string
	Instructions string
	ImageURL     string
	Source       string
	Status       RecipeStatus
	Ingredients  []RecipeIngredient
	Categories   []Category
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
