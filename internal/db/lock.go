package db

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// importLockKey identifies the import lock among all advisory locks in this
// database. The value is arbitrary and only has to stay stable: change it and
// two versions of the binary would no longer see each other's locks.
const importLockKey int64 = 8_233_071_001

// ImportLock is pipeline.ImportLock backed by a Postgres advisory lock, which
// is what lets a running server and a one-off import command see each other.
//
// The lock is session-scoped, so it is held by one connection out of the pool
// for as long as the run lasts, and Postgres drops it when that session ends —
// including when the process is killed. That is the property a lock file does
// not have.
type ImportLock struct {
	Pool *pgxpool.Pool
}

// TryAcquire implements pipeline.ImportLock.
func (l ImportLock) TryAcquire(ctx context.Context) (func(), bool, error) {
	conn, err := l.Pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("db: acquire a connection for the import lock: %w", err)
	}

	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", importLockKey).Scan(&got); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("db: take the import lock: %w", err)
	}
	if !got {
		conn.Release()
		return nil, false, nil
	}

	return func() {
		// WithoutCancel because releasing is what has to happen when the run was
		// cancelled, which is exactly when ctx is already done.
		unlockCtx := context.WithoutCancel(ctx)
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", importLockKey); err != nil {
			slog.Warn("db: could not release the import lock; discarding its connection", "error", err)
			// The lock belongs to this session. Handing the connection back to
			// the pool would lend the next borrower a lock nobody can release;
			// closing it ends the session, and Postgres frees the lock with it.
			if err := conn.Hijack().Close(unlockCtx); err != nil {
				slog.Warn("db: closing the import lock's connection failed", "error", err)
			}
			return
		}
		conn.Release()
	}, true, nil
}
