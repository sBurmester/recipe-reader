package db_test

import (
	"context"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// The point of the lock is what happens to the second holder, and that is a
// property of Postgres sessions — so this runs against a real database with two
// connections, the way two processes would meet. Each ImportLock opens its own.
func TestImportLock_SecondHolderIsRefusedUntilTheFirstReleases(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewDatabase(t, "import_lock")

	release, ok, err := (db.ImportLock{DSN: dsn}).TryAcquire(ctx)
	if err != nil {
		t.Fatalf("TryAcquire() first error = %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() first ok = false, want the lock to be free")
	}

	if _, ok, err := (db.ImportLock{DSN: dsn}).TryAcquire(ctx); err != nil {
		t.Fatalf("TryAcquire() second error = %v", err)
	} else if ok {
		t.Fatal("TryAcquire() second ok = true, want it refused while the first holds the lock")
	}

	release()

	release2, ok, err := (db.ImportLock{DSN: dsn}).TryAcquire(ctx)
	if err != nil {
		t.Fatalf("TryAcquire() after release error = %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() after release ok = false, want the lock free again")
	}
	release2()
}

// The lock used to live on a pooled connection, so server.Run's deferred
// pool.Close waited for it — including for an import the ten-second shutdown
// budget had just given up on. A lock that borrows nothing from the pool
// cannot hold Close up, and that is what this asserts: the pool is idle while
// the lock is held.
func TestImportLock_HoldsNoPoolConnection(t *testing.T) {
	ctx := t.Context()
	dsn := testdb.NewDatabase(t, "import_lock_pool")

	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer pool.Close()

	release, ok, err := (db.ImportLock{DSN: dsn}).TryAcquire(ctx)
	if err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() ok = false, want the lock to be free")
	}
	defer release()

	if got := pool.Stat().AcquiredConns(); got != 0 {
		t.Errorf("pool has %d connection(s) checked out while the lock is held, want 0", got)
	}
}
