package db_test

import (
	"context"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// The point of the lock is what happens to the second holder, and that is a
// property of Postgres sessions — so this runs against a real database with two
// pools, the way two processes would meet.
func TestImportLock_SecondHolderIsRefusedUntilTheFirstReleases(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewDatabase(t, "import_lock")

	first, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect() first error = %v", err)
	}
	defer first.Close()
	second, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect() second error = %v", err)
	}
	defer second.Close()

	release, ok, err := db.ImportLock{Pool: first}.TryAcquire(ctx)
	if err != nil {
		t.Fatalf("TryAcquire() first error = %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() first ok = false, want the lock to be free")
	}

	if _, ok, err := (db.ImportLock{Pool: second}).TryAcquire(ctx); err != nil {
		t.Fatalf("TryAcquire() second error = %v", err)
	} else if ok {
		t.Fatal("TryAcquire() second ok = true, want it refused while the first holds the lock")
	}

	release()

	release2, ok, err := db.ImportLock{Pool: second}.TryAcquire(ctx)
	if err != nil {
		t.Fatalf("TryAcquire() after release error = %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() after release ok = false, want the lock free again")
	}
	release2()
}
