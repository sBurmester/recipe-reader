package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3/lock"

	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// The advisory lock is goose's, but asking for it is this package's doing, and
// nothing else in the suite would notice if the option were dropped: a lock
// that is never contended looks exactly like no lock at all. So the test
// contends it — and then checks that a caller who gives up waiting is told
// what that means, because that sentence is the other thing this file owns.
func TestMigrateWithContext_WaitsForTheLockAndReportsGivingUp(t *testing.T) {
	dsn := testdb.NewDatabase(t, "migrate_lock_contention")

	holder, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatalf("connect the lock holder: %v", err)
	}
	defer func() { _ = holder.Close(context.WithoutCancel(t.Context())) }()
	if _, err := holder.Exec(t.Context(), "SELECT pg_advisory_lock($1)", lock.DefaultLockID); err != nil {
		t.Fatalf("take the migration lock: %v", err)
	}

	const budget = 2 * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()
	start := time.Now()
	err = db.MigrateWithContext(ctx, dsn)

	if err == nil {
		t.Fatal("MigrateWithContext() = nil, want it to report the lock it never got")
	}
	if waited := time.Since(start); waited < budget/2 {
		t.Errorf("MigrateWithContext() gave up after %v, want it to wait for the lock", waited)
	}
	if !strings.Contains(err.Error(), "re-running") {
		t.Errorf("MigrateWithContext() error = %q, want it to say that re-running is safe", err)
	}
	if tableExists(t, dsn, "units") {
		t.Error("MigrateWithContext() applied a migration although it never held the lock")
	}
}

// tableExists reports whether a table of that name is in the public schema.
func tableExists(t *testing.T, dsn, name string) bool {
	t.Helper()
	pool := connect(t, dsn)
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", "public."+name).Scan(&exists); err != nil {
		t.Fatalf("look for %s: %v", name, err)
	}
	return exists
}
