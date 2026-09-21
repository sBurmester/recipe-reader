package main

import (
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
)

// The migrate tests need Postgres. testdb starts its container on first use,
// so the rest of this package's tests still run without one — including the
// children TestMain_ExitStatus re-executes, which never touch it.
func TestMain(m *testing.M) { testdb.Main(m) }
