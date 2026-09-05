// Package db owns the Postgres connection pool, the embedded schema
// migrations, and the default-data seeding. Query execution itself lives in
// the sqlc-generated internal/db/sqlc package.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	// Registers the "pgx5://" database URL scheme with golang-migrate. The
	// driver's init() calls database.Register("pgx5", ...); without this
	// blank import migrate.NewWithSourceInstance cannot resolve the scheme.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Connect opens a pgx connection pool against dsn and verifies it with a
// ping. dsn is a postgres:// URL.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// Migrate applies all pending embedded migrations against dsn (a
// postgres:// URL — the same one passed to Connect).
func Migrate(dsn string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("db: migration source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL(dsn))
	if err != nil {
		return fmt.Errorf("db: migration init: %w", err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	return nil
}

// migrateURL rewrites a postgres:// / postgresql:// DSN to the pgx5:// scheme
// that the golang-migrate pgx/v5 database driver registers itself under. The
// driver rewrites the scheme back to postgres:// before it actually connects.
func migrateURL(dsn string) string {
	if rest, ok := strings.CutPrefix(dsn, "postgres://"); ok {
		return "pgx5://" + rest
	}
	if rest, ok := strings.CutPrefix(dsn, "postgresql://"); ok {
		return "pgx5://" + rest
	}
	return dsn
}
