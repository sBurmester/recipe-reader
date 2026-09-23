package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

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

// Open is what serve and migrate both start with. Against an empty database it
// has to leave the schema migrated and the lookup tables seeded, and a second
// call — the next boot — has to change nothing.
func TestOpen_MigratesAndSeedsAnEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewDatabase(t, "open_test")

	counts := make([]int, 2)
	for i := range counts {
		pool, err := db.Open(ctx, dsn)
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i+1, err)
		}
		err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM units").Scan(&counts[i])
		pool.Close()
		if err != nil {
			t.Fatalf("count units after Open() #%d: %v", i+1, err)
		}
	}
	if counts[0] == 0 {
		t.Error("Open() left the units table empty")
	}
	if counts[1] != counts[0] {
		t.Errorf("second Open() changed the seeded units: %d -> %d", counts[0], counts[1])
	}
}

// Up has no context parameter, so a cancelled migrate used to be noticed only
// by the Connect that followed it — after the schema had already changed. A
// context that is already done must leave the database exactly as it was.
func TestMigrateWithContext_CancelledContextAppliesNothing(t *testing.T) {
	dsn := testdb.NewDatabase(t, "migrate_cancelled")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := db.MigrateWithContext(ctx, dsn)
	if err == nil {
		t.Fatal("MigrateWithContext() = nil, want the cancellation reported")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("MigrateWithContext() error = %v, want it to wrap context.Canceled", err)
	}
	// The operator reads this line and has to decide what to do next. "context
	// canceled" alone reads like damage; the truth is that nothing is half
	// applied and a second run finishes the job.
	if !strings.Contains(err.Error(), "re-running") {
		t.Errorf("MigrateWithContext() error = %q, want it to say that re-running is safe", err)
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'units')").Scan(&exists); err != nil {
		t.Fatalf("look for the units table: %v", err)
	}
	if exists {
		t.Error("MigrateWithContext() applied a migration although its context was already cancelled")
	}
}
