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
