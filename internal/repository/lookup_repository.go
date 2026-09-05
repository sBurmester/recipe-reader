// internal/repository/lookup_repository.go
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sBurmester/recipe-reader/internal/db/sqlc"
	"github.com/sBurmester/recipe-reader/internal/domain"
)

type LookupRepository interface {
	ListCategories(ctx context.Context) ([]domain.Category, error)
	ListUnits(ctx context.Context) ([]domain.Unit, error)
	ListIngredients(ctx context.Context) ([]domain.Ingredient, error)
	FindOrCreateCategory(ctx context.Context, name string) (*domain.Category, error)
	FindOrCreateUnit(ctx context.Context, name string) (*domain.Unit, error)
	FindOrCreateIngredient(ctx context.Context, name string) (*domain.Ingredient, error)
}

type pgLookupRepository struct {
	queries *sqlc.Queries
}

func NewLookupRepository(pool *pgxpool.Pool) LookupRepository {
	return &pgLookupRepository{queries: sqlc.New(pool)}
}

func (r *pgLookupRepository) ListCategories(ctx context.Context) ([]domain.Category, error) {
	rows, err := r.queries.ListCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("repository: list categories: %w", err)
	}
	out := make([]domain.Category, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Category{ID: row.ID, Name: row.Name})
	}
	return out, nil
}

func (r *pgLookupRepository) ListUnits(ctx context.Context) ([]domain.Unit, error) {
	rows, err := r.queries.ListUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("repository: list units: %w", err)
	}
	out := make([]domain.Unit, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Unit{ID: row.ID, Name: row.Name})
	}
	return out, nil
}

func (r *pgLookupRepository) ListIngredients(ctx context.Context) ([]domain.Ingredient, error) {
	rows, err := r.queries.ListIngredients(ctx)
	if err != nil {
		return nil, fmt.Errorf("repository: list ingredients: %w", err)
	}
	out := make([]domain.Ingredient, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Ingredient{ID: row.ID, Name: row.Name})
	}
	return out, nil
}

func (r *pgLookupRepository) FindOrCreateCategory(ctx context.Context, name string) (*domain.Category, error) {
	row, err := r.queries.FindOrCreateCategory(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("repository: find or create category %q: %w", name, err)
	}
	return &domain.Category{ID: row.ID, Name: row.Name}, nil
}

func (r *pgLookupRepository) FindOrCreateUnit(ctx context.Context, name string) (*domain.Unit, error) {
	row, err := r.queries.FindOrCreateUnit(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("repository: find or create unit %q: %w", name, err)
	}
	return &domain.Unit{ID: row.ID, Name: row.Name}, nil
}

func (r *pgLookupRepository) FindOrCreateIngredient(ctx context.Context, name string) (*domain.Ingredient, error) {
	row, err := r.queries.FindOrCreateIngredient(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("repository: find or create ingredient %q: %w", name, err)
	}
	return &domain.Ingredient{ID: row.ID, Name: row.Name}, nil
}
