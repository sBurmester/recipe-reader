package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/api"
	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/extraction"
)

// baseConfig is a config far enough along to wire an extractor, with no API key
// unless a test adds one.
func baseConfig(mode string) config.Config {
	return config.Config{
		HTTPAddr:            "127.0.0.1:8080",
		ExtractionMode:      mode,
		ExtractionThreshold: 0.6,
		LLMProvider:         "anthropic",
	}
}

// This is the defect. EXTRACTION_MODE=llm with a key present used to return the
// same nil-LLM hybrid as EXTRACTION_MODE=rule — the weakest extractor, selected
// by asking for the strongest, on every post of every run.
func TestNewExtractor_LLMModeReturnsTheLLM(t *testing.T) {
	cfg := baseConfig("llm")
	cfg.AnthropicAPIKey = "sk-test"

	got, err := newExtractor(cfg)
	if err != nil {
		t.Fatalf("newExtractor() error = %v", err)
	}
	if _, ok := got.(*extraction.LLMExtractor); !ok {
		t.Errorf("newExtractor() = %T, want *extraction.LLMExtractor", got)
	}
}

// Asking for the LLM with no key is a broken deployment, not a rules-only one.
// Saying so at boot costs one run, where degrading quietly cost a month of weak
// extractions nobody attributed to the configuration.
func TestNewExtractor_LLMModeWithoutKeyFails(t *testing.T) {
	if _, err := newExtractor(baseConfig("llm")); err == nil {
		t.Error("newExtractor() error = nil, want a refusal when mode=llm has no API key")
	}
}

func TestNewExtractor_RuleModeReturnsRulesOnly(t *testing.T) {
	cfg := baseConfig("rule")
	// Even with a key configured, "rule" means rule.
	cfg.AnthropicAPIKey = "sk-test"

	got, err := newExtractor(cfg)
	if err != nil {
		t.Fatalf("newExtractor() error = %v", err)
	}
	if _, ok := got.(*extraction.RuleBasedExtractor); !ok {
		t.Errorf("newExtractor() = %T, want *extraction.RuleBasedExtractor", got)
	}
}

func TestNewExtractor_HybridModeWiresTheLLMWhenAKeyIsPresent(t *testing.T) {
	cfg := baseConfig("hybrid")
	cfg.AnthropicAPIKey = "sk-test"

	got, err := newExtractor(cfg)
	if err != nil {
		t.Fatalf("newExtractor() error = %v", err)
	}
	hybrid, ok := got.(*extraction.HybridExtractor)
	if !ok {
		t.Fatalf("newExtractor() = %T, want *extraction.HybridExtractor", got)
	}
	if hybrid.LLM == nil {
		t.Error("hybrid.LLM is nil despite a configured API key")
	}
}

// Rules-only is a coherent answer for "hybrid", which asks for the LLM only
// where the rules fall short — so this one degrades rather than failing.
func TestNewExtractor_HybridModeWithoutKeyIsRulesOnly(t *testing.T) {
	got, err := newExtractor(baseConfig("hybrid"))
	if err != nil {
		t.Fatalf("newExtractor() error = %v", err)
	}
	hybrid, ok := got.(*extraction.HybridExtractor)
	if !ok {
		t.Fatalf("newExtractor() = %T, want *extraction.HybridExtractor", got)
	}
	if hybrid.LLM != nil {
		t.Error("hybrid.LLM is set despite no configured API key")
	}
}

func TestNewExtractor_UnknownModeFails(t *testing.T) {
	if _, err := newExtractor(baseConfig("banana")); err == nil {
		t.Error("newExtractor() error = nil, want a refusal for an unknown mode")
	}
}

// LLM_API_KEY and LLM_MODEL win, with the older ANTHROPIC_* names still
// authoritative for the provider they were named after.
func TestNewExtractor_OpenAIProviderNeedsAModel(t *testing.T) {
	cfg := baseConfig("llm")
	cfg.LLMProvider = "openai"
	cfg.LLMAPIKey = "sk-test"

	if _, err := newExtractor(cfg); err == nil {
		t.Error("newExtractor() error = nil, want a refusal for the openai provider with no model")
	}

	cfg.LLMModel = "llama-3.3-70b"
	if _, err := newExtractor(cfg); err != nil {
		t.Errorf("newExtractor() error = %v once a model is set", err)
	}
}

// The anthropic provider does not fall back to an OpenAI key, and the openai
// provider does not borrow ANTHROPIC_API_KEY.
func TestLLMSettings_FallbacksAreProviderScoped(t *testing.T) {
	anthropic := config.Config{LLMProvider: "anthropic", AnthropicAPIKey: "sk-ant", AnthropicModel: "claude-x"}
	settings, ok := anthropic.LLMSettings()
	if !ok || settings.APIKey != "sk-ant" || settings.Model != "claude-x" {
		t.Errorf("anthropic settings = %+v, ok = %v", settings, ok)
	}

	openAI := config.Config{LLMProvider: "openai", AnthropicAPIKey: "sk-ant"}
	if settings, ok := openAI.LLMSettings(); ok {
		t.Errorf("openai settings = %+v, ok = %v — ANTHROPIC_API_KEY must not carry over", settings, ok)
	}
}

// newServer is the wiring run() has no seam for: a route registered on the
// wrong mux, or the frontend shadowing /api/, would otherwise surface only when
// somebody opened the page.
func TestNewServer_RoutesAPIAndFrontendSeparately(t *testing.T) {
	cfg := baseConfig("rule")
	cfg.CORSOrigins = []string{"http://localhost:5173"}

	server, err := newServer(cfg, api.Deps{})
	if err != nil {
		t.Fatalf("newServer() error = %v", err)
	}
	if server.Addr != cfg.HTTPAddr {
		t.Errorf("Addr = %q, want %q", server.Addr, cfg.HTTPAddr)
	}

	// The API router answers its own health route...
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/api/healthz status = %d, want 200", rec.Code)
	}

	// ...and everything else falls to the embedded frontend, not to a 404.
	rec = httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/deep/link", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/some/deep/link status = %d, want 200 (frontend app shell)", rec.Code)
	}
}

// The security middleware has to be reachable through the assembled server, not
// only through NewRouter in the api package's own tests.
func TestNewServer_AppliesTheConfiguredToken(t *testing.T) {
	cfg := baseConfig("rule")
	cfg.APIToken = "s3cret-token"

	server, err := newServer(cfg, api.Deps{})
	if err != nil {
		t.Fatalf("newServer() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/recipes/1", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated DELETE status = %d, want 401", rec.Code)
	}
}
