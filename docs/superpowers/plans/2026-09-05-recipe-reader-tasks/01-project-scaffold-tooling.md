> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 0: Bootstrap & Tooling.
>
> **Status:** [x] done

# Task 1: Project Scaffold & Tooling

**Files:**
- Create: `go.mod`, `.gitignore`, `.env.example`, `Makefile`, `.golangci.yml`, `README.md`
- Create: `internal/webui/dist/index.html` (placeholder so `go:embed` compiles before the frontend exists)
- Create: `cmd/server/main.go` (minimal — prints version and exits, filled in fully in Task 18)

**Interfaces:**
- Produces: module path `github.com/sBurmester/recipe-reader`, `Makefile` targets `build`, `test`, `lint`, `vuln`, `check`, `run`, `frontend`, `docker`, `sqlc-generate` — later tasks assume these exist.

- [x] **Step 1: Initialize the Go module**

```bash
go mod init github.com/sBurmester/recipe-reader
```

- [x] **Step 2: Create `.gitignore`**

```gitignore
/bin/
/data/
.env
node_modules/
web/dist/
internal/webui/dist/*
!internal/webui/dist/index.html
```

- [x] **Step 3: Create `.env.example`**

```dotenv
HTTP_ADDR=:8080

DB_DSN=postgres://recipes:recipes@localhost:5432/recipes?sslmode=disable

INSTAGRAM_USERNAME=
INSTAGRAM_PASSWORD=
INSTAGRAM_SESSION_PATH=data/instagram-session.json
INSTAGRAM_COLLECTION=Rezepte

EXTRACTION_MODE=hybrid
EXTRACTION_CONFIDENCE_THRESHOLD=0.6

ANTHROPIC_API_KEY=
ANTHROPIC_MODEL=claude-opus-5

IMPORT_INTERVAL=6h
```

- [x] **Step 4: Create the committed embed placeholder**

```bash
mkdir -p internal/webui/dist
```

`internal/webui/dist/index.html`:

```html
<!doctype html>
<html><head><title>Recipe Reader</title></head>
<body><p>Frontend not built yet — run <code>make frontend</code>.</p></body>
</html>
```

- [x] **Step 5: Create `.golangci.yml`**

```yaml
version: "2"
run:
  timeout: 5m
linters:
  enable:
    - govet
    - staticcheck
    - unused
    - errcheck
formatters:
  enable:
    - gofmt
    - goimports
```

golangci-lint v2 requires the `version: "2"` key and splits formatters (`gofmt`, `goimports`) out of `linters` into their own `formatters` section — the v1-style config fails with `unsupported version of the configuration` on v2. Confirmed against golangci-lint 2.13.2 while executing this task.

- [x] **Step 6: Create the `Makefile`**

```makefile
.PHONY: build test lint vuln check run docker frontend sqlc-generate db-up

frontend:
	npm --prefix web ci
	npm --prefix web run build
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -r web/dist/. internal/webui/dist/

sqlc-generate:
	sqlc generate

db-up:
	docker compose up -d db

build:
	CGO_ENABLED=0 go build -o bin/recipe-reader ./cmd/server

test:
	go test ./...

lint:
	gofmt -l . | tee /tmp/gofmt-out; test ! -s /tmp/gofmt-out
	go vet ./...
	golangci-lint run ./...

vuln:
	govulncheck ./...

check: lint vuln test

run:
	go run ./cmd/server

docker:
	docker build -t recipe-reader .
```

- [x] **Step 7: Minimal `cmd/server/main.go`**

```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Printf("recipe-reader starting (go %s)\n", runtime.Version())
}
```

- [x] **Step 8: Verify it builds**

Run: `go build ./...`
Expected: no output, exit code 0.

- [x] **Step 9: Install dev-time tools**

`sqlc` and `golangci-lint` are dev-time tools, not `go.mod` dependencies:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
# golangci-lint: follow https://golangci-lint.run/welcome/install/ for your platform
```

- [x] **Step 10: Create `README.md` skeleton**

```markdown
# Recipe Reader

Imports recipes from Instagram saved posts, extracts structured data, and serves a searchable web UI. See `docs/superpowers/plans/2026-09-05-recipe-reader-implementation.md` for the implementation plan.

## Development

    cp .env.example .env
    make db-up      # starts Postgres via docker-compose
    make frontend   # builds web/ and embeds it
    make run

## Testing

    make check   # gofmt + go vet + golangci-lint + govulncheck + go test
                 # go test spins up ephemeral Postgres containers via testcontainers-go — Docker must be running.
```

- [x] **Step 11: Commit**

```bash
git add go.mod .gitignore .env.example Makefile .golangci.yml README.md cmd/server/main.go internal/webui/dist/index.html
git commit -m "$(cat <<'EOF'
chore: scaffold Go module, tooling, and build targets

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[Task 2 →](02-configuration-package.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
