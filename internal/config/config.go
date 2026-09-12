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
// environment variable, then the built-in default.
type Config struct {
	// HTTPAddr defaults to loopback rather than all interfaces: the insecure
	// combination (no token, network-reachable) is then something an operator
	// has to ask for, not something they get by leaving a field unset.
	HTTPAddr string `name:"http-addr" env:"HTTP_ADDR" default:"127.0.0.1:8080" help:"Address the HTTP server listens on. Binding beyond loopback requires API_TOKEN."`
	DBDSN    string `name:"db-dsn" env:"DB_DSN" default:"postgres://recipes:recipes@localhost:5432/recipes?sslmode=disable" help:"PostgreSQL connection string."`

	// APIToken guards the mutating routes; see Config.validate for when it is
	// mandatory. CORSOrigins is the browser-origin allowlist — the bundled
	// frontend is same-origin and needs no entry, so the default covers only
	// the Vite dev server.
	APIToken    string   `name:"api-token" env:"API_TOKEN" help:"Bearer token required on POST/PUT/PATCH/DELETE. Empty leaves writes unauthenticated, which is only allowed on a loopback bind."`
	CORSOrigins []string `name:"cors-origin" env:"CORS_ORIGINS" default:"http://localhost:5173" help:"Comma-separated browser origins allowed to read API responses."`

	InstagramUsername    string `name:"instagram-username" env:"INSTAGRAM_USERNAME" help:"Instagram account username used to fetch saved posts."`
	InstagramPassword    string `name:"instagram-password" env:"INSTAGRAM_PASSWORD" help:"Instagram account password."`
	InstagramSessionPath string `name:"instagram-session-path" env:"INSTAGRAM_SESSION_PATH" default:"data/instagram-session.json" help:"File used to persist the Instagram login session."`
	InstagramCollection  string `name:"instagram-collection" env:"INSTAGRAM_COLLECTION" help:"Saved-posts collection to import; empty imports all saved posts."`

	ExtractionMode      string  `name:"extraction-mode" env:"EXTRACTION_MODE" default:"hybrid" help:"Recipe extraction strategy: rule, llm, or hybrid."`
	ExtractionThreshold float64 `name:"extraction-confidence-threshold" env:"EXTRACTION_CONFIDENCE_THRESHOLD" default:"0.6" help:"Minimum rule-based confidence before falling back to the LLM extractor."`

	AnthropicAPIKey string `name:"anthropic-api-key" env:"ANTHROPIC_API_KEY" help:"API key for the Anthropic LLM extractor."`
	AnthropicModel  string `name:"anthropic-model" env:"ANTHROPIC_MODEL" default:"claude-opus-5" help:"Anthropic model id for the LLM extractor."`

	ImportInterval time.Duration `name:"import-interval" env:"IMPORT_INTERVAL" default:"6h" help:"How often the background worker imports new saved posts."`
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
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate rejects the one combination that is insecure by construction: a
// listener reachable from the network with nothing in front of the mutating
// routes. Loopback without a token stays allowed, because that is the
// single-user desktop case this project is built for.
func (c Config) validate() error {
	if c.APIToken == "" && !isLoopbackAddr(c.HTTPAddr) {
		return fmt.Errorf(
			"config: API_TOKEN is required when HTTP_ADDR (%q) is not loopback — "+
				"otherwise anyone who can route to the port can create, edit and delete recipes",
			c.HTTPAddr)
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
