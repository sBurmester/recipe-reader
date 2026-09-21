package main

import (
	"strings"
	"testing"
)

// Without an account there is nothing to import, and a one-off run has no next
// tick to recover at — so it says what is missing and fails, where serve would
// start anyway with the import worker withheld.
func TestRun_ImportWithoutAnAccountFails(t *testing.T) {
	clearEnv(t)

	err := run([]string{"import", "--db-dsn", unreachableDSN})

	if err == nil {
		t.Fatal("run(import) = nil, want an error naming the missing account")
	}
	if !strings.Contains(err.Error(), "INSTAGRAM_USERNAME") {
		t.Errorf("run(import) error = %v, want it to name INSTAGRAM_USERNAME", err)
	}
}

// import reads the database, the account, extraction, the LLM and the run
// limits — not the listen address, and not the schedule it has no part in.
func TestParse_ImportTakesNeitherTheAddressNorTheInterval(t *testing.T) {
	clearEnv(t)

	for _, flag := range []string{"--http-addr", "--import-interval"} {
		t.Run(flag, func(t *testing.T) {
			_, _, err := parse(t, "import", flag, "1s")
			if err == nil {
				t.Fatalf("parse(import %s) = nil error, want it rejected as unknown", flag)
			}
			if !strings.Contains(err.Error(), "unknown flag") {
				t.Errorf("parse(import %s) error = %v, want an unknown-flag error", flag, err)
			}
		})
	}
}
