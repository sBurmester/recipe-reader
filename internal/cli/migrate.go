package cli

import (
	"context"
	"log/slog"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/db"
)

// MigrateCmd applies pending migrations and seeds the lookup tables, then
// exits. serve does the same at every start; this does it on its own — ahead
// of a rollout, or after a restore — without starting the server or the import
// worker, and without reading any setting but the database's.
type MigrateCmd struct {
	config.Database `embed:"" group:"Database"`
}

// Run migrates and seeds once.
func (c *MigrateCmd) Run(ctx context.Context) error {
	pool, err := db.Open(ctx, c.DSN)
	if err != nil {
		return err
	}
	pool.Close()
	slog.Info("database migrated and seeded")
	return nil
}
