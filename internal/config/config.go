// Package config declares recipe-reader's settings as kong flag groups and
// validates them. It parses nothing itself: internal/cli embeds the groups into
// the commands that need them, and kong fills them — and calls BeforeApply and
// Validate — only for the command being run.
//
// Every setting is resolved with the precedence: command-line flag, then
// environment variable, then the built-in default. The four credentials are
// the exception — they have no flag form at all; see HTTP.BeforeApply.
package config

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/alecthomas/kong"
)

// Config is every setting the serve command takes, one embedded group per
// concern. The group tags are the headings in `recipe-reader serve --help`;
// flag and environment names carry no group prefix, so they are the same as
// before the settings were grouped.
type Config struct {
	HTTP       HTTP       `embed:"" group:"HTTP"`
	Database   Database   `embed:"" group:"Database"`
	Instagram  Instagram  `embed:"" group:"Instagram"`
	Extraction Extraction `embed:"" group:"Extraction"`
	LLM        LLM        `embed:"" group:"LLM"`
	Import     Import     `embed:"" group:"Import"`
}

// Groups are the headings of `recipe-reader serve --help`, for
// kong.ExplicitGroups. Their descriptions name the credentials: those are not
// flags, so kong has no entry to list them under, and the app description that
// names them too is printed only at the top level, not where the flags are.
func Groups() []kong.Group {
	return []kong.Group{
		{Key: "HTTP", Title: "HTTP", Description: "API_TOKEN, read from the environment only, guards the write routes; it is required when --http-addr is not loopback."},
		{Key: "Database", Title: "Database"},
		{Key: "Instagram", Title: "Instagram", Description: "INSTAGRAM_PASSWORD is read from the environment only. Without --instagram-username the server runs with no import worker."},
		{Key: "Extraction", Title: "Extraction"},
		{Key: "LLM", Title: "LLM", Description: "LLM_API_KEY, and ANTHROPIC_API_KEY as its fallback for the anthropic provider, are read from the environment only."},
		{Key: "Import", Title: "Import"},
		{Key: "Logging", Title: "Logging", Description: "Applies to every command, and may be given before or after the command name."},
	}
}

// Listen is the address the HTTP server binds. It is a group of its own because
// the healthcheck command needs it and nothing else.
type Listen struct {
	// Addr defaults to loopback rather than all interfaces: the insecure
	// combination (no token, network-reachable) is then something an operator
	// has to ask for, not something they get by leaving a field unset.
	//
	// The help text is shared by serve and healthcheck, so it says nothing
	// about API_TOKEN, which a probe never needs; the HTTP group's description
	// in Groups carries that rule.
	Addr string `name:"http-addr" env:"HTTP_ADDR" default:"127.0.0.1:8080" help:"Address the HTTP server listens on; healthcheck probes an unspecified host (\":8080\") on loopback."`
}

// Logging is how the process writes its own logs. It is a group of its own,
// declared on the root of the command tree rather than in Config, because every
// command logs — healthcheck and migrate included — while Config is what serve
// takes.
type Logging struct {
	Level  string `name:"log-level" env:"LOG_LEVEL" default:"info" enum:"debug,info,warn,error" help:"Lowest level that is logged: debug, info, warn, error."`
	Format string `name:"log-format" env:"LOG_FORMAT" default:"text" enum:"text,json" help:"Log output format: text for a human, json for a log collector."`
}

// HTTP is the server's listener and the access rules in front of it.
type HTTP struct {
	Listen `embed:""`

	// CORSOrigins is the browser-origin allowlist — the bundled frontend is
	// same-origin and needs no entry, so the default covers only the Vite dev
	// server.
	CORSOrigins []string `name:"cors-origin" env:"CORS_ORIGINS" default:"http://localhost:5173" help:"Comma-separated browser origins allowed to read API responses."`

	// APIToken guards the mutating routes; see Validate for when it is
	// mandatory. It is read from API_TOKEN only.
	APIToken string `kong:"-"`
}

// BeforeApply reads API_TOKEN from the environment.
//
// The four credentials — this one, INSTAGRAM_PASSWORD, LLM_API_KEY and
// ANTHROPIC_API_KEY — are deliberately not flags. A flag puts its value in the
// process table, readable by every other user on the host with `ps aux`, and in
// shell history and any process-listing telemetry — an Instagram password and
// three billable or write-granting tokens. kong has no environment-only field:
// a field without a name tag still gets a flag named after it. So kong is told
// to ignore these fields (kong:"-") and each group's BeforeApply hook reads its
// own.
//
// BeforeApply, not AfterApply: kong runs Validate before AfterApply, and
// HTTP.Validate has to see the token.
func (h *HTTP) BeforeApply() error {
	h.APIToken = os.Getenv("API_TOKEN")
	return nil
}

// Validate rejects a configuration that is insecure by construction: a
// listener reachable from the network with nothing in front of the mutating
// routes. Loopback without a token stays allowed, because that is the
// single-user desktop case this project is built for.
func (h HTTP) Validate() error {
	if h.APIToken == "" && !isLoopbackAddr(h.Addr) {
		return fmt.Errorf(
			"config: API_TOKEN is required when HTTP_ADDR (%q) is not loopback — "+
				"otherwise anyone who can route to the port can create, edit and delete recipes",
			h.Addr)
	}
	return nil
}

// Database is the PostgreSQL connection.
type Database struct {
	DSN string `name:"db-dsn" env:"DB_DSN" default:"postgres://recipes:recipes@localhost:5432/recipes?sslmode=disable" help:"PostgreSQL connection string."`
}

// Instagram is the account whose saved posts are imported. Without a username
// the server runs with no import worker.
type Instagram struct {
	Username string `name:"instagram-username" env:"INSTAGRAM_USERNAME" help:"Instagram account username used to fetch saved posts."`
	// Password is read from INSTAGRAM_PASSWORD only; see HTTP.BeforeApply.
	Password    string `kong:"-"`
	SessionPath string `name:"instagram-session-path" env:"INSTAGRAM_SESSION_PATH" default:"data/instagram-session.json" help:"File used to persist the Instagram login session."`
	Collection  string `name:"instagram-collection" env:"INSTAGRAM_COLLECTION" help:"Saved-posts collection to import; empty imports all saved posts."`
}

// BeforeApply reads INSTAGRAM_PASSWORD from the environment; see
// HTTP.BeforeApply for why it is not a flag.
func (i *Instagram) BeforeApply() error {
	i.Password = os.Getenv("INSTAGRAM_PASSWORD")
	return nil
}

// Extraction selects the extraction strategy and the two confidence thresholds
// it is judged by.
type Extraction struct {
	// The enum is load-bearing, not decoration: EXTRACTION_MODE used to accept
	// anything and only "hybrid" was ever inspected, so a typo — or the
	// perfectly reasonable "llm" — silently selected the weakest extractor.
	Mode      string  `name:"extraction-mode" env:"EXTRACTION_MODE" default:"hybrid" enum:"rule,llm,hybrid" help:"Recipe extraction strategy: rule, llm, or hybrid."`
	Threshold float64 `name:"extraction-confidence-threshold" env:"EXTRACTION_CONFIDENCE_THRESHOLD" default:"0.6" help:"Minimum rule-based confidence before falling back to the LLM extractor."`

	// PublishThreshold is a separate, stricter knob from Threshold. "Good
	// enough to skip the LLM" and "good enough to publish without a human
	// reading it" are different questions, and one number answering both is
	// what made needs_review unreachable.
	PublishThreshold float64 `name:"extraction-publish-threshold" env:"EXTRACTION_PUBLISH_THRESHOLD" default:"0.8" help:"Minimum extraction confidence to publish without review; below it a recipe is stored as needs_review."`
}

// Validate rejects either threshold outside 0..1. Both are compared against a
// confidence the extractors only ever produce in that range, so a value outside
// it does not fail: it silently makes one branch unreachable.
// EXTRACTION_PUBLISH_THRESHOLD=80, meaning a percentage, would send every
// recipe to needs_review, and EXTRACTION_CONFIDENCE_THRESHOLD=80 would call the
// LLM for every post — on a paid API, indefinitely, with nothing in the logs
// pointing at the configuration.
func (e Extraction) Validate() error {
	if err := validateThreshold("EXTRACTION_CONFIDENCE_THRESHOLD", e.Threshold); err != nil {
		return err
	}
	return validateThreshold("EXTRACTION_PUBLISH_THRESHOLD", e.PublishThreshold)
}

// LLM configures the LLM extractor. AnthropicAPIKey and AnthropicModel predate
// Provider and stay the fallbacks for the provider they were named after; see
// Settings.
type LLM struct {
	Provider string `name:"llm-provider" env:"LLM_PROVIDER" default:"anthropic" enum:"anthropic,openai" help:"LLM extractor transport: anthropic, or openai for any OpenAI-compatible endpoint."`
	// APIKey is read from LLM_API_KEY only; see HTTP.BeforeApply.
	APIKey  string `kong:"-"`
	Model   string `name:"llm-model" env:"LLM_MODEL" help:"Model id for the LLM extractor; falls back to ANTHROPIC_MODEL when the provider is anthropic."`
	BaseURL string `name:"llm-base-url" env:"LLM_BASE_URL" help:"Override the LLM endpoint base URL, e.g. https://api.groq.com/openai/v1 or http://localhost:11434/v1."`

	// Timeout exists because a single stalled model call used to hold the
	// import worker indefinitely. 60s suits a hosted API; a local model on CPU
	// can need more, which is why it is a setting and not a constant.
	Timeout time.Duration `name:"llm-timeout" env:"LLM_TIMEOUT" default:"60s" help:"Upper bound on one LLM extraction call, retries included. Raise it for a local model running on CPU."`

	// AnthropicAPIKey is read from ANTHROPIC_API_KEY only; see HTTP.BeforeApply.
	AnthropicAPIKey string `kong:"-"`
	AnthropicModel  string `name:"anthropic-model" env:"ANTHROPIC_MODEL" default:"claude-opus-5" help:"Anthropic model id for the LLM extractor (fallback for LLM_MODEL when the provider is anthropic)."`
}

// BeforeApply reads LLM_API_KEY and ANTHROPIC_API_KEY from the environment;
// see HTTP.BeforeApply for why they are not flags.
func (l *LLM) BeforeApply() error {
	l.APIKey = os.Getenv("LLM_API_KEY")
	l.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	return nil
}

// LLMSettings is the resolved configuration for the LLM extractor, after the
// ANTHROPIC_* fallbacks have been applied.
type LLMSettings struct {
	Provider string
	APIKey   string
	Model    string
	BaseURL  string
	Timeout  time.Duration
}

// Settings resolves the effective LLM extractor settings. The bool is false
// when no API key is configured for the selected provider — what the caller
// does about that depends on the extraction mode, which is the caller's
// decision to make and not this function's.
func (l LLM) Settings() (LLMSettings, bool) {
	s := LLMSettings{
		Provider: l.Provider,
		APIKey:   l.APIKey,
		Model:    l.Model,
		BaseURL:  l.BaseURL,
		Timeout:  l.Timeout,
	}
	if s.Provider == "" {
		s.Provider = "anthropic"
	}
	// ANTHROPIC_API_KEY and ANTHROPIC_MODEL predate the provider setting, so
	// they stay authoritative for the provider they were named after.
	if s.Provider == "anthropic" {
		if s.APIKey == "" {
			s.APIKey = l.AnthropicAPIKey
		}
		if s.Model == "" {
			s.Model = l.AnthropicModel
		}
	}
	return s, s.APIKey != ""
}

// ImportLimits bounds one import run. It is a group of its own because both
// readers want it and only one of them wants the schedule: the worker in serve
// runs on an interval, the import command runs once. Same reason Listen is
// separate from HTTP.
//
// The two existed only as package defaults in internal/instagram, reachable by
// recompiling; a backfill of a large saved-posts history is exactly when an
// operator wants to raise them, and a flagged account is when they want to
// lower them. The defaults here match the package's own.
type ImportLimits struct {
	MaxItems int `name:"import-max-items" env:"IMPORT_MAX_ITEMS" default:"50" help:"Most new posts one import run collects. Posts already imported do not count against it."`
	MaxPages int `name:"import-max-pages" env:"IMPORT_MAX_PAGES" default:"100" help:"Most feed pages one import run walks, whether or not they held anything new."`
}

// Validate rejects a run bound below one: the fetcher would quietly replace it
// with its own default, so IMPORT_MAX_ITEMS=0 — plausibly meant as "pause
// imports" — would import fifty posts instead, which is the opposite.
func (i ImportLimits) Validate() error {
	if i.MaxItems < 1 {
		return fmt.Errorf("config: IMPORT_MAX_ITEMS must be at least 1, got %d", i.MaxItems)
	}
	if i.MaxPages < 1 {
		return fmt.Errorf("config: IMPORT_MAX_PAGES must be at least 1, got %d", i.MaxPages)
	}
	return nil
}

// Import schedules and bounds the background import.
type Import struct {
	Interval time.Duration `name:"import-interval" env:"IMPORT_INTERVAL" default:"6h" help:"How often the background worker imports new saved posts."`

	ImportLimits `embed:""`
}

// Validate rejects an interval at or below zero: time.NewTicker panics for one,
// on the calling goroutine, inside Worker.Start — so IMPORT_INTERVAL=0 used to
// abort the process with a stack trace instead of the clean slog.Error("fatal")
// path main was built around. The limits validate themselves.
func (i Import) Validate() error {
	if i.Interval <= 0 {
		return fmt.Errorf("config: IMPORT_INTERVAL must be positive, got %s", i.Interval)
	}
	return nil
}

// validateThreshold rejects a confidence threshold outside 0..1. NaN fails the
// comparison in both directions, which is why the test is written as "not
// inside the range" rather than as two bounds checks.
func validateThreshold(name string, v float64) error {
	if !(v >= 0 && v <= 1) {
		return fmt.Errorf("config: %s must be between 0 and 1, got %v", name, v)
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
