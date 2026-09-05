// Package repository maps domain models to and from the sqlc-generated row
// types, owning all SQL-touching logic. Repositories return
// repository.ErrNotFound for missing single-row lookups so callers can
// branch on a stable sentinel instead of a driver-specific error.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sBurmester/recipe-reader/internal/db/sqlc"
	"github.com/sBurmester/recipe-reader/internal/domain"
)

// ErrNotFound is returned by single-row lookups when no row matches.
var ErrNotFound = errors.New("repository: not found")

// SearchQuery is the filter and pagination input for RecipeRepository.Search.
type SearchQuery struct {
	Text       string
	CategoryID *int64
	Status     *domain.RecipeStatus
	Page       int
	PageSize   int
}

// RecipeRepository persists and queries recipes together with their
// ingredient and category associations.
type RecipeRepository interface {
	Create(ctx context.Context, r *domain.Recipe) error
	GetByID(ctx context.Context, id int64) (*domain.Recipe, error)
	GetBySource(ctx context.Context, source string) (*domain.Recipe, error)
	Update(ctx context.Context, r *domain.Recipe) error
	Delete(ctx context.Context, id int64) error
	Search(ctx context.Context, q SearchQuery) ([]domain.Recipe, int64, error)
}

type pgRecipeRepository struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

// NewRecipeRepository returns a Postgres-backed RecipeRepository over pool.
func NewRecipeRepository(pool *pgxpool.Pool) RecipeRepository {
	return &pgRecipeRepository{pool: pool, queries: sqlc.New(pool)}
}

func (r *pgRecipeRepository) Create(ctx context.Context, recipe *domain.Recipe) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repository: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if Commit already succeeded

	q := r.queries.WithTx(tx)
	row, err := q.CreateRecipe(ctx, sqlc.CreateRecipeParams{
		Name: recipe.Name, Instructions: recipe.Instructions,
		ImageUrl: recipe.ImageURL, Source: recipe.Source, Status: string(recipe.Status),
	})
	if err != nil {
		return fmt.Errorf("repository: create recipe: %w", err)
	}
	recipe.ID = row.ID
	recipe.CreatedAt, recipe.UpdatedAt = row.CreatedAt.Time, row.UpdatedAt.Time

	if err := writeAssociations(ctx, q, recipe); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *pgRecipeRepository) Update(ctx context.Context, recipe *domain.Recipe) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repository: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if Commit already succeeded

	q := r.queries.WithTx(tx)
	row, err := q.UpdateRecipe(ctx, sqlc.UpdateRecipeParams{
		ID: recipe.ID, Name: recipe.Name, Instructions: recipe.Instructions,
		ImageUrl: recipe.ImageURL, Status: string(recipe.Status),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("repository: update recipe %d: %w", recipe.ID, err)
	}
	recipe.UpdatedAt = row.UpdatedAt.Time

	if err := writeAssociations(ctx, q, recipe); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// writeAssociations replaces a recipe's child rows wholesale: delete then
// reinsert, inside the caller's transaction. This is the explicit
// equivalent of what an ORM's association-replace would do — written out
// by hand here means there's no half-updated association bug to find later.
func writeAssociations(ctx context.Context, q *sqlc.Queries, recipe *domain.Recipe) error {
	if err := q.DeleteRecipeIngredients(ctx, recipe.ID); err != nil {
		return fmt.Errorf("repository: clear ingredients for recipe %d: %w", recipe.ID, err)
	}
	for i, ing := range recipe.Ingredients {
		var unitID pgtype.Int8
		if ing.UnitID != nil {
			unitID = pgtype.Int8{Int64: *ing.UnitID, Valid: true}
		}
		if _, err := q.AddRecipeIngredient(ctx, sqlc.AddRecipeIngredientParams{
			RecipeID: recipe.ID, IngredientID: ing.IngredientID,
			Amount: ing.Amount, UnitID: unitID, Position: int32(i),
		}); err != nil {
			return fmt.Errorf("repository: add ingredient to recipe %d: %w", recipe.ID, err)
		}
	}

	if err := q.DeleteRecipeCategories(ctx, recipe.ID); err != nil {
		return fmt.Errorf("repository: clear categories for recipe %d: %w", recipe.ID, err)
	}
	for _, cat := range recipe.Categories {
		if err := q.AddRecipeCategory(ctx, sqlc.AddRecipeCategoryParams{RecipeID: recipe.ID, CategoryID: cat.ID}); err != nil {
			return fmt.Errorf("repository: add category to recipe %d: %w", recipe.ID, err)
		}
	}
	return nil
}

func (r *pgRecipeRepository) GetByID(ctx context.Context, id int64) (*domain.Recipe, error) {
	row, err := r.queries.GetRecipe(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: get recipe %d: %w", id, err)
	}
	return r.assemble(ctx, row)
}

func (r *pgRecipeRepository) GetBySource(ctx context.Context, source string) (*domain.Recipe, error) {
	row, err := r.queries.GetRecipeBySource(ctx, source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: get recipe by source: %w", err)
	}
	return r.assemble(ctx, row)
}

func (r *pgRecipeRepository) Delete(ctx context.Context, id int64) error {
	n, err := r.queries.DeleteRecipe(ctx, id)
	if err != nil {
		return fmt.Errorf("repository: delete recipe %d: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *pgRecipeRepository) Search(ctx context.Context, q SearchQuery) ([]domain.Recipe, int64, error) {
	page, pageSize := q.Page, q.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	textArg := pgtype.Text{String: q.Text, Valid: q.Text != ""}
	var categoryArg pgtype.Int8
	if q.CategoryID != nil {
		categoryArg = pgtype.Int8{Int64: *q.CategoryID, Valid: true}
	}
	var statusArg pgtype.Text
	if q.Status != nil {
		statusArg = pgtype.Text{String: string(*q.Status), Valid: true}
	}

	total, err := r.queries.CountRecipes(ctx, sqlc.CountRecipesParams{
		Text: textArg, CategoryID: categoryArg, Status: statusArg,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("repository: count recipes: %w", err)
	}

	// SearchRecipes orders by (name, id): recipes.name is not unique, so id is
	// the tiebreaker that keeps paging stable across successive page fetches.
	rows, err := r.queries.SearchRecipes(ctx, sqlc.SearchRecipesParams{
		Text: textArg, CategoryID: categoryArg, Status: statusArg,
		Limit: int32(pageSize), Offset: int32((page - 1) * pageSize),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("repository: search recipes: %w", err)
	}

	// One assemble() round trip per result row (list + ingredients +
	// categories queries). Fine at personal-recipe-collection scale; if
	// this ever shows up in profiling, batch it with a single joined query
	// instead of guessing at that optimization now.
	recipes := make([]domain.Recipe, 0, len(rows))
	for _, row := range rows {
		full, err := r.assemble(ctx, row)
		if err != nil {
			return nil, 0, err
		}
		recipes = append(recipes, *full)
	}
	return recipes, total, nil
}

func (r *pgRecipeRepository) assemble(ctx context.Context, row sqlc.Recipe) (*domain.Recipe, error) {
	recipe := &domain.Recipe{
		ID: row.ID, Name: row.Name, Instructions: row.Instructions,
		ImageURL: row.ImageUrl, Source: row.Source, Status: domain.RecipeStatus(row.Status),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}

	ingredientRows, err := r.queries.ListRecipeIngredients(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("repository: list ingredients for recipe %d: %w", row.ID, err)
	}
	for _, ir := range ingredientRows {
		ing := domain.RecipeIngredient{
			IngredientID: ir.IngredientID, IngredientName: ir.IngredientName,
			Amount: ir.Amount, UnitName: ir.UnitName,
		}
		// Populate the write-side UnitID too, so a load-modify-save round trip
		// through Update preserves the unit instead of nulling it.
		if ir.UnitID.Valid {
			v := ir.UnitID.Int64
			ing.UnitID = &v
		}
		recipe.Ingredients = append(recipe.Ingredients, ing)
	}

	categoryRows, err := r.queries.ListRecipeCategories(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("repository: list categories for recipe %d: %w", row.ID, err)
	}
	for _, cr := range categoryRows {
		recipe.Categories = append(recipe.Categories, domain.Category{ID: cr.ID, Name: cr.Name})
	}

	return recipe, nil
}
