// Package testdb provides a migrated Postgres instance for tests via
// testcontainers-go. It is a test helper package: importing it outside
// _test.go files pulls the Docker client into a non-test build.
//
// There is one container per test binary — which is to say per package — and
// not one per test. A package that uses it declares
//
//	func TestMain(m *testing.M) { testdb.Main(m) }
//
// so the container is terminated once the package's tests have finished.
package testdb

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/sBurmester/recipe-reader/internal/db"
)

// shared is the package's one container. It starts on the first New rather than
// in Main, so running a package's DB-free tests on their own (`go test -run
// TestWithAuth`) does not boot Postgres for nothing.
var shared struct {
	once      sync.Once
	container *tcpostgres.PostgresContainer
	pool      *pgxpool.Pool
	err       error
}

// New returns a pool on the package's shared, migrated Postgres, emptied of
// every row the previous test left behind and with id sequences restarted —
// the state a freshly migrated container used to provide. Requires Docker.
//
// Isolation now comes from that reset rather than from a new container, which
// is what took the suite from 18 container boots to four. Two consequences:
// tests that use the database must not call t.Parallel, and a test that calls
// New a second time wipes whatever it wrote before the call.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared.once.Do(start)
	if shared.err != nil {
		t.Fatalf("testdb: %v", shared.err)
	}
	if err := reset(context.Background(), shared.pool); err != nil {
		t.Fatalf("testdb: reset: %v", err)
	}
	return shared.pool
}

// NewDatabase creates an empty, unmigrated database named name in the shared
// container and returns a postgres:// DSN for it; it is dropped when the test
// ends. It is for tests of the migrations themselves, which need a schema at a
// version of their choosing — the shared database is always fully migrated.
//
// It calls New, so it resets the shared database like any other test would.
func NewDatabase(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	admin := New(t)
	ident := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		t.Fatalf("testdb: create database %s: %v", name, err)
	}
	// FORCE, because a migrator or pool the test forgot to close would
	// otherwise keep the database from being dropped.
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP DATABASE "+ident+" WITH (FORCE)") })

	cfg := admin.Config().ConnConfig
	dsn := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))),
		Path:     "/" + name,
		RawQuery: "sslmode=disable",
	}
	return dsn.String()
}

// Main runs the package's tests and then tears down the shared container, if
// any test started one. It does not return.
func Main(m *testing.M) {
	code := m.Run()
	if shared.pool != nil {
		shared.pool.Close()
	}
	if shared.container != nil {
		_ = shared.container.Terminate(context.Background())
	}
	os.Exit(code)
}

// start boots, migrates and connects the shared container. A failure is kept
// in shared.err, so every test that asks reports it instead of only the first.
func start() {
	ctx := context.Background()

	// BasicWaitStrategies is the module's own two-step readiness gate: wait for
	// "ready to accept connections" logged twice (the official image starts
	// Postgres, runs init scripts, then restarts) and then for the mapped port
	// to be served on localhost. v0.44's tcpostgres.Run sets no wait strategy
	// of its own, so this must be passed explicitly or migrate races startup.
	container, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("recipes_test"),
		tcpostgres.WithUsername("recipes"),
		tcpostgres.WithPassword("recipes"),
		tcpostgres.BasicWaitStrategies(),
	)
	// Run can return a container alongside an error; keep it so Main still
	// terminates it.
	shared.container = container
	if err != nil {
		shared.err = fmt.Errorf("start postgres container: %w", err)
		return
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		shared.err = fmt.Errorf("connection string: %w", err)
		return
	}
	if err := db.Migrate(dsn); err != nil {
		shared.err = fmt.Errorf("migrate: %w", err)
		return
	}
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		shared.err = fmt.Errorf("connect: %w", err)
		return
	}
	shared.pool = pool
}

// reset truncates every application table and restarts its sequences. The
// migrations insert no rows of their own — db.Seed is a separate, explicit
// step — so an empty table is exactly what a fresh container held.
//
// The table list comes from the catalog rather than being written out, so a
// table a later migration adds is covered without anyone remembering to add it
// here. schema_migrations is left alone: it records the schema version, not
// test data.
func reset(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx,
		`SELECT quote_ident(tablename) FROM pg_tables
		 WHERE schemaname = 'public' AND tablename <> 'schema_migrations'`)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	if len(tables) == 0 {
		return nil
	}
	// The identifiers are quote_ident'd by Postgres itself, not user input.
	_, err = pool.Exec(ctx, "TRUNCATE "+strings.Join(tables, ", ")+" RESTART IDENTITY CASCADE")
	return err
}
