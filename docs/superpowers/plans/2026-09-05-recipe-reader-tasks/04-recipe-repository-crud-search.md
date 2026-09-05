> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 1: Domain & Persistence (sqlc + Postgres).
>
> **Status:** [ ] not started

# Task 4: Recipe Repository (CRUD + Search)

**Files:**
- Create: `internal/repository/recipe_repository.go`
- Test: `internal/repository/recipe_repository_test.go`

**Interfaces:**
- Consumes: `domain.Recipe`, `domain.RecipeIngredient`, `domain.Category`, `domain.RecipeStatus` (Task 3); `sqlc.Queries` + generated types (Task 3); `testdb.New` (Task 3).
- Produces:

```go
type SearchQuery struct {
	Text       string
	CategoryID *int64
	Status     *domain.RecipeStatus
	Page       int
	PageSize   int
}

type RecipeRepository interface {
	Create(ctx context.Context, r *domain.Recipe) error
	GetByID(ctx context.Context, id int64) (*domain.Recipe, error)
	GetBySource(ctx context.Context, source string) (*domain.Recipe, error)
	Update(ctx context.Context, r *domain.Recipe) error
	Delete(ctx context.Context, id int64) error
	Search(ctx context.Context, q SearchQuery) ([]domain.Recipe, int64, error)
}

func NewRecipeRepository(pool *pgxpool.Pool) RecipeRepository
```

`GetByID`/`GetBySource` return `(nil, repository.ErrNotFound)` when missing — Task 12 (pipeline dedupe) and Task 15 (HTTP 404 mapping) both branch on this sentinel. `Create`/`Update` replace both the `Ingredients` and `Categories` child rows atomically in one transaction — there is no ORM association layer to get half-right here, so both are handled explicitly by the same `writeAssociations` helper.

- [ ] **Step 1: Write the failing test**

```go
// internal/repository/recipe_repository_test.go
package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
	"github.com/sBurmester/recipe-reader/internal/domain"
)

func newTestRecipeRepo(t *testing.T) RecipeRepository {
	t.Helper()
	pool := testdb.New(t)
	return NewRecipeRepository(pool)
}

func TestRecipeRepository_CreateGetUpdateDelete(t *testing.T) {
	ctx := context.Background()
	repo := newTestRecipeRepo(t)

	r := &domain.Recipe{Name: "Pfannkuchen", Source: "src-1", Status: domain.StatusPublished}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if r.ID == 0 {
		t.Fatal("Create() did not populate ID")
	}

	got, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Name != "Pfannkuchen" {
		t.Errorf("Name = %q, want Pfannkuchen", got.Name)
	}

	got.Name = "Pfannkuchen (süß)"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	reloaded, _ := repo.GetByID(ctx, r.ID)
	if reloaded.Name != "Pfannkuchen (süß)" {
		t.Errorf("Name after update = %q", reloaded.Name)
	}

	if err := repo.Delete(ctx, r.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := repo.GetByID(ctx, r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByID() after delete error = %v, want ErrNotFound", err)
	}
}

func TestRecipeRepository_CreateWithIngredientsAndCategories(t *testing.T) {
	ctx := context.Background()
	repo := newTestRecipeRepo(t)
	lookups := NewLookupRepository(testdb.New(t)) // separate DB instance — Task 5

	mehl, err := lookups.FindOrCreateIngredient(ctx, "Mehl")
	if err != nil {
		t.Fatalf("FindOrCreateIngredient() error = %v", err)
	}
	gramm, err := lookups.FindOrCreateUnit(ctx, "g")
	if err != nil {
		t.Fatalf("FindOrCreateUnit() error = %v", err)
	}
	backen, err := lookups.FindOrCreateCategory(ctx, "Backen")
	if err != nil {
		t.Fatalf("FindOrCreateCategory() error = %v", err)
	}

	r := &domain.Recipe{
		Name: "Brot", Source: "src-brot", Status: domain.StatusPublished,
		Categories: []domain.Category{*backen},
		Ingredients: []domain.RecipeIngredient{
			{IngredientID: mehl.ID, Amount: 500, UnitID: &gramm.ID},
		},
	}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	loaded, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if len(loaded.Ingredients) != 1 || loaded.Ingredients[0].IngredientName != "Mehl" || loaded.Ingredients[0].UnitName != "g" {
		t.Errorf("Ingredients = %+v", loaded.Ingredients)
	}
	if len(loaded.Categories) != 1 || loaded.Categories[0].Name != "Backen" {
		t.Errorf("Categories = %+v", loaded.Categories)
	}

	// Duplicate Source must be rejected — this is the pipeline's dedupe key.
	dup := &domain.Recipe{Name: "Dup", Source: "src-brot", Status: domain.StatusPublished}
	if err := repo.Create(ctx, dup); err == nil {
		t.Error("expected a unique constraint violation on duplicate Source, got nil")
	}
}

func TestRecipeRepository_Update_ReplacesIngredientsWithoutDuplicating(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	repo := NewRecipeRepository(pool)
	lookups := NewLookupRepository(pool)

	salz, _ := lookups.FindOrCreateIngredient(ctx, "Salz")
	pfeffer, _ := lookups.FindOrCreateIngredient(ctx, "Pfeffer")

	r := &domain.Recipe{
		Name: "Salat", Source: "src-salat", Status: domain.StatusPublished,
		Ingredients: []domain.RecipeIngredient{{IngredientID: salz.ID, Amount: 1}},
	}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	got.Ingredients = []domain.RecipeIngredient{{IngredientID: pfeffer.ID, Amount: 2}}
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	reloaded, err := repo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetByID() after update error = %v", err)
	}
	if len(reloaded.Ingredients) != 1 {
		t.Fatalf("Ingredients after update = %+v, want exactly 1 (old ones must be replaced, not appended)", reloaded.Ingredients)
	}
	if reloaded.Ingredients[0].IngredientName != "Pfeffer" {
		t.Errorf("Ingredients[0] = %+v, want Pfeffer", reloaded.Ingredients[0])
	}
}

func TestRecipeRepository_GetBySource_NotFound(t *testing.T) {
	repo := newTestRecipeRepo(t)
	_, err := repo.GetBySource(context.Background(), "does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestRecipeRepository_Search(t *testing.T) {
	ctx := context.Background()
	repo := newTestRecipeRepo(t)

	published := domain.StatusPublished
	for i, name := range []string{"Apfelkuchen", "Bananenbrot", "Apfelmus"} {
		r := &domain.Recipe{Name: name, Source: "src-" + name, Status: domain.StatusPublished}
		if i == 2 {
			r.Status = domain.StatusNeedsReview
		}
		if err := repo.Create(ctx, r); err != nil {
			t.Fatalf("Create(%q) error = %v", name, err)
		}
	}

	results, total, err := repo.Search(ctx, SearchQuery{Text: "apfel", PageSize: 10})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(results) != 2 {
		t.Errorf("len(results) = %d, want 2", len(results))
	}

	_, total, err = repo.Search(ctx, SearchQuery{Status: &published, PageSize: 10})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if total != 2 {
		t.Errorf("published total = %d, want 2", total)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repository/... -v`
Expected: FAIL — package doesn't compile (`RecipeRepository`, `ErrNotFound`, `NewLookupRepository` undefined; the latter arrives in Task 5, so this whole package's tests only fully pass once both Task 4 and Task 5 are done — that's fine, they're one PR-sized unit of work).

- [ ] **Step 3: Implement**

```go
// internal/repository/recipe_repository.go
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

var ErrNotFound = errors.New("repository: not found")

type SearchQuery struct {
	Text       string
	CategoryID *int64
	Status     *domain.RecipeStatus
	Page       int
	PageSize   int
}

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
	defer tx.Rollback(ctx) //nolint:errcheck

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
		recipe.Ingredients = append(recipe.Ingredients, domain.RecipeIngredient{
			IngredientID: ir.IngredientID, IngredientName: ir.IngredientName,
			Amount: ir.Amount, UnitName: ir.UnitName,
		})
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
```

`sqlc generate`'s exact field names (`ImageUrl` vs `ImageURL`, `ListRecipeIngredientsRow` field names, whether `SearchRecipes`/`CountRecipes` params share one generated struct) depend on the installed sqlc version's naming conventions — this is the same "write it, then fix against the compiler" situation as Task 3 Step 8. Run `go build ./internal/repository/...` after `sqlc generate` and correct any field-name mismatches the compiler reports; the query logic and control flow above are what matters and shouldn't need to change.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repository/... -v` (needs Docker running; each test starts its own Postgres container)
Expected: PASS once Task 5's `NewLookupRepository`/`FindOrCreate*` exist too.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/recipe_repository.go internal/repository/recipe_repository_test.go
git commit -m "$(cat <<'EOF'
feat: add sqlc-backed RecipeRepository with search, pagination, and dedupe lookup

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 3](03-database-schema-migrations-sqlc-codegen-domain-models.md) · [Task 5 →](05-lookup-repository-categories-units-ingredients.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
