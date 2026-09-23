// Package db owns the Postgres connection pool, the embedded schema
// migrations, and the default-data seeding. Query execution itself lives in
// the sqlc-generated internal/db/sqlc package.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"
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

// MigrateWithContext is Migrate, bounded by ctx. Cancelling it rolls back the
// migration in flight and leaves every migration applied before it applied, so
// a second run continues where this one stopped.
//
// That promise is goose's rather than this file's: it runs each SQL migration
// in its own transaction and holds a Postgres advisory lock for the length of
// the run, so two instances starting at once queue up instead of racing. The
// version before this one built the same guarantee by hand out of golang-
// migrate's GracefulStop channel, a context.AfterFunc and two ctx.Err()
// checks, and still could not tell a cancelled run from a completed one when
// the cancellation arrived during the last migration.
func MigrateWithContext(ctx context.Context, dsn string) error {
	return withMigrator(ctx, dsn, func(provider *goose.Provider) error {
		_, err := provider.Up(ctx)
		return err
	})
}

// MigrateDown reverts every applied migration against dsn, newest first,
// leaving no application table behind. It destroys every row in them.
// goose_db_version stays, holding only its zero row: that is how goose records
// a database at no version, as opposed to one it has never seen.
//
// Nothing in the binary calls it. It exists so the down migrations run through
// the same embedded source and the same DSN handling as Migrate, which is what
// a rollback of a deployed binary would have to use — the image carries no
// migration files for a goose command line to read. A down migration that has
// never been run is not a rollback plan: until TestMigrations_UpDownUp ran
// this, the only evidence that 0001 drops its tables in a workable order was
// reading it.
func MigrateDown(dsn string) error {
	ctx := context.Background()
	return withMigrator(ctx, dsn, func(provider *goose.Provider) error {
		_, err := provider.DownTo(ctx, 0)
		return err
	})
}

// withMigrator opens a goose provider over the embedded migrations for dsn and
// hands it to run.
//
// The guard on the way in is the one piece of the hand-written cancellation
// path worth keeping: without it an already cancelled context would still cost
// a connection and a lock attempt before failing.
func withMigrator(ctx context.Context, dsn string, run func(*goose.Provider) error) (retErr error) {
	if err := ctx.Err(); err != nil {
		return cancelledMigration(err)
	}
	sqlDB, err := openSQL(dsn)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, sqlDB.Close()) }()

	src, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("db: migration source: %w", err)
	}
	// The engine before goose took an advisory lock of its own accord, inside
	// its pgx driver. goose does it only when asked, and dropping the option
	// would quietly lose the protection: two instances starting at once would
	// apply the same migration side by side.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("db: migration lock: %w", err)
	}
	provider, err := goose.NewProvider(database.DialectPostgres, sqlDB, src,
		goose.WithSessionLocker(locker),
		// Migrating used to be silent, so a slow start said nothing about which
		// migration it was in. goose is silent too unless asked, and through
		// slog it obeys LOG_LEVEL and LOG_FORMAT like every other line.
		goose.WithSlog(slog.Default()),
		goose.WithVerbose(true),
	)
	if err != nil {
		return fmt.Errorf("db: migration init: %w", err)
	}

	if err := run(provider); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return cancelledMigration(err)
		}
		return fmt.Errorf("db: migrate: %w", err)
	}
	return nil
}

// openSQL opens a database/sql handle on dsn through pgx's stdlib adapter.
//
// goose speaks database/sql and the rest of this project speaks pgx, so
// exactly one place translates between them, and this is it. The handle is
// capped at a single connection because a migration run is a single connection
// by construction — goose takes one *sql.Conn, holds its advisory lock on it
// and applies every migration through it — and the cap says so rather than
// leaving a pool idling behind the migrator. The caller closes it.
func openSQL(dsn string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse dsn: %w", err)
	}
	sqlDB := stdlib.OpenDB(*cfg)
	sqlDB.SetMaxOpenConns(1)
	return sqlDB, nil
}

// cancelledMigration says what an operator reading the line needs to decide
// what to do next. "context canceled" on its own reads like damage; what in
// fact happened is that the migration in flight was rolled back and every
// earlier one stands, so running the command again finishes the job.
func cancelledMigration(err error) error {
	return fmt.Errorf("db: migrate: cancelled, the migration in flight was rolled back and "+
		"re-running continues where it stopped: %w", err)
}
