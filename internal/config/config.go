// Package config loads runtime configuration from command-line flags and
// environment variables using github.com/alecthomas/kong.
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/alecthomas/kong"
)

// Config holds all runtime configuration for the recipe-reader service.
//
// Each field is resolved with the precedence: command-line flag, then
// environment variable, then the built-in default.
type Config struct {
	HTTPAddr string `name:"http-addr" env:"HTTP_ADDR" default:":8080" help:"Address the HTTP server listens on."`
	DBDSN    string `name:"db-dsn" env:"DB_DSN" default:"postgres://recipes:recipes@localhost:5432/recipes?sslmode=disable" help:"PostgreSQL connection string."`

	InstagramUsername    string `name:"instagram-username" env:"INSTAGRAM_USERNAME" help:"Instagram account username used to fetch saved posts."`
	InstagramPassword    string `name:"instagram-password" env:"INSTAGRAM_PASSWORD" help:"Instagram account password."`
	InstagramSessionPath string `name:"instagram-session-path" env:"INSTAGRAM_SESSION_PATH" default:"data/instagram-session.json" help:"File used to persist the Instagram login session."`
	InstagramCollection  string `name:"instagram-collection" env:"INSTAGRAM_COLLECTION" help:"Saved-posts collection to import; empty imports all saved posts."`

	ExtractionMode      string  `name:"extraction-mode" env:"EXTRACTION_MODE" default:"hybrid" help:"Recipe extraction strategy: rule, llm, or hybrid."`
	ExtractionThreshold float64 `name:"extraction-confidence-threshold" env:"EXTRACTION_CONFIDENCE_THRESHOLD" default:"0.6" help:"Minimum rule-based confidence before falling back to the LLM extractor."`

	AnthropicAPIKey string `name:"anthropic-api-key" env:"ANTHROPIC_API_KEY" help:"API key for the Anthropic LLM extractor (fallback for LLM_API_KEY when the provider is anthropic)."`
	AnthropicModel  string `name:"anthropic-model" env:"ANTHROPIC_MODEL" default:"claude-opus-5" help:"Anthropic model id for the LLM extractor (fallback for LLM_MODEL when the provider is anthropic)."`

	LLMProvider string `name:"llm-provider" env:"LLM_PROVIDER" default:"anthropic" help:"LLM extractor transport: anthropic, or openai for any OpenAI-compatible endpoint."`
	LLMAPIKey   string `name:"llm-api-key" env:"LLM_API_KEY" help:"API key for the LLM extractor; falls back to ANTHROPIC_API_KEY when the provider is anthropic."`
	LLMModel    string `name:"llm-model" env:"LLM_MODEL" help:"Model id for the LLM extractor; falls back to ANTHROPIC_MODEL when the provider is anthropic."`
	LLMBaseURL  string `name:"llm-base-url" env:"LLM_BASE_URL" help:"Override the LLM endpoint base URL, e.g. https://api.groq.com/openai/v1 or http://localhost:11434/v1."`

	ImportInterval time.Duration `name:"import-interval" env:"IMPORT_INTERVAL" default:"6h" help:"How often the background worker imports new saved posts."`
}

// LLMExtractorConfig resolves the effective LLM extractor settings, applying
// the ANTHROPIC_* fallbacks for the anthropic provider. ok is false when no API
// key is configured for the selected provider, in which case the caller should
// disable the LLM fallback (pass a nil LLM to the hybrid extractor).
func (c Config) LLMExtractorConfig() (provider, apiKey, model, baseURL string, ok bool) {
	provider = c.LLMProvider
	if provider == "" {
		provider = "anthropic"
	}
	apiKey, model, baseURL = c.LLMAPIKey, c.LLMModel, c.LLMBaseURL
	if provider == "anthropic" {
		if apiKey == "" {
			apiKey = c.AnthropicAPIKey
		}
		if model == "" {
			model = c.AnthropicModel
		}
	}
	return provider, apiKey, model, baseURL, apiKey != ""
}

// Load parses configuration from the process command-line arguments and the
// environment. See Config for the precedence rules.
func Load() (Config, error) {
	return load(os.Args[1:])
}

// load is the testable core of Load, taking an explicit argument slice.
func load(args []string) (Config, error) {
	var cfg Config
	parser, err := kong.New(&cfg,
		kong.Name("recipe-reader"),
		kong.Description("Imports recipes from Instagram saved posts, extracts structured data, and serves a searchable web UI."),
	)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if _, err := parser.Parse(args); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}
