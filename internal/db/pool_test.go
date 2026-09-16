package db

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const baseDSN = "postgres://recipes:recipes@localhost:5432/recipes?sslmode=disable"

func parse(t *testing.T, dsn string) *pgxpool.Config {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig(%q) error = %v", dsn, err)
	}
	applyPoolDefaults(cfg, dsn)
	return cfg
}

// The pool used to be built by pgxpool.New, which accepts every default:
// MinConns 0, so the pool drains to nothing between the six-hourly imports and
// the first request afterwards pays a full connect; MaxConns max(4, NumCPU), so
// on a two-CPU container four connections serve a list endpoint that costs
// 2+2N queries per page.
func TestApplyPoolDefaults_FillsInWhatTheDSNOmits(t *testing.T) {
	cfg := parse(t, baseDSN)

	if cfg.MaxConns != defaultMaxConns {
		t.Errorf("MaxConns = %d, want %d", cfg.MaxConns, defaultMaxConns)
	}
	if cfg.MinConns != defaultMinConns {
		t.Errorf("MinConns = %d, want %d", cfg.MinConns, defaultMinConns)
	}
	if cfg.ConnConfig.ConnectTimeout != defaultConnectTimeout {
		t.Errorf("ConnectTimeout = %v, want %v", cfg.ConnConfig.ConnectTimeout, defaultConnectTimeout)
	}
}

// The point of reading the DSN a second time: an operator who sets a pool_*
// parameter has to win, or the setting is decoration and the only real lever is
// still a rebuild.
func TestApplyPoolDefaults_DSNWins(t *testing.T) {
	cfg := parse(t, baseDSN+"&pool_max_conns=25&pool_min_conns=5&connect_timeout=11")

	if cfg.MaxConns != 25 {
		t.Errorf("MaxConns = %d, want the DSN's 25", cfg.MaxConns)
	}
	if cfg.MinConns != 5 {
		t.Errorf("MinConns = %d, want the DSN's 5", cfg.MinConns)
	}
	if cfg.ConnConfig.ConnectTimeout != 11*time.Second {
		t.Errorf("ConnectTimeout = %v, want the DSN's 11s", cfg.ConnConfig.ConnectTimeout)
	}
}

// Lowering the ceiling below our floor must not produce a config pgxpool
// refuses to build — that would turn an operator capping connections into a
// server that will not start.
func TestApplyPoolDefaults_MinNeverExceedsMax(t *testing.T) {
	dsn := baseDSN + "&pool_max_conns=1"
	cfg := parse(t, dsn)

	if cfg.MinConns > cfg.MaxConns {
		t.Fatalf("MinConns = %d exceeds MaxConns = %d", cfg.MinConns, cfg.MaxConns)
	}
	// Built, not connected: NewWithConfig validates the config and returns, so
	// this asserts pgxpool accepts it without needing a Postgres to talk to.
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v, want a buildable config", err)
	}
	pool.Close()
}

// A keyword/value DSN carries no URL query, so nothing is detected as set and
// every default applies. Recorded rather than fixed: the project builds and
// documents URL DSNs only, and the cost of being wrong here is a default where
// an explicit value was meant — never a failure to start.
func TestDSNParams_KeywordValueDSNYieldsNoParams(t *testing.T) {
	if got := dsnParams("host=localhost port=5432 pool_max_conns=25"); len(got) != 0 {
		t.Errorf("dsnParams() = %v, want empty for a keyword/value DSN", got)
	}
}
