> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 0: Bootstrap & Tooling.
>
> **Status:** [x] done

# Task 2: Configuration Package

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config` struct (fields: `HTTPAddr`, `DBDSN`, `InstagramUsername`, `InstagramPassword`, `InstagramSessionPath`, `InstagramCollection`, `ExtractionMode`, `ExtractionThreshold float64`, `AnthropicAPIKey`, `AnthropicModel`, `ImportInterval time.Duration`) and `config.Load() (Config, error)`. All later tasks (Task 3 DB, Task 8 LLM, Task 10 Instagram, Task 13 worker, Task 18 main) consume `config.Config` by field name above — do not rename fields later.

- [x] **Step 1: Write the failing test**

```go
// internal/config/config_test.go
package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DB_DSN", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("IMPORT_INTERVAL", "")
	t.Setenv("EXTRACTION_CONFIDENCE_THRESHOLD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
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
}

func TestLoad_InvalidThreshold(t *testing.T) {
	t.Setenv("EXTRACTION_CONFIDENCE_THRESHOLD", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for invalid threshold")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v`
Expected: FAIL — `package config: config.go: no such file or directory` (or `undefined: Load`).

- [x] **Step 3: Implement**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr string
	DBDSN    string

	InstagramUsername    string
	InstagramPassword    string
	InstagramSessionPath string
	InstagramCollection  string

	ExtractionMode      string
	ExtractionThreshold float64

	AnthropicAPIKey string
	AnthropicModel  string

	ImportInterval time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:             getEnv("HTTP_ADDR", ":8080"),
		DBDSN:                getEnv("DB_DSN", "postgres://recipes:recipes@localhost:5432/recipes?sslmode=disable"),
		InstagramUsername:    os.Getenv("INSTAGRAM_USERNAME"),
		InstagramPassword:    os.Getenv("INSTAGRAM_PASSWORD"),
		InstagramSessionPath: getEnv("INSTAGRAM_SESSION_PATH", "data/instagram-session.json"),
		InstagramCollection:  os.Getenv("INSTAGRAM_COLLECTION"),
		ExtractionMode:       getEnv("EXTRACTION_MODE", "hybrid"),
		AnthropicAPIKey:      os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicModel:       getEnv("ANTHROPIC_MODEL", "claude-opus-5"),
	}

	threshold, err := strconv.ParseFloat(getEnv("EXTRACTION_CONFIDENCE_THRESHOLD", "0.6"), 64)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid EXTRACTION_CONFIDENCE_THRESHOLD: %w", err)
	}
	cfg.ExtractionThreshold = threshold

	interval, err := time.ParseDuration(getEnv("IMPORT_INTERVAL", "6h"))
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid IMPORT_INTERVAL: %w", err)
	}
	cfg.ImportInterval = interval

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/config
git commit -m "$(cat <<'EOF'
feat: add environment-based configuration loader

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 1](01-project-scaffold-tooling.md) · [Task 3 →](03-database-schema-migrations-sqlc-codegen-domain-models.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
