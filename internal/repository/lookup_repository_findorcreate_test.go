package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sBurmester/recipe-reader/internal/db/sqlc"
	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// persistence P6: a repeated lookup went through the insert path, spending a
// sequence value each time. With the read first, finding an existing row leaves
// the sequence where it was, so the next new row takes the very next id.
func TestFindOrCreate_ExistingRowSpendsNoSequenceValue(t *testing.T) {
	ctx := context.Background()
	repo := newTestLookupRepo(t)

	first, err := repo.FindOrCreateIngredient(ctx, "Mehl")
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := repo.FindOrCreateIngredient(ctx, "Mehl"); err != nil {
			t.Fatal(err)
		}
	}
	next, err := repo.FindOrCreateIngredient(ctx, "Zucker")
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != first.ID+1 {
		t.Errorf("next new id = %d after five repeat lookups of id %d, want %d — the repeats spent sequence values",
			next.ID, first.ID, first.ID+1)
	}
}

// Recipe writes resolve names inside their own transaction, so a lock taken on
// an existing lookup row was held until the recipe committed: two writes naming
// the same ingredient queued, and two naming a pair in opposite order could
// deadlock. Finding an existing row must not block behind a transaction that
// is still open and has looked up the same row.
func TestFindOrCreate_ExistingRowIsNotLockedByAnOpenTransaction(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	repo := NewLookupRepository(pool)
	salz, err := repo.FindOrCreateIngredient(ctx, "Salz")
	if err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // the test never commits it
	if _, err := sqlc.New(pool).WithTx(tx).FindOrCreateIngredient(ctx, "Salz"); err != nil {
		t.Fatal(err)
	}

	// The open transaction above would hold the row lock DO UPDATE takes; a
	// second lookup would then wait for it until this deadline.
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	got, err := repo.FindOrCreateIngredient(waitCtx, "Salz")
	if err != nil {
		t.Fatalf("lookup of an existing row blocked behind an open transaction: %v", err)
	}
	if got.ID != salz.ID {
		t.Errorf("id = %d, want %d", got.ID, salz.ID)
	}
}

// The one case the read cannot see: another transaction inserts the name after
// this statement's snapshot. The insert then conflicts, and DO UPDATE still
// returns the row — where DO NOTHING would have returned none, and the lookup
// would have failed with ErrNoRows.
func TestFindOrCreate_ConcurrentInsertOfTheSameNameReturnsOneRow(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	repo := NewLookupRepository(pool)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // committed below; a no-op then
	inserted, err := sqlc.New(pool).WithTx(tx).FindOrCreateIngredient(ctx, "Pfeffer")
	if err != nil {
		t.Fatal(err)
	}

	// This lookup sees no committed row, attempts the insert, and waits on the
	// uncommitted one above.
	type result struct {
		id  int64
		err error
	}
	done := make(chan result, 1)
	go func() {
		ing, err := repo.FindOrCreateIngredient(ctx, "Pfeffer")
		if err != nil {
			done <- result{err: err}
			return
		}
		done <- result{id: ing.ID}
	}()
	waitUntilBlockedOnALock(t, pool)

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("concurrent FindOrCreateIngredient() error = %v, want the committed row", r.err)
		}
		if r.id != inserted.ID {
			t.Errorf("id = %d, want %d — the row the other transaction committed", r.id, inserted.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the concurrent lookup never returned")
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM ingredients WHERE name = 'Pfeffer'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("%d rows named Pfeffer, want 1", count)
	}
}

// waitUntilBlockedOnALock returns once some backend is waiting on a lock, so the
// test commits only after the concurrent statement has reached the conflict.
func waitUntilBlockedOnALock(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := pool.QueryRow(context.Background(),
			"SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock'").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the concurrent lookup never waited on the uncommitted insert")
}
