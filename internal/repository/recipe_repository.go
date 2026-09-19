// Package repository maps domain models to and from the sqlc-generated row
// types, owning all SQL-touching logic. Repositories return
// repository.ErrNotFound for missing single-row lookups so callers can
// branch on a stable sentinel instead of a driver-specific error.
package repository

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sBurmester/recipe-reader/internal/db/sqlc"
	"github.com/sBurmester/recipe-reader/internal/domain"
)

// ErrNotFound is returned by single-row lookups when no row matches.
var ErrNotFound = errors.New("repository: not found")

// ErrDuplicateSource is returned by Create when a recipe with the same source
// already exists. It is a normal outcome rather than a failure: source is the
// import's identity for a post, so a second insert of one is a duplicate, not
// an error — see CreateRecipe's ON CONFLICT clause.
var ErrDuplicateSource = errors.New("repository: a recipe with this source already exists")

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
	// Create returns ErrDuplicateSource when r.Source is already stored.
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

// Create inserts recipe with its ingredient and category associations in one
// transaction, resolving any association given by name rather than id (see
// resolveLookups). recipe is updated with the stored ids and timestamps only
// once the transaction has committed; after a failure it is left as passed.
//
// It returns ErrDuplicateSource when a recipe with the same source is already
// stored. Callers that import are expected to treat that as a skip.
func (r *pgRecipeRepository) Create(ctx context.Context, recipe *domain.Recipe) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repository: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if Commit already succeeded

	q := r.queries.WithTx(tx)
	staged := stage(recipe)
	row, err := q.CreateRecipe(ctx, sqlc.CreateRecipeParams{
		Name: staged.Name, Instructions: staged.Instructions,
		ImageUrl: staged.ImageURL, Source: staged.Source, Status: string(staged.Status),
	})
	// ON CONFLICT (source) DO NOTHING returns no row on a duplicate, which pgx
	// reports as ErrNoRows. That is the dedupe firing, not a failure, so it
	// gets its own sentinel — and it fires inside the transaction, before any
	// lookup row has been written, so the rollback leaves nothing behind.
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDuplicateSource
	}
	if err != nil {
		return fmt.Errorf("repository: create recipe: %w", err)
	}
	staged.ID = row.ID
	staged.CreatedAt, staged.UpdatedAt = row.CreatedAt.Time, row.UpdatedAt.Time

	if err := resolveLookups(ctx, q, &staged); err != nil {
		return err
	}
	if err := writeAssociations(ctx, q, &staged); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("repository: commit create: %w", err)
	}
	*recipe = staged
	return nil
}

// Update replaces recipe's fields and associations in one transaction, with
// the same name resolution and the same only-on-commit update of recipe as
// Create.
func (r *pgRecipeRepository) Update(ctx context.Context, recipe *domain.Recipe) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repository: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if Commit already succeeded

	q := r.queries.WithTx(tx)
	staged := stage(recipe)
	row, err := q.UpdateRecipe(ctx, sqlc.UpdateRecipeParams{
		ID: staged.ID, Name: staged.Name, Instructions: staged.Instructions,
		ImageUrl: staged.ImageURL, Status: string(staged.Status),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("repository: update recipe %d: %w", staged.ID, err)
	}
	staged.CreatedAt, staged.UpdatedAt = row.CreatedAt.Time, row.UpdatedAt.Time

	if err := resolveLookups(ctx, q, &staged); err != nil {
		return err
	}
	if err := writeAssociations(ctx, q, &staged); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("repository: commit update %d: %w", staged.ID, err)
	}
	*recipe = staged
	return nil
}

// stage copies recipe, including its association slices, so a write can fill
// in ids without touching the caller's value until the transaction commits.
func stage(recipe *domain.Recipe) domain.Recipe {
	staged := *recipe
	staged.Ingredients = slices.Clone(recipe.Ingredients)
	staged.Categories = slices.Clone(recipe.Categories)
	return staged
}

// resolveLookups fills in the lookup-table ids an association is written with,
// finding or creating the ingredient, unit or category row by name wherever
// the caller gave a name and no id.
//
// It runs on the write's own transaction, and that is the point. The handler
// and the pipeline used to resolve these on the bare pool before Create or
// Update began, so the rows were already committed by the time the recipe
// write could fail — a duplicate source, a cancelled context — and they stayed
// behind as orphans in the frontend's autocomplete pickers. Here a failed write
// rolls them back with everything else.
//
// An id wins over a name: a recipe loaded through GetByID carries both, and a
// caller that changes the id without the name means the id.
func resolveLookups(ctx context.Context, q *sqlc.Queries, recipe *domain.Recipe) error {
	for i := range recipe.Categories {
		cat := &recipe.Categories[i]
		if cat.ID != 0 {
			continue
		}
		row, err := q.FindOrCreateCategory(ctx, cat.Name)
		if err != nil {
			return fmt.Errorf("repository: find or create category %q: %w", cat.Name, err)
		}
		cat.ID, cat.Name = row.ID, row.Name
	}
	for i := range recipe.Ingredients {
		ing := &recipe.Ingredients[i]
		if ing.IngredientID == 0 {
			row, err := q.FindOrCreateIngredient(ctx, ing.IngredientName)
			if err != nil {
				return fmt.Errorf("repository: find or create ingredient %q: %w", ing.IngredientName, err)
			}
			ing.IngredientID, ing.IngredientName = row.ID, row.Name
		}
		if ing.UnitID == nil && ing.UnitName != "" {
			row, err := q.FindOrCreateUnit(ctx, ing.UnitName)
			if err != nil {
				return fmt.Errorf("repository: find or create unit %q: %w", ing.UnitName, err)
			}
			ing.UnitID, ing.UnitName = &row.ID, row.Name
		}
	}
	return nil
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

	// The OFFSET column is int32 on the wire, and page arrives from
	// strconv.Atoi on a query parameter with only its lower bound clamped. An
	// unchecked conversion wrapped: page=214748365 became offset=-16, which
	// Postgres rejects as a 500, and a value that wrapped to a small positive
	// offset was worse — it returned page 1's rows while claiming to be page
	// N, with no error at all. The guard divides rather than multiplying, so
	// the product that would overflow is never computed.
	if page-1 > math.MaxInt32/pageSize {
		// Past the last addressable page. The count still stands: this is an
		// empty page of a real result set, not a failure.
		return []domain.Recipe{}, total, nil
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

	recipes, err := r.assemblePage(ctx, rows)
	if err != nil {
		return nil, 0, err
	}
	return recipes, total, nil
}

// assemblePage fills in the associations for a whole page of search results in
// two queries, rather than the two per row assemble costs.
//
// A page used to be assembled one row at a time, so one GET /api/recipes cost
// 2 + 2N round trips: 42 at the default page size of 20, 202 at the maximum of
// 100. The in-code note that deferred this was right that the scale does not
// demand it and wrong about the constant — it is two queries per row, not one.
// Both child queries take the page's ids at once and are grouped here, so the
// page costs four queries whatever its size.
func (r *pgRecipeRepository) assemblePage(ctx context.Context, rows []sqlc.Recipe) ([]domain.Recipe, error) {
	if len(rows) == 0 {
		return []domain.Recipe{}, nil
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	ingredientRows, err := r.queries.ListIngredientsForRecipes(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("repository: list ingredients for %d recipes: %w", len(ids), err)
	}
	// The query orders by (recipe_id, position, id), so appending in row order
	// reproduces the per-recipe order the single-row query returns.
	ingredients := make(map[int64][]domain.RecipeIngredient, len(ids))
	for _, ir := range ingredientRows {
		ingredients[ir.RecipeID] = append(ingredients[ir.RecipeID],
			toRecipeIngredient(ir.IngredientID, ir.IngredientName, ir.Amount, ir.UnitID, ir.UnitName))
	}

	categoryRows, err := r.queries.ListCategoriesForRecipes(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("repository: list categories for %d recipes: %w", len(ids), err)
	}
	categories := make(map[int64][]domain.Category, len(ids))
	for _, cr := range categoryRows {
		categories[cr.RecipeID] = append(categories[cr.RecipeID], domain.Category{ID: cr.ID, Name: cr.Name})
	}

	recipes := make([]domain.Recipe, 0, len(rows))
	for _, row := range rows {
		recipe := baseRecipe(row)
		recipe.Ingredients = ingredients[row.ID]
		recipe.Categories = categories[row.ID]
		recipes = append(recipes, recipe)
	}
	return recipes, nil
}

// assemble loads one recipe's associations. GetByID and GetBySource use it;
// Search uses assemblePage, which asks the same two questions for a whole page
// at once.
func (r *pgRecipeRepository) assemble(ctx context.Context, row sqlc.Recipe) (*domain.Recipe, error) {
	recipe := baseRecipe(row)

	ingredientRows, err := r.queries.ListRecipeIngredients(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("repository: list ingredients for recipe %d: %w", row.ID, err)
	}
	for _, ir := range ingredientRows {
		recipe.Ingredients = append(recipe.Ingredients,
			toRecipeIngredient(ir.IngredientID, ir.IngredientName, ir.Amount, ir.UnitID, ir.UnitName))
	}

	categoryRows, err := r.queries.ListRecipeCategories(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("repository: list categories for recipe %d: %w", row.ID, err)
	}
	for _, cr := range categoryRows {
		recipe.Categories = append(recipe.Categories, domain.Category{ID: cr.ID, Name: cr.Name})
	}

	return &recipe, nil
}

// baseRecipe maps a recipes row onto the domain model, associations excluded.
func baseRecipe(row sqlc.Recipe) domain.Recipe {
	return domain.Recipe{
		ID: row.ID, Name: row.Name, Instructions: row.Instructions,
		ImageURL: row.ImageUrl, Source: row.Source, Status: domain.RecipeStatus(row.Status),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
}

// toRecipeIngredient builds one association from the columns both the
// single-row and the batched ingredient query return. It takes the columns
// rather than a row type because sqlc generates a distinct struct per query,
// and the two would otherwise need two copies of this mapping to drift apart.
func toRecipeIngredient(ingredientID int64, ingredientName string, amount float64, unitID pgtype.Int8, unitName string) domain.RecipeIngredient {
	ing := domain.RecipeIngredient{
		IngredientID: ingredientID, IngredientName: ingredientName,
		Amount: amount, UnitName: unitName,
	}
	// Populate the write-side UnitID too, so a load-modify-save round trip
	// through Update preserves the unit instead of nulling it.
	if unitID.Valid {
		v := unitID.Int64
		ing.UnitID = &v
	}
	return ing
}
