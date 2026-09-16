package db_test

import (
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// TestMain shares one Postgres container across this package's tests.
func TestMain(m *testing.M) { testdb.Main(m) }
