package config

import (
	"strings"
	"testing"
)

// integration I6: the per-run bounds existed only as package defaults. They
// are reachable from configuration now, with the same defaults.
func TestLoad_ImportBounds(t *testing.T) {
	clearEnv(t, "IMPORT_MAX_ITEMS", "IMPORT_MAX_PAGES")

	cfg, err := loadArgs(nil)
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	if cfg.Import.MaxItems != 50 || cfg.Import.MaxPages != 100 {
		t.Errorf("defaults = %d items / %d pages, want 50 / 100", cfg.Import.MaxItems, cfg.Import.MaxPages)
	}

	t.Setenv("IMPORT_MAX_ITEMS", "500")
	cfg, err = loadArgs([]string{"--import-max-pages", "400"})
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	if cfg.Import.MaxItems != 500 || cfg.Import.MaxPages != 400 {
		t.Errorf("configured = %d items / %d pages, want 500 / 400", cfg.Import.MaxItems, cfg.Import.MaxPages)
	}
}

// Zero would be quietly replaced by the fetcher's default, so "0" — plausibly
// meant as "import nothing" — would import fifty posts. It is refused instead.
func TestLoad_RejectsImportBoundsBelowOne(t *testing.T) {
	for _, args := range [][]string{
		{"--import-max-items", "0"},
		{"--import-max-items", "-5"},
		{"--import-max-pages", "0"},
	} {
		if _, err := loadArgs(args); err == nil {
			t.Errorf("loadArgs(%v) error = nil, want a refusal", args)
		}
	}
}

// import reads the limits but not the interval, so the two validate
// separately — the same split that lets healthcheck share Listen without
// inheriting the rest of HTTP.
func TestImportLimits_ValidateRejectsABoundBelowOne(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits ImportLimits
		want   string
	}{
		{"max items", ImportLimits{MaxItems: 0, MaxPages: 100}, "IMPORT_MAX_ITEMS"},
		{"max pages", ImportLimits{MaxItems: 50, MaxPages: 0}, "IMPORT_MAX_PAGES"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.limits.Validate()
			if err == nil {
				t.Fatalf("Validate(%+v) = nil, want an error naming %s", tc.limits, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() error = %v, want it to name %s", err, tc.want)
			}
		})
	}
}

// The interval stays with Import, and a valid pair of limits must not make it
// pass on its own.
func TestImport_ValidateStillRejectsANonPositiveInterval(t *testing.T) {
	cfg := Import{MaxItems: 50, MaxPages: 100}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() = nil for a zero interval, want an error")
	}
}
