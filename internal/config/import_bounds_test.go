package config

import "testing"

// integration I6: the per-run bounds existed only as package defaults. They
// are reachable from configuration now, with the same defaults.
func TestLoad_ImportBounds(t *testing.T) {
	clearEnv(t, "IMPORT_MAX_ITEMS", "IMPORT_MAX_PAGES")

	cfg, err := loadArgs(nil)
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	if cfg.ImportMaxItems != 50 || cfg.ImportMaxPages != 100 {
		t.Errorf("defaults = %d items / %d pages, want 50 / 100", cfg.ImportMaxItems, cfg.ImportMaxPages)
	}

	t.Setenv("IMPORT_MAX_ITEMS", "500")
	cfg, err = loadArgs([]string{"--import-max-pages", "400"})
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	if cfg.ImportMaxItems != 500 || cfg.ImportMaxPages != 400 {
		t.Errorf("configured = %d items / %d pages, want 500 / 400", cfg.ImportMaxItems, cfg.ImportMaxPages)
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
