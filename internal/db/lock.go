package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// importLockKey identifies the import lock among all advisory locks in this
// database. The value is arbitrary and only has to stay stable: change it and
// two versions of the binary would no longer see each other's locks.
const importLockKey int64 = 8_233_071_001

// releaseTimeout bounds the two statements that run after a run is already
// over: the unlock, and the connection close that stands in for it when the
// unlock fails. Both detach from the run's context, because releasing is what
// has to happen when the run was cancelled — and detaching drops the deadline
// along with the cancellation, so without one of their own a Postgres that
// accepts the connection but never answers would block `defer release()`
// forever, with no context left for a Ctrl-C to cancel.
const releaseTimeout = 5 * time.Second

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
		// Whether Postgres took the lock before this failed is not knowable
		// from here: a cancellation can land between the server executing the
		// statement and the row reaching us. Releasing the connection would
		// then lend the next borrower a lock nobody will ever release, so it is
		// discarded the same way a failed unlock discards it. The cost of being
		// wrong is one reconnect on a path that is already failing the run.
		discard(ctx, conn)
		return nil, false, fmt.Errorf("db: take the import lock: %w", err)
	}
	if !got {
		conn.Release()
		return nil, false, nil
	}

	return func() {
		// WithoutCancel because releasing is what has to happen when the run was
		// cancelled, which is exactly when ctx is already done; with a deadline
		// of its own because WithoutCancel strips the run's — see releaseTimeout.
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", importLockKey); err != nil {
			slog.Warn("db: could not release the import lock; discarding its connection", "error", err)
			// The lock belongs to this session. Handing the connection back to
			// the pool would lend the next borrower a lock nobody can release;
			// closing it ends the session, and Postgres frees the lock with it.
			discard(ctx, conn)
			return
		}
		conn.Release()
	}, true, nil
}

// discard takes conn out of the pool and closes it, which ends its Postgres
// session and with it every advisory lock that session still holds. It answers
// both ways a connection can end up back in the pool holding a lock nobody can
// release: an unlock that failed, and an acquire whose answer never arrived.
//
// It derives its own bounded context from ctx rather than taking one, because
// every caller is on a path where ctx may already be cancelled and a close
// still has to be attempted.
func discard(ctx context.Context, conn *pgxpool.Conn) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	if err := conn.Hijack().Close(closeCtx); err != nil {
		slog.Warn("db: closing the import lock's connection failed", "error", err)
	}
}
