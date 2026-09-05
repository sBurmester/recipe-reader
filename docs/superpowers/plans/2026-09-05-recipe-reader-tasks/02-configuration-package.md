> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 0: Bootstrap & Tooling.
>
> **Status:** [x] done

# Task 2: Configuration Package

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config` struct (fields: `HTTPAddr`, `DBDSN`, `InstagramUsername`, `InstagramPassword`, `InstagramSessionPath`, `InstagramCollection`, `ExtractionMode`, `ExtractionThreshold float64`, `AnthropicAPIKey`, `AnthropicModel`, `ImportInterval time.Duration`) and `config.Load() (Config, error)`. All later tasks (Task 3 DB, Task 8 LLM, Task 10 Instagram, Task 13 worker, Task 18 main) consume `config.Config` by field name above — do not rename fields later.

> **Deviation from plan (per user request):** parsing is done with
> [`github.com/alecthomas/kong`](https://github.com/alecthomas/kong) via struct tags, so every
> setting is also a command-line flag. Precedence is flag → env var → `default:` tag. `Load()`
> keeps its `() (Config, error)` signature and parses `os.Args[1:]`; an unexported
> `load(args []string)` is the testable core. kong parses a *set-but-empty* env var as an empty
> value (it does not fall back to the default), so the defaults test unsets the vars rather than
> setting them to `""`.

- [x] **Step 1: Write the failing test**

```go
// internal/config/config_test.go — see the file for the full suite. Key cases:
//   TestLoad_Defaults           unset env -> struct defaults (:8080, 6h, 0.6, hybrid, claude-opus-5)
//   TestLoad_InvalidThreshold   EXTRACTION_CONFIDENCE_THRESHOLD=not-a-number -> error
//   TestLoad_InvalidImportInterval  IMPORT_INTERVAL=not-a-duration -> error
//   TestLoad_EnvOverridesDefault    env var beats default
//   TestLoad_FlagsOverrideEnv       --http-addr flag beats $HTTP_ADDR
//   TestLoad_UnknownFlag            --does-not-exist -> error
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v`
Expected: FAIL — `undefined: load` / `undefined: Config`.

- [x] **Step 3: Implement**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/alecthomas/kong"
)

type Config struct {
	HTTPAddr string `name:"http-addr" env:"HTTP_ADDR" default:":8080" help:"Address the HTTP server listens on."`
	DBDSN    string `name:"db-dsn" env:"DB_DSN" default:"postgres://recipes:recipes@localhost:5432/recipes?sslmode=disable" help:"PostgreSQL connection string."`

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

func Load() (Config, error) { return load(os.Args[1:]) }

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
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/config go.mod go.sum
git commit -m "$(cat <<'EOF'
feat: add kong-based flag/env configuration loader

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 1](01-project-scaffold-tooling.md) · [Task 3 →](03-database-schema-migrations-sqlc-codegen-domain-models.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
