package config

import (
	"os"
	"testing"
	"time"
)

// clearEnv unsets the named environment variables for the duration of the test,
// restoring their original values afterwards. t.Setenv registers the restore
// hook (and guards against t.Parallel); os.Unsetenv then makes them absent so
// kong falls back to the struct defaults.
func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("Unsetenv(%q): %v", k, err)
		}
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t, "DB_DSN", "HTTP_ADDR", "IMPORT_INTERVAL", "EXTRACTION_CONFIDENCE_THRESHOLD", "EXTRACTION_MODE", "ANTHROPIC_MODEL")

	cfg, err := load(nil)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DBDSN == "" {
		t.Error("expected a default DBDSN")
	}
	if cfg.ImportInterval != 6*time.Hour {
		t.Errorf("ImportInterval = %v, want 6h", cfg.ImportInterval)
	}
	if cfg.ExtractionThreshold != 0.6 {
		t.Errorf("ExtractionThreshold = %v, want 0.6", cfg.ExtractionThreshold)
	}
	if cfg.ExtractionMode != "hybrid" {
		t.Errorf("ExtractionMode = %q, want hybrid", cfg.ExtractionMode)
	}
	if cfg.AnthropicModel != "claude-opus-5" {
		t.Errorf("AnthropicModel = %q, want claude-opus-5", cfg.AnthropicModel)
	}
}

func TestLoad_InvalidThreshold(t *testing.T) {
	t.Setenv("EXTRACTION_CONFIDENCE_THRESHOLD", "not-a-number")
	if _, err := load(nil); err == nil {
		t.Fatal("load() error = nil, want error for invalid threshold")
	}
}

func TestLoad_InvalidImportInterval(t *testing.T) {
	t.Setenv("IMPORT_INTERVAL", "not-a-duration")
	if _, err := load(nil); err == nil {
		t.Fatal("load() error = nil, want error for invalid import interval")
	}
}

func TestLoad_EnvOverridesDefault(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9999")
	t.Setenv("IMPORT_INTERVAL", "12h")
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")

	cfg, err := load(nil)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if cfg.HTTPAddr != ":9999" {
		t.Errorf("HTTPAddr = %q, want :9999", cfg.HTTPAddr)
	}
	if cfg.ImportInterval != 12*time.Hour {
		t.Errorf("ImportInterval = %v, want 12h", cfg.ImportInterval)
	}
	if cfg.AnthropicAPIKey != "sk-test" {
		t.Errorf("AnthropicAPIKey = %q, want sk-test", cfg.AnthropicAPIKey)
	}
}

func TestLoad_FlagsOverrideEnv(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9999")

	cfg, err := load([]string{
		"--http-addr", ":7777",
		"--extraction-mode", "rule",
		"--import-interval", "30m",
	})
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if cfg.HTTPAddr != ":7777" {
		t.Errorf("HTTPAddr = %q, want :7777 (flag should beat env)", cfg.HTTPAddr)
	}
	if cfg.ExtractionMode != "rule" {
		t.Errorf("ExtractionMode = %q, want rule", cfg.ExtractionMode)
	}
	if cfg.ImportInterval != 30*time.Minute {
		t.Errorf("ImportInterval = %v, want 30m", cfg.ImportInterval)
	}
}

func TestLoad_UnknownFlag(t *testing.T) {
	if _, err := load([]string{"--does-not-exist"}); err == nil {
		t.Fatal("load() error = nil, want error for unknown flag")
	}
}
