package config

import (
	"os"
	"testing"
	"time"

	"github.com/alecthomas/kong"
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

// loadArgs parses args into a Config the way the serve command does: kong
// applies defaults and environment, runs BeforeApply, applies the flags, then
// runs Validate.
func loadArgs(args []string) (Config, error) {
	var cfg Config
	parser, err := kong.New(&cfg, kong.Name("recipe-reader"))
	if err != nil {
		return Config{}, err
	}
	if _, err := parser.Parse(args); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t, "DB_DSN", "HTTP_ADDR", "IMPORT_INTERVAL", "EXTRACTION_CONFIDENCE_THRESHOLD", "EXTRACTION_MODE", "ANTHROPIC_MODEL", "API_TOKEN", "CORS_ORIGINS")

	cfg, err := loadArgs(nil)
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	// Loopback, not ":8080": the default deployment must not be reachable
	// from the network while its writes are unauthenticated.
	if cfg.HTTP.Addr != "127.0.0.1:8080" {
		t.Errorf("HTTPAddr = %q, want 127.0.0.1:8080", cfg.HTTP.Addr)
	}
	if len(cfg.HTTP.CORSOrigins) != 1 || cfg.HTTP.CORSOrigins[0] != "http://localhost:5173" {
		t.Errorf("CORSOrigins = %v, want the Vite dev server only", cfg.HTTP.CORSOrigins)
	}
	if cfg.Database.DSN == "" {
		t.Error("expected a default DBDSN")
	}
	if cfg.Import.Interval != 6*time.Hour {
		t.Errorf("ImportInterval = %v, want 6h", cfg.Import.Interval)
	}
	if cfg.Extraction.Threshold != 0.6 {
		t.Errorf("ExtractionThreshold = %v, want 0.6", cfg.Extraction.Threshold)
	}
	if cfg.Extraction.Mode != "hybrid" {
		t.Errorf("ExtractionMode = %q, want hybrid", cfg.Extraction.Mode)
	}
	if cfg.LLM.AnthropicModel != "claude-opus-5" {
		t.Errorf("AnthropicModel = %q, want claude-opus-5", cfg.LLM.AnthropicModel)
	}
}

func TestLoad_InvalidThreshold(t *testing.T) {
	t.Setenv("EXTRACTION_CONFIDENCE_THRESHOLD", "not-a-number")
	if _, err := loadArgs(nil); err == nil {
		t.Fatal("loadArgs() error = nil, want error for invalid threshold")
	}
}

func TestLoad_InvalidImportInterval(t *testing.T) {
	t.Setenv("IMPORT_INTERVAL", "not-a-duration")
	if _, err := loadArgs(nil); err == nil {
		t.Fatal("loadArgs() error = nil, want error for invalid import interval")
	}
}

func TestLoad_EnvOverridesDefault(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:9999")
	t.Setenv("IMPORT_INTERVAL", "12h")
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")

	cfg, err := loadArgs(nil)
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	if cfg.HTTP.Addr != "127.0.0.1:9999" {
		t.Errorf("HTTPAddr = %q, want 127.0.0.1:9999", cfg.HTTP.Addr)
	}
	if cfg.Import.Interval != 12*time.Hour {
		t.Errorf("ImportInterval = %v, want 12h", cfg.Import.Interval)
	}
	if cfg.LLM.AnthropicAPIKey != "sk-test" {
		t.Errorf("AnthropicAPIKey = %q, want sk-test", cfg.LLM.AnthropicAPIKey)
	}
}

func TestLoad_FlagsOverrideEnv(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:9999")

	cfg, err := loadArgs([]string{
		"--http-addr", "127.0.0.1:7777",
		"--extraction-mode", "rule",
		"--import-interval", "30m",
	})
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	if cfg.HTTP.Addr != "127.0.0.1:7777" {
		t.Errorf("HTTPAddr = %q, want 127.0.0.1:7777 (flag should beat env)", cfg.HTTP.Addr)
	}
	if cfg.Extraction.Mode != "rule" {
		t.Errorf("ExtractionMode = %q, want rule", cfg.Extraction.Mode)
	}
	if cfg.Import.Interval != 30*time.Minute {
		t.Errorf("ImportInterval = %v, want 30m", cfg.Import.Interval)
	}
}

func TestLoad_UnknownFlag(t *testing.T) {
	if _, err := loadArgs([]string{"--does-not-exist"}); err == nil {
		t.Fatal("loadArgs() error = nil, want error for unknown flag")
	}
}

// A network-reachable bind with no token in front of the mutating routes is
// the combination that makes S1 exploitable from anywhere that can route to
// the port, so Validate refuses it.
func TestLoad_RejectsNonLoopbackBindWithoutToken(t *testing.T) {
	clearEnv(t, "API_TOKEN")

	for _, addr := range []string{":8080", "0.0.0.0:8080", "192.168.1.10:8080"} {
		if _, err := loadArgs([]string{"--http-addr", addr}); err == nil {
			t.Errorf("loadArgs(--http-addr %s) error = nil, want a refusal without API_TOKEN", addr)
		}
	}
}

func TestLoad_AllowsNonLoopbackBindWithToken(t *testing.T) {
	t.Setenv("API_TOKEN", "s3cret-token")

	cfg, err := loadArgs([]string{"--http-addr", ":8080"})
	if err != nil {
		t.Fatalf("loadArgs() error = %v, want success once a token is configured", err)
	}
	if cfg.HTTP.APIToken != "s3cret-token" {
		t.Errorf("APIToken = %q", cfg.HTTP.APIToken)
	}
}

// Loopback stays usable without a token: that is the single-user desktop case
// the project is built for, and requiring a token there would only train
// people to set a fixed one.
func TestLoad_AllowsLoopbackBindWithoutToken(t *testing.T) {
	clearEnv(t, "API_TOKEN")

	for _, addr := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"} {
		if _, err := loadArgs([]string{"--http-addr", addr}); err != nil {
			t.Errorf("loadArgs(--http-addr %s) error = %v, want success", addr, err)
		}
	}
}

func TestLoad_CORSOriginsSplitOnComma(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "http://localhost:5173,https://recipes.example")

	cfg, err := loadArgs(nil)
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	if len(cfg.HTTP.CORSOrigins) != 2 || cfg.HTTP.CORSOrigins[1] != "https://recipes.example" {
		t.Errorf("CORSOrigins = %v, want both origins", cfg.HTTP.CORSOrigins)
	}
}

// EXTRACTION_MODE used to accept anything, and only "hybrid" was ever
// inspected — so a typo silently selected the weakest extractor rather than
// failing. The enum is what makes a wrong value loud.
func TestLoad_RejectsUnknownExtractionMode(t *testing.T) {
	if _, err := loadArgs([]string{"--extraction-mode", "banana"}); err == nil {
		t.Error("loadArgs() error = nil, want a refusal for an unknown extraction mode")
	}
	for _, mode := range []string{"rule", "llm", "hybrid"} {
		if _, err := loadArgs([]string{"--extraction-mode", mode}); err != nil {
			t.Errorf("loadArgs(--extraction-mode %s) error = %v", mode, err)
		}
	}
}

func TestLoad_RejectsUnknownLLMProvider(t *testing.T) {
	if _, err := loadArgs([]string{"--llm-provider", "gemini"}); err == nil {
		t.Error("loadArgs() error = nil, want a refusal for an unsupported provider")
	}
}

func TestLoad_PublishThresholdDefaultsAboveTheFallbackThreshold(t *testing.T) {
	clearEnv(t, "EXTRACTION_CONFIDENCE_THRESHOLD", "EXTRACTION_PUBLISH_THRESHOLD")

	cfg, err := loadArgs(nil)
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	// Publishing without review must be the stricter of the two questions;
	// they were one number, which is what made needs_review unreachable.
	if cfg.Extraction.PublishThreshold <= cfg.Extraction.Threshold {
		t.Errorf("publish threshold %v is not stricter than the fallback threshold %v",
			cfg.Extraction.PublishThreshold, cfg.Extraction.Threshold)
	}
}

// LLM_* wins where set, and the older ANTHROPIC_* names stay authoritative for
// the provider they were named after.
func TestLLMSettings_Precedence(t *testing.T) {
	llm := LLM{
		Provider: "anthropic", APIKey: "sk-llm", Model: "claude-new",
		AnthropicAPIKey: "sk-ant", AnthropicModel: "claude-old",
	}
	settings, ok := llm.Settings()
	if !ok || settings.APIKey != "sk-llm" || settings.Model != "claude-new" {
		t.Errorf("settings = %+v, ok = %v, want the LLM_* values to win", settings, ok)
	}

	fallback := LLM{Provider: "", AnthropicAPIKey: "sk-ant", AnthropicModel: "claude-old"}
	settings, ok = fallback.Settings()
	if !ok || settings.Provider != "anthropic" || settings.APIKey != "sk-ant" || settings.Model != "claude-old" {
		t.Errorf("settings = %+v, ok = %v, want the ANTHROPIC_* fallbacks", settings, ok)
	}

	none := LLM{Provider: "anthropic"}
	if _, ok := none.Settings(); ok {
		t.Error("Settings() ok = true with no key configured anywhere")
	}
}

// The anthropic provider does not fall back to an OpenAI key, and the openai
// provider does not borrow ANTHROPIC_API_KEY.
func TestLLMSettings_FallbacksAreProviderScoped(t *testing.T) {
	anthropic := LLM{Provider: "anthropic", AnthropicAPIKey: "sk-ant", AnthropicModel: "claude-x"}
	settings, ok := anthropic.Settings()
	if !ok || settings.APIKey != "sk-ant" || settings.Model != "claude-x" {
		t.Errorf("anthropic settings = %+v, ok = %v", settings, ok)
	}

	openAI := LLM{Provider: "openai", AnthropicAPIKey: "sk-ant"}
	if settings, ok := openAI.Settings(); ok {
		t.Errorf("openai settings = %+v, ok = %v — ANTHROPIC_API_KEY must not carry over", settings, ok)
	}
}
