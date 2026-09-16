// Package config loads runtime configuration from command-line flags and
// environment variables using github.com/alecthomas/kong.
package config

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/alecthomas/kong"
)

// Config holds all runtime configuration for the recipe-reader service.
//
// Each field is resolved with the precedence: command-line flag, then
// environment variable, then the built-in default. The four credentials are the
// exception — they have no flag form at all; see readCredentials.
type Config struct {
	// HTTPAddr defaults to loopback rather than all interfaces: the insecure
	// combination (no token, network-reachable) is then something an operator
	// has to ask for, not something they get by leaving a field unset.
	HTTPAddr string `name:"http-addr" env:"HTTP_ADDR" default:"127.0.0.1:8080" help:"Address the HTTP server listens on. Binding beyond loopback requires API_TOKEN."`
	DBDSN    string `name:"db-dsn" env:"DB_DSN" default:"postgres://recipes:recipes@localhost:5432/recipes?sslmode=disable" help:"PostgreSQL connection string."`

	// APIToken guards the mutating routes; see Config.validate for when it is
	// mandatory. It is read from API_TOKEN only. CORSOrigins is the
	// browser-origin allowlist — the bundled frontend is same-origin and needs
	// no entry, so the default covers only the Vite dev server.
	APIToken    string   `kong:"-"`
	CORSOrigins []string `name:"cors-origin" env:"CORS_ORIGINS" default:"http://localhost:5173" help:"Comma-separated browser origins allowed to read API responses."`

	// InstagramPassword is read from INSTAGRAM_PASSWORD only.
	InstagramUsername    string `name:"instagram-username" env:"INSTAGRAM_USERNAME" help:"Instagram account username used to fetch saved posts."`
	InstagramPassword    string `kong:"-"`
	InstagramSessionPath string `name:"instagram-session-path" env:"INSTAGRAM_SESSION_PATH" default:"data/instagram-session.json" help:"File used to persist the Instagram login session."`
	InstagramCollection  string `name:"instagram-collection" env:"INSTAGRAM_COLLECTION" help:"Saved-posts collection to import; empty imports all saved posts."`

	// The enum is load-bearing, not decoration: EXTRACTION_MODE used to accept
	// anything and only "hybrid" was ever inspected, so a typo — or the
	// perfectly reasonable "llm" — silently selected the weakest extractor.
	ExtractionMode      string  `name:"extraction-mode" env:"EXTRACTION_MODE" default:"hybrid" enum:"rule,llm,hybrid" help:"Recipe extraction strategy: rule, llm, or hybrid."`
	ExtractionThreshold float64 `name:"extraction-confidence-threshold" env:"EXTRACTION_CONFIDENCE_THRESHOLD" default:"0.6" help:"Minimum rule-based confidence before falling back to the LLM extractor."`

	// ExtractionPublishThreshold is a separate, stricter knob from
	// ExtractionThreshold. "Good enough to skip the LLM" and "good enough to
	// publish without a human reading it" are different questions, and one
	// number answering both is what made needs_review unreachable.
	ExtractionPublishThreshold float64 `name:"extraction-publish-threshold" env:"EXTRACTION_PUBLISH_THRESHOLD" default:"0.8" help:"Minimum extraction confidence to publish without review; below it a recipe is stored as needs_review."`

	// AnthropicAPIKey is read from ANTHROPIC_API_KEY only.
	AnthropicAPIKey string `kong:"-"`
	AnthropicModel  string `name:"anthropic-model" env:"ANTHROPIC_MODEL" default:"claude-opus-5" help:"Anthropic model id for the LLM extractor (fallback for LLM_MODEL when the provider is anthropic)."`

	// LLMAPIKey is read from LLM_API_KEY only.
	LLMProvider string `name:"llm-provider" env:"LLM_PROVIDER" default:"anthropic" enum:"anthropic,openai" help:"LLM extractor transport: anthropic, or openai for any OpenAI-compatible endpoint."`
	LLMAPIKey   string `kong:"-"`
	LLMModel    string `name:"llm-model" env:"LLM_MODEL" help:"Model id for the LLM extractor; falls back to ANTHROPIC_MODEL when the provider is anthropic."`
	LLMBaseURL  string `name:"llm-base-url" env:"LLM_BASE_URL" help:"Override the LLM endpoint base URL, e.g. https://api.groq.com/openai/v1 or http://localhost:11434/v1."`

	// LLMTimeout exists because a single stalled model call used to hold the
	// import worker indefinitely. 60s suits a hosted API; a local model on CPU
	// can need more, which is why it is a setting and not a constant.
	LLMTimeout time.Duration `name:"llm-timeout" env:"LLM_TIMEOUT" default:"60s" help:"Upper bound on one LLM extraction call, retries included. Raise it for a local model running on CPU."`

	ImportInterval time.Duration `name:"import-interval" env:"IMPORT_INTERVAL" default:"6h" help:"How often the background worker imports new saved posts."`

	// ImportMaxItems and ImportMaxPages bound one import run. They existed only
	// as package defaults in internal/instagram, reachable by recompiling; a
	// backfill of a large saved-posts history is exactly when an operator wants
	// to raise them, and a flagged account is when they want to lower them.
	// The defaults here match the package's own.
	ImportMaxItems int `name:"import-max-items" env:"IMPORT_MAX_ITEMS" default:"50" help:"Most new posts one import run collects. Posts already imported do not count against it."`
	ImportMaxPages int `name:"import-max-pages" env:"IMPORT_MAX_PAGES" default:"100" help:"Most feed pages one import run walks, whether or not they held anything new."`
}

// description is the --help preamble. It names the credentials because kong
// cannot: they are not flags, so it has no entry to list them under.
const description = "Imports recipes from Instagram saved posts, extracts structured data, and serves a searchable web UI.\n\n" +
	"Credentials are read from the environment only, never from flags: API_TOKEN (bearer token for writes; " +
	"required on a non-loopback HTTP_ADDR), INSTAGRAM_PASSWORD, LLM_API_KEY, and ANTHROPIC_API_KEY " +
	"(fallback for LLM_API_KEY with the anthropic provider)."

// LLMSettings is the resolved configuration for the LLM extractor, after the
// ANTHROPIC_* fallbacks have been applied.
type LLMSettings struct {
	Provider string
	APIKey   string
	Model    string
	BaseURL  string
	Timeout  time.Duration
}

// LLMSettings resolves the effective LLM extractor settings. The bool is false
// when no API key is configured for the selected provider — what the caller
// does about that depends on the extraction mode, which is the caller's
// decision to make and not this function's.
func (c Config) LLMSettings() (LLMSettings, bool) {
	s := LLMSettings{
		Provider: c.LLMProvider,
		APIKey:   c.LLMAPIKey,
		Model:    c.LLMModel,
		BaseURL:  c.LLMBaseURL,
		Timeout:  c.LLMTimeout,
	}
	if s.Provider == "" {
		s.Provider = "anthropic"
	}
	// ANTHROPIC_API_KEY and ANTHROPIC_MODEL predate the provider setting, so
	// they stay authoritative for the provider they were named after.
	if s.Provider == "anthropic" {
		if s.APIKey == "" {
			s.APIKey = c.AnthropicAPIKey
		}
		if s.Model == "" {
			s.Model = c.AnthropicModel
		}
	}
	return s, s.APIKey != ""
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
		kong.Description(description),
	)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if _, err := parser.Parse(args); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	cfg.readCredentials()
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// readCredentials fills in the four secrets from the environment.
//
// They are deliberately not flags. A flag puts its value in the process table,
// readable by every other user on the host with `ps aux`, and in shell history
// and any process-listing telemetry — an Instagram password and three billable
// or write-granting tokens. kong has no environment-only field: a field without
// a name tag still gets a flag named after it, which is why the fix the review
// proposed ("drop name:") would not have worked. So kong is told to ignore
// these fields (kong:"-") and they are read here instead.
func (c *Config) readCredentials() {
	c.APIToken = os.Getenv("API_TOKEN")
	c.InstagramPassword = os.Getenv("INSTAGRAM_PASSWORD")
	c.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	c.LLMAPIKey = os.Getenv("LLM_API_KEY")
}

// validate rejects configurations that parse but cannot be what the operator
// meant.
//
// The first is insecure by construction: a listener reachable from the network
// with nothing in front of the mutating routes. Loopback without a token stays
// allowed, because that is the single-user desktop case this project is built
// for.
//
// The second is a run bound below one. The fetcher would quietly replace it
// with its own default, so IMPORT_MAX_ITEMS=0 — plausibly meant as "pause
// imports" — would import fifty posts instead, which is the opposite.
func (c Config) validate() error {
	if c.APIToken == "" && !isLoopbackAddr(c.HTTPAddr) {
		return fmt.Errorf(
			"config: API_TOKEN is required when HTTP_ADDR (%q) is not loopback — "+
				"otherwise anyone who can route to the port can create, edit and delete recipes",
			c.HTTPAddr)
	}
	if c.ImportMaxItems < 1 {
		return fmt.Errorf("config: IMPORT_MAX_ITEMS must be at least 1, got %d", c.ImportMaxItems)
	}
	if c.ImportMaxPages < 1 {
		return fmt.Errorf("config: IMPORT_MAX_PAGES must be at least 1, got %d", c.ImportMaxPages)
	}
	return nil
}

// isLoopbackAddr reports whether addr binds the loopback interface only. A
// bare port (":8080") or an empty host means every interface, so it is not
// loopback — which is exactly the case that needs a token.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
