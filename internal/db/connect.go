// Package db owns the Postgres connection pool, the embedded schema
// migrations, and the default-data seeding. Query execution itself lives in
// the sqlc-generated internal/db/sqlc package.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

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

// Pool defaults applied when the DSN does not name the corresponding
// pgxpool parameter. They replace pgxpool's own, which are tuned for a service
// with far more traffic than this one and were being accepted by default.
const (
	// defaultMaxConns is raised above pgxpool's max(4, NumCPU), which is four
	// on a two-CPU container — few enough that an import and a couple of
	// browser tabs contend for them. (This was first argued from search costing
	// 2+2N queries a page; persistence P4 has since cut that to four.)
	defaultMaxConns = 10
	// defaultMinConns is above pgxpool's 0 so the pool does not drain to
	// nothing between the six-hourly imports, leaving the first request after
	// an idle stretch to pay a full connect and TLS handshake.
	defaultMinConns = 2
	// defaultConnectTimeout bounds establishing one connection. Without it the
	// driver default applies and a Postgres that accepts the TCP connection but
	// never completes the handshake holds the caller for as long as it likes.
	defaultConnectTimeout = 5 * time.Second
)

// Connect opens a pgx connection pool against dsn and verifies it with a
// ping. dsn is a postgres:// URL.
//
// The pool is configured rather than taken as it comes. pgxpool.New accepts
// every default, and the defaults an operator most wants to move — pool size,
// above all — are then reachable only by editing this file. Going through
// ParseConfig keeps the DSN's own pool_* parameters authoritative where they
// are given (pool_max_conns, pool_min_conns, pool_max_conn_lifetime,
// pool_max_conn_idle_time, pool_health_check_period, connect_timeout) and
// supplies the constants above only where the DSN is silent, so the operator
// has a lever that needs no rebuild.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse dsn: %w", err)
	}
	applyPoolDefaults(cfg, dsn)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// Open migrates the database at dsn, connects to it and seeds the lookup
// tables — everything a process needs before it can use the database. serve
// and migrate both start with it, so the order is written down once. The
// caller closes the pool.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if err := MigrateWithContext(ctx, dsn); err != nil {
		return nil, err
	}
	pool, err := Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := Seed(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// applyPoolDefaults fills in the pool settings dsn did not name. ParseConfig
// has already substituted pgxpool's defaults for those, and the two are
// indistinguishable afterwards — hence the second look at the DSN rather than a
// comparison against pgxpool's values.
func applyPoolDefaults(cfg *pgxpool.Config, dsn string) {
	params := dsnParams(dsn)
	if _, ok := params["pool_max_conns"]; !ok {
		cfg.MaxConns = defaultMaxConns
	}
	if _, ok := params["pool_min_conns"]; !ok {
		cfg.MinConns = defaultMinConns
	}
	if _, ok := params["connect_timeout"]; !ok {
		cfg.ConnConfig.ConnectTimeout = defaultConnectTimeout
	}
	// A DSN that sets pool_max_conns below our MinConns would otherwise produce
	// a config pgxpool rejects at construction, turning an operator lowering the
	// ceiling into a server that will not start.
	if cfg.MinConns > cfg.MaxConns {
		cfg.MinConns = cfg.MaxConns
	}
}

// dsnParams reports which settings a URL-form DSN names, as a set. A
// keyword/value DSN ("host=... port=...") parses as a URL with no query, so it
// yields the empty set and every default above applies — acceptable because
// every DSN this project builds or documents is a URL, and the cost of being
// wrong is a default where an explicit value was meant, not a failure.
func dsnParams(dsn string) map[string]struct{} {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil
	}
	params := make(map[string]struct{}, len(u.Query()))
	for key := range u.Query() {
		params[key] = struct{}{}
	}
	return params
}

// Migrate applies all pending embedded migrations against dsn (a
// postgres:// URL — the same one passed to Connect).
func Migrate(dsn string) error {
	return MigrateWithContext(context.Background(), dsn)
}

// MigrateWithContext is Migrate, bounded by ctx. Cancelling it stops the migrator
// between two migrations; the one in flight is always finished, because a
// half-applied migration is worse than a slow Ctrl-C.
//
// golang-migrate has no context parameter, only the GracefulStop channel, so
// the cancellation path is this file's own — see migrateUp, which holds it.
func MigrateWithContext(ctx context.Context, dsn string) error {
	return migrateUp(ctx, func() (migrator, error) {
		m, err := newMigrate(dsn)
		if err != nil {
			return nil, err
		}
		return gracefulMigrator{m}, nil
	})
}

// migrator is the part of *migrate.Migrate that the cancellation path drives.
// It exists so that path — the only thing this file adds to golang-migrate —
// can be tested without a migration slow enough to interrupt.
type migrator interface {
	Up() error
	// Stop asks the migrator to stop before it starts the next migration.
	Stop()
	Close() (source error, database error)
}

// gracefulMigrator adapts *migrate.Migrate to migrator. GracefulStop is
// buffered with room for one (migrate.go:185) and read between migrations, so
// the send never blocks and at most one ever happens.
//
// This one statement is deliberately the uncovered line of the cancellation
// path. Reaching it from a test needs a migration slow enough to interrupt,
// and golang-migrate reads and writes its isGracefulStop flag from two
// goroutines without synchronization (migrate.go:71, :549, :726, :816), so a
// test that drove the real migrator to a graceful stop would be liable to
// report a race inside the dependency rather than a bug here. In production
// that race is harmless — the buffered channel is the authoritative signal,
// and a stale read costs at most one further migration before the stop takes.
// migrate_test.go's fake stands in for this instead.
type gracefulMigrator struct{ *migrate.Migrate }

func (g gracefulMigrator) Stop() { g.GracefulStop <- true }

// migrateUp runs the migrator open returns, bounded by ctx. The three guards
// are the whole point: the first refuses to open anything under a context that
// is already done, the AfterFunc turns a later cancellation into a stop between
// two migrations, and the last one reports it — a stopped Up returns nil rather
// than an error, so without it a cancelled, partly applied run would look like
// a successful one.
//
// That last guard is deliberately blunt: a cancellation arriving while the
// final migration runs, or after Up has already returned, reports a run that
// in fact applied everything as a failure. golang-migrate keeps its
// isGracefulStop flag unexported and offers no way to ask whether the stop
// actually took, so the two cases cannot be told apart from here. Re-running
// is the remedy — ErrNoChange makes the retry a success.
//
// open is a parameter rather than a package-level variable so a test can hand
// in a fake migrator without the package growing mutable state to swap.
func migrateUp(ctx context.Context, open func() (migrator, error)) error {
	if err := ctx.Err(); err != nil {
		return migrateCancelled(err)
	}
	m, err := open()
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()

	stop := context.AfterFunc(ctx, m.Stop)
	defer stop()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return migrateCancelled(err)
	}
	return nil
}

// migrateCancelled words a cancellation for the operator who reads it and has to
// decide what to do next. "context canceled" alone reads like damage, and it is
// true at both places migrateUp reports it: before open nothing was applied,
// and after Up only whole migrations were, because a graceful stop always lets
// the one in flight finish.
func migrateCancelled(err error) error {
	return fmt.Errorf("db: migrate up: cancelled, nothing is half applied and re-running continues where it stopped: %w", err)
}

// MigrateDown reverts every applied migration against dsn, newest first,
// leaving no application tables behind. It destroys every row in them.
//
// Nothing in the binary calls it. It exists so the down migrations run through
// the same embedded source and the same URL handling as Migrate, which is what
// a rollback of a deployed binary would have to use — the image carries no
// migration files for the golang-migrate CLI to read. A down migration that has
// never been run is not a rollback plan: until TestMigrations_UpDownUp ran this,
// the only evidence that 0001 drops its tables in a workable order was reading
// it.
func MigrateDown(dsn string) error {
	m, err := newMigrate(dsn)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: migrate down: %w", err)
	}
	return nil
}

// newMigrate builds a migrator over the embedded migrations for dsn.
func newMigrate(dsn string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("db: migration source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL(dsn))
	if err != nil {
		return nil, fmt.Errorf("db: migration init: %w", err)
	}
	return m, nil
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
