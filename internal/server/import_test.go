package server

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/db"
	"github.com/sBurmester/recipe-reader/internal/db/testdb"
	"github.com/sBurmester/recipe-reader/internal/pipeline"
)

// importOnceConfig is a config for a run that reaches the lock and then fails
// at the login: an account with no password, which instago refuses before it
// touches the network, so nothing here talks to Instagram.
func importOnceConfig(t *testing.T, name string) config.Config {
	t.Helper()
	cfg := baseConfig("rule")
	cfg.Database.DSN = testdb.NewDatabase(t, name)
	cfg.Instagram.Username = "someone"
	cfg.Instagram.SessionPath = filepath.Join(t.TempDir(), "session.json")
	return cfg
}

// The lock used to be taken inside Pipeline.Run, which ImportOnce reaches only
// after newFetcher has logged in and written the session file — the very login
// D1 says the lock exists to prevent. The proof that it is taken first is the
// identity of the error: this account cannot log in, so a run that logged in
// before consulting the lock would fail with the login's error. Getting
// ErrImportInProgress instead means the refusal came first.
func TestImportOnce_RefusedLockIsReportedBeforeTheLogin(t *testing.T) {
	ctx := context.Background()
	cfg := importOnceConfig(t, "import_once_refused")

	// A second pool, the way a running server's worker would hold it.
	holder, err := db.Connect(ctx, cfg.Database.DSN)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer holder.Close()
	release, ok, err := db.ImportLock{Pool: holder}.TryAcquire(ctx)
	if err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() ok = false, want the lock to be free")
	}
	defer release()

	if _, err := ImportOnce(ctx, cfg); !errors.Is(err, pipeline.ErrImportInProgress) {
		t.Fatalf("ImportOnce() error = %v, want ErrImportInProgress; a login error means the "+
			"lock was consulted after newFetcher", err)
	}
}

// Every return from ImportOnce has to give the lock back, or one failed command
// would block the server's worker until that process's pool is closed. The
// login failure is the shortest path out after the lock is held.
func TestImportOnce_ReleasesTheLockWhenTheRunFails(t *testing.T) {
	ctx := context.Background()
	cfg := importOnceConfig(t, "import_once_release")

	if _, err := ImportOnce(ctx, cfg); err == nil {
		t.Fatal("ImportOnce() = nil, want the error from an account that cannot log in")
	}

	after, err := db.Connect(ctx, cfg.Database.DSN)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer after.Close()
	release, ok, err := db.ImportLock{Pool: after}.TryAcquire(ctx)
	if err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() ok = false; ImportOnce returned without releasing the import lock")
	}
	release()
}
