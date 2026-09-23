package db_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	// Registers the "pgx" driver name with database/sql, which goose speaks.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"

	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// applicationTables is every table the migrations create.
var applicationTables = []string{
	"categories", "ingredients", "recipe_categories", "recipe_ingredients", "recipes", "units",
}

// persistence P11 + delivery D12: the down migrations were never run, so the
// only evidence that a rollback works was reading the files. This runs every
// migration up, all of them down — over a schema holding a row in every table,
// because the drop order only matters once foreign keys point at something —
// and up again, through the embedded source the binary ships.
func TestMigrations_UpDownUp(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewDatabase(t, "migrations_up_down_up")

	if err := db.Migrate(dsn); err != nil {
		t.Fatalf("up: %v", err)
	}
	pool := connect(t, dsn)
	if got := tables(t, pool); !slices.Equal(got, applicationTables) {
		t.Fatalf("tables after up = %v, want %v", got, applicationTables)
	}
	writeOneOfEverything(t, pool)
	pool.Close()

	if err := db.MigrateDown(dsn); err != nil {
		t.Fatalf("down over a populated schema: %v", err)
	}
	pool = connect(t, dsn)
	if got := tables(t, pool); len(got) != 0 {
		t.Errorf("tables left after down: %v", got)
	}
	var sequences int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind = 'S'
		   AND c.relname NOT LIKE 'goose\_db\_version%'`).Scan(&sequences); err != nil {
		t.Fatal(err)
	}
	if sequences != 0 {
		t.Errorf("%d sequences left after down, want 0", sequences)
	}
	pool.Close()

	if err := db.Migrate(dsn); err != nil {
		t.Fatalf("up again after down: %v", err)
	}
	pool = connect(t, dsn)
	defer pool.Close()
	if got := tables(t, pool); !slices.Equal(got, applicationTables) {
		t.Fatalf("tables after up, down, up = %v, want %v", got, applicationTables)
	}
	// Every later migration's constraint is back, not only 0001's tables.
	for _, name := range []string{"recipes_status_check", "recipe_ingredients_recipe_id_position_key"} {
		var present bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1)", name).Scan(&present); err != nil {
			t.Fatal(err)
		}
		if !present {
			t.Errorf("constraint %s missing after up, down, up", name)
		}
	}
	writeOneOfEverything(t, pool)
}

// Migration 0003 makes (recipe_id, position) unique, and a database written to
// by hand may already break that. Such rows are renumbered in the order they
// are read in, then the constraint applies — while a recipe listing the same
// ingredient twice, which persistence P9 left as a product question, stays
// legal on purpose.
func TestMigration0003_RenumbersThenEnforcesPosition(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewDatabase(t, "migration_0003")
	provider := fileProvider(t, dsn)
	if _, err := provider.UpTo(ctx, 2); err != nil {
		t.Fatalf("migrate to 2: %v", err)
	}
	pool := connect(t, dsn)
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
		INSERT INTO recipes (id, name, source, status) VALUES
			(1, 'Kuchen', 'src-kuchen', 'published'),
			(2, 'Brot', 'src-brot', 'published');
		INSERT INTO ingredients (id, name) VALUES (1, 'Zucker'), (2, 'Mehl');
		-- Recipe 1: two lines share position 0, and Zucker appears twice.
		INSERT INTO recipe_ingredients (id, recipe_id, ingredient_id, amount, position) VALUES
			(10, 1, 1, 200, 0),
			(11, 1, 2, 300, 0),
			(12, 1, 1, 50, 1),
		-- Recipe 2 was written by writeAssociations and is already clean.
			(20, 2, 2, 500, 0),
			(21, 2, 1, 10, 1);`); err != nil {
		t.Fatalf("seed pre-0003 rows: %v", err)
	}

	if _, err := provider.UpTo(ctx, 3); err != nil {
		t.Fatalf("migrate to 3 over colliding positions: %v", err)
	}

	for id, want := range map[int64]int32{10: 0, 11: 1, 12: 2, 20: 0, 21: 1} {
		var got int32
		if err := pool.QueryRow(ctx, "SELECT position FROM recipe_ingredients WHERE id = $1", id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("row %d position = %d after 0003, want %d", id, got, want)
		}
	}

	// The same ingredient again, on its own line, is still a legal recipe.
	if _, err := pool.Exec(ctx, "INSERT INTO recipe_ingredients (recipe_id, ingredient_id, amount, position) VALUES (1, 1, 5, 3)"); err != nil {
		t.Errorf("a repeated ingredient on a new line was refused: %v", err)
	}
	// A second line at a position already taken is not.
	_, err := pool.Exec(ctx, "INSERT INTO recipe_ingredients (recipe_id, ingredient_id, amount, position) VALUES (1, 2, 5, 0)")
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23505" {
		t.Errorf("duplicate position after 0003: err = %v, want unique_violation (23505)", err)
	}

	if _, err := provider.DownTo(ctx, 2); err != nil {
		t.Fatalf("migrate down to 2: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO recipe_ingredients (recipe_id, ingredient_id, amount, position) VALUES (1, 2, 5, 0)"); err != nil {
		t.Errorf("insert after down migration: %v, want the constraint gone", err)
	}
}

// fileProvider returns a migrator over ./migrations for dsn, closed when the
// test ends. It reads the files from disk rather than the embedded copy — the
// same files — because stepping to an exact version needs goose's own API,
// which db deliberately does not export.
func fileProvider(t *testing.T, dsn string) *goose.Provider {
	t.Helper()
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	provider, err := goose.NewProvider(database.DialectPostgres, sqlDB, os.DirFS("migrations"))
	if err != nil {
		t.Fatalf("goose.NewProvider: %v", err)
	}
	return provider
}

func connect(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := db.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return pool
}

// tables lists the application tables present, sorted.
func tables(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT tablename FROM pg_tables
		 WHERE schemaname = 'public' AND tablename <> 'goose_db_version' ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// writeOneOfEverything puts a row in every table, joined by every foreign key.
func writeOneOfEverything(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		WITH u AS (INSERT INTO units (name) VALUES ('g') RETURNING id),
		     c AS (INSERT INTO categories (name) VALUES ('Backen') RETURNING id),
		     i AS (INSERT INTO ingredients (name) VALUES ('Mehl') RETURNING id),
		     r AS (INSERT INTO recipes (name, source, status) VALUES ('Brot', 'src-brot', 'published') RETURNING id),
		     ri AS (INSERT INTO recipe_ingredients (recipe_id, ingredient_id, unit_id, amount, position)
		            SELECT r.id, i.id, u.id, 500, 0 FROM r, i, u RETURNING id)
		INSERT INTO recipe_categories (recipe_id, category_id) SELECT r.id, c.id FROM r, c`); err != nil {
		t.Fatalf("write a row to every table: %v", err)
	}
}
