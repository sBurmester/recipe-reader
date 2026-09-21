package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

func TestRun_MigrateMigratesAndSeedsAnEmptyDatabase(t *testing.T) {
	clearEnv(t)
	ctx := context.Background()
	dsn := testdb.NewDatabase(t, "cli_migrate")
	args := []string{"migrate", "--db-dsn", dsn}

	if err := run(args); err != nil {
		t.Fatalf("run(migrate) error = %v", err)
	}
	// Idempotent: migrate after migrate is the normal case for an operator who
	// runs it ahead of every rollout.
	if err := run(args); err != nil {
		t.Fatalf("second run(migrate) error = %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()
	var units int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM units").Scan(&units); err != nil {
		t.Fatalf("count units: %v", err)
	}
	if units == 0 {
		t.Error("migrate left the units table empty")
	}
}

// migrate reads DB_DSN and nothing else, so serve's rules do not apply to it.
func TestParse_MigrateIgnoresServeValidation(t *testing.T) {
	clearEnv(t)
	t.Setenv("API_TOKEN", "")
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("IMPORT_MAX_ITEMS", "0")
	t.Setenv("EXTRACTION_MODE", "banana")
	t.Setenv("IMPORT_INTERVAL", "banana")

	_, kctx, err := parse(t, "migrate")
	if err != nil {
		t.Fatalf("parse(migrate) error = %v, want serve's rules not applied", err)
	}
	if got := kctx.Command(); got != "migrate" {
		t.Errorf("Command() = %q, want migrate", got)
	}
}
