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

	// Re-seeding must not duplicate: the rows exist, so nothing is inserted.
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

// Every test in a package now shares one container, so the isolation a fresh
// container used to provide rests on testdb.New's reset. Two parts of that are
// relied on silently elsewhere — tests that count rows, and tests that expect
// the first recipe to have id 1 — and both are checked here.
func TestTestDBNew_ResetsRowsAndSequences(t *testing.T) {
	ctx := context.Background()

	pool := testdb.New(t)
	if _, err := pool.Exec(ctx, "INSERT INTO units (name) VALUES ('left-behind-1'), ('left-behind-2')"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	pool = testdb.New(t)
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM units").Scan(&count); err != nil {
		t.Fatalf("count units: %v", err)
	}
	if count != 0 {
		t.Errorf("units after reset = %d, want 0", count)
	}
	var id int64
	if err := pool.QueryRow(ctx, "INSERT INTO units (name) VALUES ('after-reset') RETURNING id").Scan(&id); err != nil {
		t.Fatalf("insert after reset: %v", err)
	}
	if id != 1 {
		t.Errorf("first id after reset = %d, want 1 (sequence not restarted)", id)
	}
}
