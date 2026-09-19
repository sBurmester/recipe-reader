package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// Migration 0002 adds a CHECK that the existing data may already violate:
// before it, both write handlers stored whatever status a client sent. A
// migration that failed on such a row would stop an existing deployment from
// starting at all, so the backfill is run against a database that took exactly
// those writes — stopped at 0001, written to, then migrated.
//
// It uses a database of its own (testdb.NewDatabase), because the shared one is
// already fully migrated.
func TestMigration0002_BackfillsThenEnforcesStatus(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewDatabase(t, "migration_0002")
	m := fileMigrator(t, dsn)
	if err := m.Migrate(1); err != nil {
		t.Fatalf("migrate to 1: %v", err)
	}
	pool := connect(t, dsn)
	defer pool.Close()

	if _, err := pool.Exec(ctx, `INSERT INTO recipes (name, source, status) VALUES
		('bogus', 'src-bogus', 'banana'),
		('empty', 'src-empty', ''),
		('published', 'src-published', 'published'),
		('review', 'src-review', 'needs_review')`); err != nil {
		t.Fatalf("seed pre-0002 rows: %v", err)
	}

	if err := m.Migrate(2); err != nil {
		t.Fatalf("migrate to 2 over out-of-domain rows: %v", err)
	}

	want := map[string]string{
		"src-bogus":     "needs_review",
		"src-empty":     "needs_review",
		"src-published": "published",
		"src-review":    "needs_review",
	}
	for source, status := range want {
		var got string
		if err := pool.QueryRow(ctx, "SELECT status FROM recipes WHERE source = $1", source).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", source, err)
		}
		if got != status {
			t.Errorf("%s status = %q after backfill, want %q", source, got, status)
		}
	}

	// And from here on the schema refuses it, whoever the writer is.
	_, err := pool.Exec(ctx, "INSERT INTO recipes (name, source, status) VALUES ('x', 'src-after', 'banana')")
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23514" {
		t.Errorf("insert with bogus status after 0002: err = %v, want check_violation (23514)", err)
	}

	// The down migration has to apply too, or a rollback is stuck at 2.
	if err := m.Migrate(1); err != nil {
		t.Fatalf("migrate down to 1: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO recipes (name, source, status) VALUES ('x', 'src-down', 'banana')"); err != nil {
		t.Errorf("insert after down migration: %v, want the constraint gone", err)
	}
}
