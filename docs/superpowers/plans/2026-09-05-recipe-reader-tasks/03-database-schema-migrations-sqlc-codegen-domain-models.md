> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 1: Domain & Persistence (sqlc + Postgres).
>
> **Status:** [ ] not started

# Task 3: Database Schema, Migrations, sqlc Codegen & Domain Models

**Files:**
- Create: `sqlc.yaml`
- Create: `internal/db/migrations/0001_init.up.sql`, `internal/db/migrations/0001_init.down.sql`
- Create: `internal/db/queries/units.sql`, `internal/db/queries/categories.sql`, `internal/db/queries/ingredients.sql`, `internal/db/queries/recipes.sql`
- Generate: `internal/db/sqlc/*.go` (via `sqlc generate`, then committed)
- Create: `internal/domain/models.go`, `internal/db/connect.go`, `internal/db/seed.go`, `internal/db/testdb/testdb.go`
- Test: `internal/db/db_test.go`

**Interfaces:**
- Produces:

```go
// internal/domain
type RecipeStatus string
const (
	StatusNeedsReview RecipeStatus = "needs_review"
	StatusPublished   RecipeStatus = "published"
)
type Unit struct { ID int64; Name string }
type Category struct { ID int64; Name string }
type Ingredient struct { ID int64; Name string }
type RecipeIngredient struct {
	IngredientID   int64
	IngredientName string
	Amount         float64
	UnitID         *int64
	UnitName       string
}
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

// internal/db
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error)
func Migrate(dsn string) error
func Seed(ctx context.Context, pool *pgxpool.Pool) error

// internal/db/testdb
func New(t *testing.T) *pgxpool.Pool   // starts an ephemeral Postgres container, migrates it, registers cleanup
```

Every later task that touches the database (Task 4, 5, 12, 15, 16, 17, 18) imports `internal/domain` for types and either `internal/db` (connect/migrate/seed, wired once in `main.go`) or `internal/db/testdb` (in tests). `int64` is the ID type throughout — Postgres `BIGSERIAL`/`BIGINT`, not the `uint` GORM used, since there is no ORM auto-mapping doing that conversion anymore.

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/jackc/pgx/v5 github.com/golang-migrate/migrate/v4
go get github.com/testcontainers/testcontainers-go github.com/testcontainers/testcontainers-go/modules/postgres
```

- [ ] **Step 2: Write the schema migration**

```sql
-- internal/db/migrations/0001_init.up.sql
CREATE TABLE units (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE categories (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE ingredients (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE recipes (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    instructions TEXT NOT NULL DEFAULT '',
    image_url TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_recipes_name_lower ON recipes (lower(name));
CREATE INDEX idx_recipes_status ON recipes (status);

CREATE TABLE recipe_ingredients (
    id BIGSERIAL PRIMARY KEY,
    recipe_id BIGINT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    ingredient_id BIGINT NOT NULL REFERENCES ingredients(id),
    amount DOUBLE PRECISION NOT NULL DEFAULT 0,
    unit_id BIGINT REFERENCES units(id),
    position INT NOT NULL DEFAULT 0
);
CREATE INDEX idx_recipe_ingredients_recipe_id ON recipe_ingredients (recipe_id);

CREATE TABLE recipe_categories (
    recipe_id BIGINT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (recipe_id, category_id)
);
```

```sql
-- internal/db/migrations/0001_init.down.sql
DROP TABLE IF EXISTS recipe_categories;
DROP TABLE IF EXISTS recipe_ingredients;
DROP TABLE IF EXISTS recipes;
DROP TABLE IF EXISTS ingredients;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS units;
```

`default_unit_id` on ingredients (present in the original GORM model) is dropped here — nothing in the app ever reads or writes it, so it doesn't earn a place in a hand-written schema the way it might have as an unused ORM struct field. Add it back in a `0002_...` migration if a real use for it shows up.

- [ ] **Step 3: Write `sqlc.yaml`**

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "internal/db/queries"
    schema: "internal/db/migrations"
    gen:
      go:
        package: "sqlc"
        out: "internal/db/sqlc"
        sql_package: "pgx/v5"
        emit_interface: true
```

- [ ] **Step 4: Write the query files**

```sql
-- internal/db/queries/units.sql
-- name: FindOrCreateUnit :one
INSERT INTO units (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListUnits :many
SELECT * FROM units ORDER BY name;
```

```sql
-- internal/db/queries/categories.sql
-- name: FindOrCreateCategory :one
INSERT INTO categories (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListCategories :many
SELECT * FROM categories ORDER BY name;
```

```sql
-- internal/db/queries/ingredients.sql
-- name: FindOrCreateIngredient :one
INSERT INTO ingredients (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListIngredients :many
SELECT * FROM ingredients ORDER BY name;
```

```sql
-- internal/db/queries/recipes.sql
-- name: CreateRecipe :one
INSERT INTO recipes (name, instructions, image_url, source, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetRecipe :one
SELECT * FROM recipes WHERE id = $1;

-- name: GetRecipeBySource :one
SELECT * FROM recipes WHERE source = $1;

-- name: UpdateRecipe :one
UPDATE recipes
SET name = $2, instructions = $3, image_url = $4, status = $5, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteRecipe :execrows
DELETE FROM recipes WHERE id = $1;

-- name: SearchRecipes :many
SELECT DISTINCT r.* FROM recipes r
LEFT JOIN recipe_categories rc ON rc.recipe_id = r.id
WHERE (sqlc.narg(text)::text IS NULL OR lower(r.name) LIKE '%' || lower(sqlc.narg(text)::text) || '%')
  AND (sqlc.narg(category_id)::bigint IS NULL OR rc.category_id = sqlc.narg(category_id)::bigint)
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text)
ORDER BY r.name
LIMIT $1 OFFSET $2;

-- name: CountRecipes :one
SELECT COUNT(DISTINCT r.id) FROM recipes r
LEFT JOIN recipe_categories rc ON rc.recipe_id = r.id
WHERE (sqlc.narg(text)::text IS NULL OR lower(r.name) LIKE '%' || lower(sqlc.narg(text)::text) || '%')
  AND (sqlc.narg(category_id)::bigint IS NULL OR rc.category_id = sqlc.narg(category_id)::bigint)
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text);

-- name: ListRecipeIngredients :many
SELECT ri.ingredient_id, i.name AS ingredient_name, ri.amount, COALESCE(u.name, '') AS unit_name
FROM recipe_ingredients ri
JOIN ingredients i ON i.id = ri.ingredient_id
LEFT JOIN units u ON u.id = ri.unit_id
WHERE ri.recipe_id = $1
ORDER BY ri.position, ri.id;

-- name: ListRecipeCategories :many
SELECT c.id, c.name FROM categories c
JOIN recipe_categories rc ON rc.category_id = c.id
WHERE rc.recipe_id = $1
ORDER BY c.name;

-- name: AddRecipeIngredient :one
INSERT INTO recipe_ingredients (recipe_id, ingredient_id, amount, unit_id, position)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteRecipeIngredients :exec
DELETE FROM recipe_ingredients WHERE recipe_id = $1;

-- name: AddRecipeCategory :exec
INSERT INTO recipe_categories (recipe_id, category_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: DeleteRecipeCategories :exec
DELETE FROM recipe_categories WHERE recipe_id = $1;
```

`sqlc.narg(...)` marks a nullable/optional query parameter — `Search`/`Count` callers pass a `pgtype.Text`/`pgtype.Int8` with `Valid: false` for "no filter", which sqlc's generated code turns into a SQL `NULL` bound to that parameter. Task 4 shows exactly how the repository constructs these.

- [ ] **Step 5: Generate and inspect the sqlc output**

```bash
sqlc generate
```

Expected: `internal/db/sqlc/` now contains `db.go`, `models.go`, `querier.go`, and one `*.sql.go` file per query file above, with a `Queries` struct and one Go method per `-- name:` annotation. Run `go doc ./internal/db/sqlc` and skim the generated `Recipe`, `SearchRecipesParams`, `SearchRecipesRow`, and `ListRecipeIngredientsRow` struct field names — Task 4/5's code below assumes sqlc's standard `snake_case` → `PascalCase` naming (e.g. `ingredient_id` → `IngredientID`, `image_url` → `ImageUrl`); if a generated field name differs, fix the repository code to match what the compiler/`go doc` actually shows rather than guessing further.

- [ ] **Step 6: Implement `internal/domain/models.go`**

```go
package domain

import "time"

type RecipeStatus string

const (
	StatusNeedsReview RecipeStatus = "needs_review"
	StatusPublished   RecipeStatus = "published"
)

type Unit struct {
	ID   int64
	Name string
}

type Category struct {
	ID   int64
	Name string
}

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
```

- [ ] **Step 7: Implement `internal/db/connect.go`**

```go
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// Migrate applies all pending embedded migrations against dsn (a
// postgres:// URL — the same one passed to Connect).
func Migrate(dsn string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("db: migration source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("db: migration init: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	return nil
}

var _ = pgxmigrate.WithInstance // referenced only to document the intended driver; see Step 8 note
```

- [ ] **Step 8: Verify the migration driver against the installed module**

`golang-migrate`'s pgx-v5 support has moved between import paths and setup calls across versions. Run:

```bash
go doc github.com/golang-migrate/migrate/v4/database/pgx/v5
```

If `migrate.NewWithSourceInstance("iofs", src, dsn)` doesn't compile against what that shows (e.g. it wants a registered driver via a blank import like `_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"` plus a `pgx5://` DSN scheme, or an explicit `pgxmigrate.WithInstance(...)` call producing a `database.Driver` passed to `migrate.NewWithInstance`), adjust `Connect`/`Migrate` to match and delete the placeholder `var _ = pgxmigrate.WithInstance` line — it exists only to keep the import from being flagged as unused while you wire up whichever exact call shape the installed version expects. This is real, load-bearing code to get right, not a stub: don't move on until `go build ./internal/db/...` succeeds and Step 11's test passes against a real container.

- [ ] **Step 9: Implement `internal/db/seed.go`**

```go
package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sBurmester/recipe-reader/internal/db/sqlc"
)

var defaultUnits = []string{"g", "kg", "ml", "l", "Stück", "EL", "TL", "Prise", "Bund", "Dose", "Packung"}

var defaultCategories = []string{
	"Frühstück", "Hauptgericht", "Dessert", "Vorspeise", "Snack",
	"Vegetarisch", "Vegan", "Backen", "Getränk",
}

func Seed(ctx context.Context, pool *pgxpool.Pool) error {
	q := sqlc.New(pool)
	for _, name := range defaultUnits {
		if _, err := q.FindOrCreateUnit(ctx, name); err != nil {
			return err
		}
	}
	for _, name := range defaultCategories {
		if _, err := q.FindOrCreateCategory(ctx, name); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 10: Implement `internal/db/testdb/testdb.go`**

```go
package testdb

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sBurmester/recipe-reader/internal/db"
)

// New starts an ephemeral Postgres container, migrates it, and returns a
// connected pool. The container and pool are torn down via t.Cleanup.
// Requires Docker to be running — every test that calls this pays
// container-startup cost (roughly 1-2s), which is the accepted tradeoff for
// testing against real Postgres semantics instead of a stand-in.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("recipes_test"),
		tcpostgres.WithUsername("recipes"),
		tcpostgres.WithPassword("recipes"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp")),
	)
	if err != nil {
		t.Fatalf("testdb: start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("testdb: connection string: %v", err)
	}

	if err := db.Migrate(dsn); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}

	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("testdb: connect: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}
```

- [ ] **Step 11: Write and run the verification test**

```go
// internal/db/db_test.go
package db_test

import (
	"context"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

func TestMigrateConnectSeed(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	if err := db.Seed(ctx, pool); err != nil {
		t.Fatalf("Seed() error = %v", err)
	}

	var unitCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM units").Scan(&unitCount); err != nil {
		t.Fatalf("count units: %v", err)
	}
	if unitCount == 0 {
		t.Error("expected seeded units, got 0")
	}

	// Re-seeding must not duplicate (ON CONFLICT DO UPDATE keeps row count stable).
	if err := db.Seed(ctx, pool); err != nil {
		t.Fatalf("second Seed() error = %v", err)
	}
	var unitCount2 int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM units").Scan(&unitCount2); err != nil {
		t.Fatalf("count units (2nd): %v", err)
	}
	if unitCount2 != unitCount {
		t.Errorf("re-seeding created duplicates: %d -> %d", unitCount, unitCount2)
	}
}
```

Run: `go test ./internal/db/... -v` (needs Docker running)
Expected: PASS — this single test exercises `Migrate`, `Connect`, and `Seed` together against a real, disposable Postgres.

- [ ] **Step 12: Commit**

```bash
git add sqlc.yaml internal/domain internal/db go.mod go.sum
git commit -m "$(cat <<'EOF'
feat: add Postgres schema, sqlc codegen, and domain models

Replaces the earlier GORM design: sqlc generates type-safe query code
from hand-written SQL migrations; internal/domain holds plain structs
shared across the app. Postgres everywhere (dev/test/prod) — sqlc
compiles queries per-dialect, so dev-mode SQLite would mean maintaining
a second query set for every schema change.

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 2](02-configuration-package.md) · [Task 4 →](04-recipe-repository-crud-search.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
