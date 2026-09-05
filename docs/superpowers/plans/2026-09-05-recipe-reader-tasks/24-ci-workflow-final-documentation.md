> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 7: Packaging & CI.
>
> **Status:** [ ] not started

# Task 24: CI Workflow & Final Documentation

**Files:**
- Create: `.github/workflows/ci.yml`
- Modify: `README.md` (replace the Task 1 skeleton with full setup/usage docs)

**Interfaces:**
- Consumes: `Makefile` targets `check`, `frontend`, `build`, `sqlc-generate` (Task 1); `npm run typecheck`/`build` (Task 19/22).
- Produces: nothing further consumes this — it is the last task in the plan.

- [ ] **Step 1: Create the CI workflow**

```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  backend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.27"
      - name: sqlc generated code is up to date
        run: |
          go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
          git diff --exit-code -- internal/db/sqlc || \
            (echo "::error::internal/db/sqlc is stale — run 'sqlc generate' and commit the result" && exit 1)
      - name: gofmt
        run: |
          out=$(gofmt -l .)
          if [ -n "$out" ]; then echo "$out"; exit 1; fi
      - name: go vet
        run: go vet ./...
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest
      - name: govulncheck
        run: go run golang.org/x/vuln/cmd/govulncheck@latest ./...
      - name: go test
        run: go test ./... -race
        # internal/db, internal/repository, internal/pipeline, and internal/api
        # tests start ephemeral Postgres containers via testcontainers-go —
        # ubuntu-latest runners have Docker available natively, no services:
        # block needed.

  frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: "26"
          cache: "npm"
          cache-dependency-path: web/package-lock.json
      - run: npm ci
        working-directory: web
      - run: npm run typecheck
        working-directory: web
      - run: npm run build
        working-directory: web

  docker:
    runs-on: ubuntu-latest
    needs: [backend, frontend]
    steps:
      - uses: actions/checkout@v4
      - name: Build image
        run: docker build -t recipe-reader:ci .
```

Check the current Go/Node minor versions at execution time (`go version`, `node --version`) and update the `go-version`/`node-version` fields to match — this workflow was written against Go 1.27 and Node 26, current at plan-writing time.

- [ ] **Step 2: Verify the workflow's steps locally**

```bash
sqlc generate && git diff --exit-code -- internal/db/sqlc
make check
npm --prefix web run typecheck
npm --prefix web run build
docker build -t recipe-reader:ci .
```

Expected: every command succeeds (`make check` = gofmt + go vet + golangci-lint + govulncheck + go test, per Task 1's Makefile; the `go test` leg needs Docker running for the testcontainers-backed suites).

- [ ] **Step 3: Finalize `README.md`**

```markdown
# Recipe Reader

Imports recipes from an Instagram account's saved posts, extracts structured
recipe data (ingredients, instructions, categories) via a rule-based parser
with an optional LLM fallback, and serves a searchable web UI. Single Go
binary; Postgres everywhere (dev, test, prod).

## Requirements

- Go (latest stable — see `go.mod`)
- Node.js 26+ (for the frontend build)
- Docker + Docker Compose (Postgres for dev/prod, and testcontainers-go
  spins up ephemeral Postgres containers for `go test`)
- `sqlc` (only needed if you change `internal/db/queries/*.sql` or
  `internal/db/migrations/*.sql` and need to regenerate `internal/db/sqlc/`)

## Setup

    cp .env.example .env
    # edit .env: at minimum set INSTAGRAM_USERNAME/PASSWORD to enable
    # importing, and ANTHROPIC_API_KEY to enable the LLM extraction
    # fallback. Both are optional — the app runs without them.

    make db-up      # starts Postgres via docker-compose
    make frontend   # builds web/ and embeds it into internal/webui/dist
    make run        # starts the server on :8080

## Docker (full stack)

    docker compose up --build

## Testing & Linting

    make check      # gofmt + go vet + golangci-lint + govulncheck + go test
                     # (go test needs Docker running — see Requirements)
    npm --prefix web run typecheck
    npm --prefix web run build

## Changing the database schema

    # 1. Add internal/db/migrations/000N_*.up.sql / .down.sql
    # 2. Add/edit internal/db/queries/*.sql
    make sqlc-generate
    # 3. Commit the migration, query, and regenerated internal/db/sqlc/ files together

## Architecture

See `docs/superpowers/plans/2026-09-05-recipe-reader-implementation.md` for
the full implementation plan and architectural rationale (persistence
strategy, extraction strategy, frontend stack — all chosen explicitly per
project decisions recorded there).

## Instagram integration caveat

The saved-posts/collection fetch (`internal/instagram/saved.go`) uses
Instagram's unofficial private API via the `instago` library. This can break
without notice if Instagram changes its API, and carries real Terms of
Service risk — use it against your own account for personal recipe
collection, not as a scraping service.
```

- [ ] **Step 4: Final full-repo verification**

```bash
make check
make db-up
make frontend
make build
go run ./cmd/recipe-reader &
sleep 1
curl -sf localhost:8080/api/healthz
curl -sf localhost:8080/
kill %1
```

Expected: every command exits 0; both `curl` calls return valid responses. This is the final acceptance check for the whole plan — if it passes, the app builds, tests pass, lints clean, and serves both the API and the embedded frontend from one binary against Postgres.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml README.md
git commit -m "$(cat <<'EOF'
chore: add CI workflow and finalize README

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 23](23-single-binary-packaging-go-embed-dockerfile-docker-compose.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
