package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sBurmester/recipe-reader/internal/db/sqlc"
)

// defaultUnits and defaultCategories are the German-language starting set the
// UI offers before any recipes are imported. Extra units/categories are
// created on demand via LookupRepository.FindOrCreate*.
var defaultUnits = []string{"g", "kg", "ml", "l", "Stück", "EL", "TL", "Prise", "Bund", "Dose", "Packung"}

var defaultCategories = []string{
	"Frühstück", "Hauptgericht", "Dessert", "Vorspeise", "Snack",
	"Vegetarisch", "Vegan", "Backen", "Getränk",
}

// Seed inserts the default units and categories. It is idempotent — the
// underlying queries use ON CONFLICT DO UPDATE, so repeated calls neither
// error nor create duplicates.
func Seed(ctx context.Context, pool *pgxpool.Pool) error {
	q := sqlc.New(pool)
	for _, name := range defaultUnits {
		if _, err := q.FindOrCreateUnit(ctx, name); err != nil {
			return fmt.Errorf("db: seed unit %q: %w", name, err)
		}
	}
	for _, name := range defaultCategories {
		if _, err := q.FindOrCreateCategory(ctx, name); err != nil {
			return fmt.Errorf("db: seed category %q: %w", name, err)
		}
	}
	return nil
}
