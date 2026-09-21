package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stopWait bounds the fake's Up. A migrateUp that never stops its migrator
// would otherwise block here for as long as the suite is willing to wait; this
// turns that into a failed assertion with a name on it.
const stopWait = 5 * time.Second

// errNeverStopped is what the fake reports when the cancellation never reached
// it, which is the failure the AfterFunc in migrateUp exists to prevent.
var errNeverStopped = errors.New("Stop was never called")

// stoppingMigrator is golang-migrate's graceful stop with the migration taken
// out: Up blocks until Stop is called and then returns nil, exactly as a
// stopped *migrate.Migrate does. The embedded migrations run far too fast to
// interrupt on purpose, so the window a real Ctrl-C would land in is made here
// instead — Up cancels the context itself, which is why the cancellation always
// arrives while Up is in flight and never before or after it.
type stoppingMigrator struct {
	cancel  context.CancelFunc
	stopped chan struct{}
}

func (m *stoppingMigrator) Up() error {
	m.cancel()
	select {
	case <-m.stopped:
		return nil
	case <-time.After(stopWait):
		return errNeverStopped
	}
}

// Stop closes rather than signals: context.AfterFunc runs its function at most
// once, so a second close would be a bug worth the panic.
func (m *stoppingMigrator) Stop() { close(m.stopped) }

func (m *stoppingMigrator) Close() (source error, database error) { return nil, nil }

// The promise MigrateContext adds to golang-migrate is that a cancelled run is
// reported as one. A gracefully stopped Up returns nil, so a run that was cut
// short after applying part of the schema would otherwise be indistinguishable
// from a clean one — and the caller would carry on against a half-migrated
// database. Deleting either the AfterFunc or the ctx.Err() check after Up
// fails this test.
func TestMigrateUp_GracefulStopIsReportedAsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m := &stoppingMigrator{cancel: cancel, stopped: make(chan struct{})}

	err := migrateUp(ctx, func() (migrator, error) { return m, nil })
	if err == nil {
		t.Fatal("migrateUp() = nil, want the cancellation reported although Up returned nil")
	}
	if errors.Is(err, errNeverStopped) {
		t.Fatalf("migrateUp() error = %v: the cancellation never reached the migrator", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("migrateUp() error = %v, want it to wrap context.Canceled", err)
	}
}
