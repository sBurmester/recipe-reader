package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// importLockKey identifies the import lock among all advisory locks in this
// database. The value is arbitrary and only has to stay stable: change it and
// two versions of the binary would no longer see each other's locks.
const importLockKey int64 = 8_233_071_001

// releaseTimeout bounds giving the lock back. Releasing runs on a context that
// is usually already cancelled and therefore carries no deadline of its own,
// and a Postgres that accepts the connection but never answers would otherwise
// hold the caller for as long as it likes.
const releaseTimeout = 5 * time.Second

// ImportLock is pipeline.ImportLock backed by a Postgres advisory lock, which
// is what lets a running server and a one-off import command see each other.
//
// It opens a connection of its own rather than borrowing one from the pool. The
// lock is session-scoped and has to be held for the whole run, and a pooled
// connection held that long is one that server.Run's deferred pool.Close waits
// for — including for an import the shutdown budget has already given up on.
// Its own connection leaves the pool free to close on time.
type ImportLock struct {
	// DSN is the same postgres:// URL the pool was opened with.
	DSN string
}

// TryAcquire implements pipeline.ImportLock.
func (l ImportLock) TryAcquire(ctx context.Context) (func(), bool, error) {
	conn, err := pgx.Connect(ctx, l.DSN)
	if err != nil {
		return nil, false, fmt.Errorf("db: connect for the import lock: %w", err)
	}

	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", importLockKey).Scan(&got); err != nil {
		l.close(ctx, conn)
		return nil, false, fmt.Errorf("db: take the import lock: %w", err)
	}
	if !got {
		l.close(ctx, conn)
		return nil, false, nil
	}

	return func() { l.close(ctx, conn) }, true, nil
}

// close ends the session, which is what releases the lock: Postgres drops every
// advisory lock a session holds when it ends. There is nothing to unlock first,
// and nothing is left behind when the process dies instead of closing cleanly.
func (l ImportLock) close(ctx context.Context, conn *pgx.Conn) {
	// WithoutCancel because closing is what has to happen when the run was
	// cancelled, which is exactly when ctx is already done.
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	if err := conn.Close(closeCtx); err != nil {
		slog.Warn("db: closing the import lock's connection failed; Postgres frees the lock when the session ends anyway", "error", err)
	}
}
