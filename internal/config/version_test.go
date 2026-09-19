package config

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

// --version is half of D8's answer to "what is actually deployed" — the half
// for whoever has a shell on the host; GET /api/healthz is the half for whoever
// has only the port. It prints the string Load was given, which the Makefile
// and the Dockerfile stamp in at link time, and exits 0 before anything else is
// parsed or validated.
func TestLoad_VersionFlagPrintsTheStampedVersionAndExits(t *testing.T) {
	var out bytes.Buffer
	exited := -1

	// kong calls Exit after printing. Left at its default that is os.Exit,
	// which would end the test binary, so the test supplies its own and lets
	// load return normally afterwards.
	_, err := load([]string{"--version"}, "v1.2.3-4-gabc1234",
		kong.Writers(&out, &out),
		kong.Exit(func(code int) { exited = code }),
	)
	if err != nil {
		t.Fatalf("load(--version) error = %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "v1.2.3-4-gabc1234" {
		t.Errorf("printed %q, want the stamped version", got)
	}
	if exited != 0 {
		t.Errorf("exit code = %d, want 0", exited)
	}
}

// The flag must not be reachable through the environment: every other setting
// is, and a VERSION variable in a .env file that quietly made the process print
// its version and exit would be a very confusing way to fail to start.
func TestLoad_VersionIsFlagOnly(t *testing.T) {
	t.Setenv("VERSION", "1")
	if _, err := loadArgs(nil); err != nil {
		t.Fatalf("load() with VERSION set in the environment error = %v", err)
	}
}
