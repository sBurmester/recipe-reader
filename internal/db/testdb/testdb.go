// Package testdb provides an ephemeral, migrated Postgres instance for tests
// via testcontainers-go. It is a test helper package: importing it outside
// _test.go files pulls the Docker client into a non-test build.
package testdb

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sBurmester/recipe-reader/internal/db"
)

// New starts an ephemeral Postgres container, migrates it, and returns a
// connected pool. The container and pool are torn down via t.Cleanup.
// Requires Docker to be running — every test that calls this pays
// container-startup cost (roughly 1-2s), which is the accepted tradeoff for
// testing against real Postgres semantics instead of a stand-in.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("recipes_test"),
		tcpostgres.WithUsername("recipes"),
		tcpostgres.WithPassword("recipes"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp")),
	)
	if err != nil {
		t.Fatalf("testdb: start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("testdb: connection string: %v", err)
	}

	if err := db.Migrate(dsn); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}

	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("testdb: connect: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}
