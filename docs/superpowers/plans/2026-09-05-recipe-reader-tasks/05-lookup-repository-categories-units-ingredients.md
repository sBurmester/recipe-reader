> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 1: Domain & Persistence (sqlc + Postgres).
>
> **Status:** [x] done

# Task 5: Lookup Repository (Categories, Units, Ingredients)

**Files:**
- Create: `internal/repository/lookup_repository.go`
- Test: `internal/repository/lookup_repository_test.go`

**Interfaces:**
- Consumes: `domain.Unit`, `domain.Category`, `domain.Ingredient` (Task 3); `sqlc.Queries` (Task 3).
- Produces:

```go
type LookupRepository interface {
	ListCategories(ctx context.Context) ([]domain.Category, error)
	ListUnits(ctx context.Context) ([]domain.Unit, error)
	ListIngredients(ctx context.Context) ([]domain.Ingredient, error)
	FindOrCreateCategory(ctx context.Context, name string) (*domain.Category, error)
	FindOrCreateUnit(ctx context.Context, name string) (*domain.Unit, error)
	FindOrCreateIngredient(ctx context.Context, name string) (*domain.Ingredient, error)
}

func NewLookupRepository(pool *pgxpool.Pool) LookupRepository
```

Task 12 (pipeline) uses `FindOrCreate*` to resolve extracted ingredient/unit/category names into IDs before building a `domain.Recipe`. Task 16 (HTTP handlers) uses the `List*` methods.

- [x] **Step 1: Write the failing test**

```go
// internal/repository/lookup_repository_test.go
package repository

import (
	"context"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

func newTestLookupRepo(t *testing.T) LookupRepository {
	t.Helper()
	return NewLookupRepository(testdb.New(t))
}

func TestLookupRepository_FindOrCreate_Idempotent(t *testing.T) {
	ctx := context.Background()
	repo := newTestLookupRepo(t)

	first, err := repo.FindOrCreateIngredient(ctx, "Zucker")
	if err != nil {
		t.Fatalf("FindOrCreateIngredient() error = %v", err)
	}
	second, err := repo.FindOrCreateIngredient(ctx, "Zucker")
	if err != nil {
		t.Fatalf("FindOrCreateIngredient() second call error = %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("expected same ID, got %d and %d", first.ID, second.ID)
	}

	ingredients, err := repo.ListIngredients(ctx)
	if err != nil {
		t.Fatalf("ListIngredients() error = %v", err)
	}
	if len(ingredients) != 1 {
		t.Errorf("len(ingredients) = %d, want 1", len(ingredients))
	}
}

func TestLookupRepository_FindOrCreateUnitAndCategory(t *testing.T) {
	ctx := context.Background()
	repo := newTestLookupRepo(t)

	unit, err := repo.FindOrCreateUnit(ctx, "EL")
	if err != nil || unit.Name != "EL" {
		t.Fatalf("FindOrCreateUnit() = %+v, err = %v", unit, err)
	}
	cat, err := repo.FindOrCreateCategory(ctx, "Dessert")
	if err != nil || cat.Name != "Dessert" {
		t.Fatalf("FindOrCreateCategory() = %+v, err = %v", cat, err)
	}

	units, err := repo.ListUnits(ctx)
	if err != nil || len(units) != 1 {
		t.Fatalf("ListUnits() = %+v, err = %v", units, err)
	}
	cats, err := repo.ListCategories(ctx)
	if err != nil || len(cats) != 1 {
		t.Fatalf("ListCategories() = %+v, err = %v", cats, err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repository/... -run TestLookupRepository -v`
Expected: FAIL — `LookupRepository` undefined.

- [x] **Step 3: Implement**

```go
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
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repository/... -v`
Expected: PASS (full `internal/repository` suite: recipe repository + lookup repository)

- [x] **Step 5: Commit**

```bash
git add internal/repository/lookup_repository.go internal/repository/lookup_repository_test.go
git commit -m "$(cat <<'EOF'
feat: add sqlc-backed LookupRepository for categories, units, and ingredients

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 4](04-recipe-repository-crud-search.md) · [Task 6 →](06-extractor-interface-unit-normalization.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
