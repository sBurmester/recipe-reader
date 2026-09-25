# AGENTS.md

Instructions for coding agents working in this repository. Read this before you change anything.
It gathers the decisions scattered across `README.md`, `docs/PROJECT.md`, the 2026-09-11 review
panel (`docs/reviews/`) and the plans (`docs/superpowers/plans/`). Those files keep the full
reasoning. This one tells you what holds today and what not to undo.

If this file and the code disagree, the code wins. Fix this file in the same change. When a change
fixes something listed here (a backlog entry, an open question, a known gap), delete it from this
file rather than noting that it was fixed. The commit, the plan and the code's comments keep that
history.

## What this is

A single Go binary for **one person's private recipe collection**. It imports the owner's saved
Instagram posts, extracts recipes from the captions (rules first, optional LLM fallback), stores
them in Postgres and serves a searchable, editable web UI embedded in the binary.

- Module: `github.com/sBurmester/recipe-reader`. Go: latest stable (see `go.mod`). Node 26+ for `web/`.
- Postgres **everywhere**: dev, test and prod. There is no SQLite path, and none should be added.
  sqlc compiles against one dialect.
- Released as `v0.x` via release-please: static binaries for `linux/amd64`, `linux/arm64` and
  `darwin/arm64`, plus `ghcr.io/sburmester/recipe-reader`.
- UI text is German, and captions are mostly German and full of emoji. Code, comments, README and
  commit messages are English. The plans in `docs/superpowers/plans/` since 2026-09-19 and
  `docs/PROJECT.md` are German.

## Working rules

### Git and PRs

- **Never work on `main`.** Start each session or task on a new branch named
  `<prefix>/<short-description>`, where the prefix is a Conventional Commits type (`feat`, `fix`,
  `chore`, `refactor`, `docs`, `test`, `ci`, `perf`, `build`). Multi-milestone plans use one
  branch and one PR per milestone. Each branch starts from `main` after the previous PR has merged.
- If you are fixing an **existing PR**, including a Renovate PR, push to that PR's branch. Don't
  open a new one. Then comment on the PR with what was wrong and what the fix changed.
- Commit and push only when asked.
- PR titles and commit subjects follow Conventional Commits, and **the prefix sets the version**.
  PRs are squash-merged, so the PR title becomes the commit that release-please reads:
  - `fix:` → patch release. `feat:` → minor release. `!` or a `BREAKING CHANGE:` footer →
    breaking change, which raises the **minor** while the version is below 1.0.
  - `refactor`, `perf`, `build`, `ci`, `docs`, `test` and `chore` do **not** release and are hidden
    from `CHANGELOG.md`. A breaking `refactor!:` still releases.
  - A wrong prefix means a wrong version. Check the release PR before merging it.
- Every commit message ends with:
  ```
  Assisted-by: <Model Name> (<Effort>) via <Tool Name>
  Co-Authored-By: <Model Name> <noreply@anthropic.com>
  ```
- Never commit secrets. `.env` is gitignored.

### Before every commit

Run these in this order. Docker must be running: tests start Postgres through testcontainers-go.

```bash
go fix ./...
gofmt -w .
go vet ./...
~/.local/bin/golangci-lint run ./...        # latest version; CI uses golangci-lint-action@latest
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go test -count=1 -race ./...
```

- `go fix` and `gofmt` rewrite files. Read the diff they produce.
- If you touched `web/`, also run `make frontend-check`, which runs typecheck and build.
  `make check` runs everything, frontend included.
- If you touched `Dockerfile` or `scripts/`, also run the **Docker gate**:
  ```bash
  docker build --build-arg VERSION=smoke -t recipe-reader:ci .
  scripts/smoke-test-image.sh recipe-reader:ci smoke   # expect: smoke test passed: recipe-reader:ci (smoke)
  ```
- Fix every finding before you commit. No `//nolint`, no loosening `.golangci.yml`, and no skipped
  step without the user's explicit approval. If a `govulncheck` finding has no fixed version, report
  it to the user. The known one is GO-2026-5932 in `golang.org/x/crypto/openpgp`: the code never
  calls it and no fix exists.
- Don't say a step passed unless you ran it.

### Plans, docs and config files

- If you work from a plan file, tick each step and task (`[x]`) **in that file** when it is done.
  Milestone PRs end with a `docs: mark M<n> done in the <plan>` commit.
- If a task needs something only a person has (credentials, a live account, a paid key, a GitHub
  setting), write it into the plan's or working plan's task record as **blocked on access**, with
  what it needs and who can supply it. Mentioning it only in the chat reply is not enough.
- Update documentation in the same change as the code: `README.md` for anything users or
  operators see, code comments for the reasons behind it, and the relevant plan.
- **Keep `.env.example` and `docker-compose.yml` up to date alongside the code.** Never plan
  around leaving them untouched. `.env.example` style: every variable uncommented, its default as
  the value, grouped by topic. Credentials get an empty value (`API_TOKEN=`).
- `src/types.ts` in `web/` mirrors `internal/api/dto.go` field for field, and nothing checks this.
  Change both together.

## Commands

| Task | Command |
| --- | --- |
| Postgres for local dev | `POSTGRES_PASSWORD=dev make db-up` (binds `127.0.0.1:5432`) |
| Run locally | `make run` (builds the frontend first, serves on `127.0.0.1:8080`) |
| Build binary | `make build` → `bin/recipe-reader` (stamped with `git describe --tags --always --dirty`) |
| All checks | `make check` (lint, frontend-check, vuln, test) |
| Tests + coverage | `make test`, `make cover`, `make cover-html` |
| Regenerate sqlc | `make sqlc-generate` (`go tool sqlc`, pinned in `go.mod`) |
| Frontend dev | `npm --prefix web run dev` (port 5173, proxies `/api` to 8080) |
| Image | `make docker` or `docker compose up --build` (needs `API_TOKEN` and `POSTGRES_PASSWORD`) |
| Extraction eval | `go test ./internal/extraction -run GoldSetRules -v` (free). Adding `RECIPE_READER_EVAL_LLM=1` plus a key runs `GoldSetLLM`, which costs money. |

## Architecture

```
cmd/recipe-reader/main.go   kong command tree, parse, dispatch; `var version` (stamped via -X main.version)
internal/cli                one type per command (serve, healthcheck, migrate, import); Run delegates at once
internal/config             settings as kong groups (Logging, HTTP, Database, Instagram, Extraction, LLM, Import)
internal/logging            builds the slog handler main installs after parsing
internal/server             composition root: Run (serve) and ImportOnce (import)
internal/healthcheck        the probe behind `recipe-reader healthcheck`
internal/api                net/http ServeMux routes, middleware, DTOs, handlers
internal/pipeline           fetch → extract → store; Worker (schedule, trigger, cooldown, status); failure codes
internal/extraction         Extractor interface; rules, LLM (anthropic | openai-compatible), hybrid; gold set
internal/instagram          instago wrapper: login/session, saved/collection feed, PostFetcher adapter
internal/repository         domain ⇄ sqlc mapping; RecipeRepository, LookupRepository
internal/db                 pool, goose migrations (embedded), seed, ImportLock; queries/, sqlc/ (generated), testdb/
internal/domain             plain domain structs
internal/webui              go:embed of the built frontend + SPA fallback; placeholder/ lives outside dist/
web/                        Vite + TypeScript, no framework
```

Invariants of the layering (kong plan E7/E13, CLI backlog D3):

- `main.go` holds the tree, `newParser`, `run` and `rejectMisplacedFlags`. From `internal/*` it
  imports exactly `internal/cli`, `internal/config` and `internal/logging`. `cmd/recipe-reader/`
  contains only `main.go` and its tests.
- `internal/cli` contains only commands. Each command embeds **only the config groups it reads**,
  so kong validates only those groups: `healthcheck` sees `config.Listen`, `migrate` sees
  `config.Database`, and `import` sees Database, Instagram, Extraction, LLM and `ImportLimits`, but
  not the interval.
- Small interfaces live at the consumer: `pipeline.PostFetcher`, `pipeline.CategoryLister`, and
  `pipeline.ImportLock` (adapter `db.ImportLock`). A nil `Lock` means unlocked, so the zero value
  of `Pipeline` stays usable.
- `context.Context` is the first parameter and is never stored in a struct. Every goroutine has an
  owner, a cancellation path and something that waits for it.
- Logging goes through the **slog default** everywhere (`slog.Info(...)`). Don't migrate to an
  injected `*slog.Logger` (CLI backlog non-goal).

## Go conventions for this repo

- **sqlc, not an ORM.** SQL lives in `internal/db/queries/*.sql`. The generated
  `internal/db/sqlc/` is committed, and CI fails if it drifts from the queries and migrations. No
  GORM and no hand-written scanning for anything sqlc can generate.
- **Standard library first.** A new dependency needs a one-line justification and the user's
  approval. Prefer `golang.org/x/...`. Every added GitHub Action also needs a justification.
- Use modern idioms: `errors.AsType`, `for range`, `slices`/`maps`, and so on. Check with
  `go doc` whether an API exists in the installed toolchain instead of recalling it.
- Naming: a function that takes a special parameter is `…WithX` (`MigrateWithContext`, not
  `MigrateContext`). Options are `WithX`.
- Errors: wrap with `fmt.Errorf("doing x: %w", err)`, use sentinels such as `ErrDuplicateSource`,
  `ErrNoRecipe`, `ErrRateLimited`, `ErrSchemaDrift`, `ErrReauthRequired`, `ErrFetch`, `ErrLogin`
  and `ErrImportInProgress`, and inspect them with `errors.Is` and `errors.AsType`.
- **Comments explain *why*, not *what*.** This codebase's comments record the defect or decision
  behind the code ("X used to do Y, which caused Z"). Keep that density and tone. **When you move
  code, keep its comments word for word.** Lines stay around 100 columns.
- Exported identifiers get doc comments that start with the identifier's name.

### Tests

- Every package that touches the database has `func TestMain(m *testing.M) { testdb.Main(m) }`,
  which gives it **one shared Postgres container per package**. `testdb.New(t)` resets all tables
  (except `goose_db_version`) and restarts the sequences. Consequences: **DB tests must not call
  `t.Parallel()`**, and calling `New` a second time wipes what the test wrote.
- `testdb.NewDatabase(t, name)` creates an empty, unmigrated database on the shared container and
  returns its DSN. It is for migration tests that stop at an earlier version.
- A test for a fix must be **shown to fail with the fix reverted**.
- Coverage is measured and reported but **never gated**. Don't add a threshold (review D6).
- The suite is hermetic. The Instagram login test makes no network call: instago returns
  `BadCredentials` on empty credentials before it sends anything (consensus correction 1).
  LLM providers are tested against `httptest` stubs.

## Decisions by area

Each of these was decided on purpose. Don't reverse one without the user's approval. Tags such as
`S1` or `T-07` refer to the review, the working plan or a plan's decision table.

### CLI and configuration

- Commands are `serve` (default), `healthcheck`, `migrate` and `import`. `serve` is
  `default:"withargs"`, so `recipe-reader` and `recipe-reader --http-addr …` keep working (E1).
- `--version` is a global `kong.VersionFlag` with no env tag (E3).
- A flag placed **before** another command's name is refused by `rejectMisplacedFlags` (E11).
  `--log-level` and `--log-format` are the exceptions: they sit on the root and apply to every
  command.
- Exit code is `0` on success and **`1` on every failure**, including parse errors. The container
  `HEALTHCHECK` only understands 0 and 1, which is why `kong.Parse` and `FatalIfErrorf` aren't used
  (E6). `main` logs `invalid command line` for a `*kong.ParseError` and `fatal` for anything else.
- Credentials are **environment-only**, with no flag form (`kong:"-"`): `API_TOKEN`,
  `INSTAGRAM_PASSWORD`, `LLM_API_KEY` and `ANTHROPIC_API_KEY`. The `--help` descriptions and the
  group descriptions name them (T-21, E12). They are read in `BeforeApply` hooks, not `AfterApply`,
  because kong runs `Validate()` before `AfterApply` (E4).
- Validation happens in per-group `Validate()` methods, which kong runs only for groups on the
  selected command's path. Nonsense values are refused at startup with a single line:
  `IMPORT_INTERVAL > 0`, `IMPORT_MAX_ITEMS/PAGES ≥ 1`, both thresholds within `[0,1]` with the
  endpoints allowed (a NaN is rejected too), a non-loopback `HTTP_ADDR` requires `API_TOKEN`,
  `EXTRACTION_MODE` is an enum, and `llm` mode without a key is refused (T-34, T-23).
  **Not enforced on purpose:** that the publish threshold is ≥ the confidence threshold.
- Logging is configured **after** `Parse` rather than in a hook (D4). A rejected command line is
  therefore logged in the default text format.
- Env names, flag names and defaults are a compatibility contract. New flags are additive and use
  `name:`, `env:`, `default:` and `help:`. There is no `kong.DefaultEnvars` prefix and no config file.
- Open question, left to the user: whether kong's command-path prefix in error messages
  (`serve: config: API_TOKEN …`) helps operators.

### HTTP API and security

- **Reads are open. Writes (`POST`, `PUT`, `PATCH`, `DELETE`) need `Authorization: Bearer
  $API_TOKEN`** when a token is set. The default bind is `127.0.0.1:8080`, and the server
  **refuses to start on a non-loopback address without a token** (S1, T-02). The authorization
  model is "whoever can reach the port", so a reachable instance lets anyone read the collection.
  For private use, keep it on loopback, behind a VPN, or behind an authenticating proxy
  (PROJECT.md §6.1).
- `CORS_ORIGINS` is an allowlist with no `*`. Writes must send `Content-Type: application/json` so
  that a cross-origin write is always preflighted (consensus correction 7).
- Bodies on recipe create and replace are capped at 1 MiB (413, connection closed). Server timeouts:
  headers 10s, request 30s, write 60s, idle 120s (S3, T-25). `DisallowUnknownFields` was
  **deliberately not** added: it would change the API contract, which is a separate decision from
  hardening.
- A 500 logs its cause server-side, and the client gets only a generic message. Recovery sits
  *inside* logging, so a panicking request still gets its log line with status 500.
  `http.ErrAbortHandler` is re-panicked (T-58). A successful `/api/healthz` is logged at debug.
- `/api/healthz` touches no dependency. It reports liveness and `version`, which is omitted on a
  `dev` build.
- Import failures are reported as **stable codes, never error text**: `rate_limited`,
  `instagram_auth`, `instagram_schema_drift`, `fetch_failed`, `cancelled` and `import_failed`, each
  with a fixed sentence. **Add codes, never rename them**, because the frontend (`IMPORT_ERROR_TEXT`)
  branches on them. The classification lives in `pipeline.Classify`. The handler deliberately
  doesn't log, because the endpoint is polled every 2s (S4, T-30).
- `/api/import/*` returns 503 when Instagram isn't configured. `POST /api/import/run` returns 202,
  or 429 with `Retry-After` during a rate-limit cooldown.
- Recipe status is `needs_review` or `published`. It is validated in the handler (POST defaults to
  `published`, PUT requires it) **and** enforced by a `CHECK` constraint (migration `0002`). It's
  both halves or it isn't done (ruling R1).
- `POST /api/recipes` with an existing `source` returns **409**.
- Ingredients and categories are written **by name**. The API resolves them to lookup rows, so
  clients never handle lookup IDs.
- Frontend headers: nosniff, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, and a strict
  CSP (`default-src 'self'; img-src 'self' data:; …; frame-ancestors 'none'`). A test fails if
  `index.html` gains inline script, inline style or an event handler (T-48).
- `image_url` is stored but **never displayed**, and the CSP blocks external images. Showing them
  would make every page view hit Instagram's CDN. Changing that is a deliberate decision to take
  in `img-src` (PROJECT.md §6.1).
- The UI builds DOM through `el()` in `web/src/dom.ts`. Never parse caption text as HTML. The API
  token lives in `localStorage` and is requested on the first 401. It is never in the bundle.

### Import and Instagram

- The Instagram API is **unofficial** (`instago` v1.0.2, private endpoints). This is personal
  automation against the owner's own account, **not** a scraper for other people's data, and it
  carries Terms of Service risk.
- **The endpoints have never been verified against a live account** (T-43, blocked on access).
  The response is decoded into named types. A page that doesn't decode returns `ErrSchemaDrift`,
  undecodable items are counted with a reason, and "items returned but none readable" is an error,
  not an empty success (T-46, T-15).
- **No cursor is persisted** (T-01). `IMPORT_MAX_ITEMS` counts only *new* posts:
  `FetchOptions.Known` (advisory, and "not known" on any error) pages past posts that are already
  imported, so a backlog drains over several runs. `IMPORT_MAX_PAGES` bounds the walk. A persisted
  cursor was rejected because there is no settings table, and resuming mid-feed would miss newly
  saved posts at the head.
- instago has no context, no HTTP timeout and no transport hook (consensus correction 5). Each
  call runs in a goroutine the client stops waiting for, and only one call is admitted at a time.
  A timeout must exceed 60s, because instago sleeps 60s on a 408 (ruling R5). Don't try to thread
  `ctx` into `PrivateRequest`: that isn't possible.
- **Logins are rationed to one every 15 minutes.** Repeated logins are what Instagram flags. An
  expired session is re-established once, in place. A challenge or 2FA prompt returns
  `ErrReauthRequired` without spending a login (T-06). A failed login at `serve` startup doesn't
  disable imports: the status shows `instagram_auth` and the next run logs in. For `import`, a
  failed login fails the run (D7).
- On a rate limit (`ErrRateLimited`), the posts already collected are still imported, and then the
  worker stands down for **30 minutes** (T-12).
- The resolved collection id is cached for 24h. It is dropped on any failed fetch except a rate
  limit, an auth failure or a context error (T-56).
- **Only one import runs per database**, enforced by a session-scoped Postgres advisory lock (key
  `8_233_071_001`). The worker and `recipe-reader import` both take it (D1). `ImportOnce` takes it
  **before logging in**, because the login is the cost the lock exists to avoid. The lock holds
  **its own `pgx.Connect` connection** outside the pool, so `pool.Close()` never waits for an
  abandoned import (E3). A worker that gets refused keeps reporting the last real tally.
- `serve` shutdown: HTTP drain plus import wait, with a 10s budget. After that an import is
  abandoned with a log line. `server.Run` works on its own cancellable context and waits for its
  goroutines even on a failed start.
- Tally fields are `seen`, `imported`, `skipped`, `no_recipe`, `failed` and `degraded`.
  `no_recipe` is separate from `skipped` and `failed`. `degraded` overlaps `imported` (the LLM
  failed and the rules result was used).
- **Retention:** nothing is deleted automatically, and the import only adds. `source` (the
  permalink) is the attribution and the dedupe key, and it is kept when a recipe is edited
  (PROJECT.md §6.1). Sharing or publishing the collection would first require showing the source,
  a retention rule, authenticated reads and a legal check.

### Extraction

- `EXTRACTION_MODE`: `rule` uses only the rules. `llm` uses only the LLM and **refuses to start
  without a key**. `hybrid` (the default) runs the rules first and falls back to the LLM below
  `EXTRACTION_CONFIDENCE_THRESHOLD` (0.6), and runs rules-only, saying so at startup, when there
  is no key. The selected mode is logged. The extractor is built **before** the database migrates,
  so a misconfiguration isn't buried (T-23).
- Providers: `anthropic` (the default; Messages API with a forced `record_recipe` tool call) or
  `openai`, meaning any OpenAI-compatible `/chat/completions` endpoint, which requires `LLM_MODEL`.
  Both build their tool schema from **one shared property set**. `ANTHROPIC_API_KEY` and
  `ANTHROPIC_MODEL` stay authoritative for the anthropic provider only.
- `LLM_TIMEOUT` (60s) bounds each call, retries included. Captions are cut to 8,000 runes before
  they are sent.
- **Two thresholds:** the confidence threshold decides the LLM fallback, and
  `EXTRACTION_PUBLISH_THRESHOLD` (0.8) decides `needs_review` versus `published` (E2, T-04).
- LLM confidence = min(the model's self-report, a structural score). The rules confidence is
  continuous, but the pinned boundaries must hold: nothing extracted → exactly `0`, one section
  alone → at most `0.5`, a clean full caption → `1.0`, and no usable title → ×0.75 (T-40).
- No recipe → `extraction.ErrNoRecipe`, and **nothing is stored**. Never write sentinel rows. In
  hybrid mode, an LLM `ErrNoRecipe` is final and never falls back to the rules result. An LLM
  *failure* does fall back, and the result is marked `Degraded` and logged (E1, E8/T-15).
- **Prompt injection:** the caption is attacker-controlled. It is wrapped in a `<caption>` span
  declared as data, with any `<caption>`/`</caption>` in it neutralised, and the confidence field
  is named as part of that data. `ToolChoice` is pinned to `record_recipe` and the schema is
  closed. Imported categories are **limited to the vocabulary already in the DB** (the seed plus
  anything a human created). Ingredient and unit names are collapsed to one line and truncated
  (T-32).
- The rules extractor derives categories only from the title, the lines before the first section
  header, and hashtags, using a short keyword list tied by a test to the **nine seeded
  categories**. Ingredient and instruction text is never read for categories. A missing category
  beats a wrong one.
- Title: the rules scan down to the first section header, skip hashtag blocks and long hooks, and
  strip leading emoji and trailing hashtags. The fallback is `Unbenanntes Rezept`.
- **Measure extraction changes against the gold set** (`internal/extraction/testdata/gold/`). Read
  its README first. The corpus is **synthetic** until T-43 runs, and the floors are a regression
  signal, not a target. Two misses are recorded gaps on purpose: amount ranges (`2-3 EL`) and
  `Würfel`.
- The default model (`ANTHROPIC_MODEL=claude-opus-5`) is **not** to be switched by guesswork.
  T-24 (default to Haiku 4.5) is blocked until someone with a key runs `GoldSetLLM` per candidate.
  When it lands, three places change.

### Persistence and migrations

- Migrations run under **goose** (`pressly/goose/v3`, embedded in the binary; E12). Bookkeeping
  is in `goose_db_version`. `PostgresSessionLocker` makes concurrent starts queue up (E16).
  Migrations run over their own `*sql.DB` (`pgx/v5/stdlib`), separate from the pool (E17). goose
  logs through slog, with per-statement lines at debug only.
- **A new migration is one file**, `internal/db/migrations/000N_<name>.sql`, with `-- +goose Up`
  and `-- +goose Down` and **no `BEGIN;`/`COMMIT;`**, because goose wraps each migration in a
  transaction. Use `-- +goose NO TRANSACTION` only for statements like `CREATE INDEX CONCURRENTLY`.
  **No comment may contain `+goose`:** goose takes every such line for an annotation, obeys a
  valid one (a commented-out example still applies) and refuses the file over anything else.
  Never renumber or edit an applied migration.
- A migration change means: the migration plus the query edits, then `make sqlc-generate`, then
  one commit with everything including the regenerated `internal/db/sqlc/`. sqlc reads the same
  files and stops at `-- +goose Down`.
- `TestMigrations_UpDownUp` exercises every Down section automatically. A migration that
  **transforms existing rows** needs its own test that stops at the previous version, writes the
  rows and migrates over them, like `0002` and `0003` (T-54).
- `serve`, `migrate` and `import` all migrate and seed on start. Cancelling a migration rolls back
  the one in flight, and re-running is safe. `db.MigrateDown` exists only for tests.
- **The insert is the dedupe:** `CreateRecipe` uses `ON CONFLICT (source) DO NOTHING` and maps to
  `ErrDuplicateSource`, which the pipeline counts as `skipped` (T-07).
- Lookup `FindOrCreate*` reads before it writes: a CTE reads, and the insert runs only on a miss,
  with `DO UPDATE` kept for the race. Lookups run **inside the recipe transaction** (T-08, T-60).
- **A recipe may list the same ingredient twice.** `(recipe_id, position)` is unique, and
  `(recipe_id, ingredient_id)` is not (T-55, migration `0003`).
- Search escapes `%`, `_` and `\` **in SQL**, in both `SearchRecipes` and `CountRecipes`, so the
  page and `total` agree (T-31). Count and page are two statements, not one snapshot. That was
  accepted (T-62).
- Pool defaults: max 10, min 2, connect timeout 5s. Any `pool_*` or `connect_timeout` in the DSN
  wins (T-37).
- The DB is Postgres 18 (`postgres:18-alpine`, pinned by digest in compose; tests use it
  unpinned). The volume is mounted at `/var/lib/postgresql`. Postgres majors are excluded from
  Renovate because they need a dump and restore.

### Frontend and embedding

- Vite + TypeScript, **no UI framework**. Routing is hash-based (`#/`, `#/recipes/:id`,
  `#/import`), so the binary needs no rewrite rules. Everything outside `/api/` falls back to the
  app shell.
- `make build` and `make run` depend on `frontend`. A build that bypasses the Makefile embeds the
  placeholder, and the binary logs a warning about it (`webui.IsPlaceholder`). The placeholder
  lives in `internal/webui/placeholder/`, outside `dist/`, on purpose (review D1, T-05).
- There is no create form in the UI. `createRecipe` in `web/src/api.ts` has no caller.

### Delivery, CI, releases and dependencies

- CI (`.github/workflows/ci.yml`) runs sqlc drift, gofmt, vet, golangci-lint, govulncheck and
  `go test -race` with coverage, plus frontend typecheck and build, plus `docker build` and
  `scripts/smoke-test-image.sh`. The smoke test checks healthz, the image `HEALTHCHECK` status, a
  DB route, a real frontend (not the placeholder), and a stamped version matching `--version`.
- **CI skips runs where every changed file is `**.md`, `docs/**`, `.github/workflows/**` or
  `.release-please-manifest.json`.** Workflow-only changes, including Renovate action updates,
  must be run by hand: `gh workflow run ci.yml --ref <branch>`. The manifest is ignored so that the
  release PR starts no run. GitHub would hold that run for approval, because the PR is opened with
  `GITHUB_TOKEN`.
- Every GitHub Action and base image is **pinned by digest with a readable tag comment**
  (`uses: actions/checkout@<sha> # v7`, `image@sha256:… `). Pin anything you add. Renovate raises
  digests. Don't pin by hand in bulk (E10).
- Renovate runs as the Mend GitHub app with `renovate.json`: Monday before 6am (Europe/Berlin), one
  grouped PR per ecosystem plus a separate PR for majors, and security fixes at any time via OSV.
  Go and npm updates are `fix:` (they release). Pins and actions are `chore:` (they don't).
- Releases: release-please (`release-please.yml`) keeps a release PR open. Merging it creates a
  **draft** release. `release-artifacts.yml` attaches the binaries and `checksums.txt`, pushes the
  image (tag + `latest`, `linux/amd64` only) after the smoke test, then publishes. Releases are
  **immutable**. If a run fails, finish it with `gh workflow run release-artifacts.yml -f tag=vX.Y.Z`.
- The compose `app` service has both `image:` and `build:` (E6), `env_file: .env` (E4), and a
  mandatory `API_TOKEN` and `POSTGRES_PASSWORD` with no defaults. Postgres is published only on
  `127.0.0.1`. The container DSN is built from `POSTGRES_*`, and `CONTAINER_DB_DSN` overrides it.
  It is deliberately **not** `DB_DSN`, which in `.env` points a host binary at `localhost`.
- The image is a multi-stage build that runs Alpine as a non-root user (uid 10001). Its
  `HEALTHCHECK` is `recipe-reader healthcheck`, because there's no curl or wget. Compose inherits
  it instead of restating it (T-50). `CMD ["serve"]`. `import` and `migrate` run via
  `docker compose run --rm app <cmd>` (D8).
- Build flags: `CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=$VERSION"`. `VERSION`
  is a build arg for the image because `.dockerignore` excludes `.git`.
- Tool versions: sqlc is pinned through `go.mod` (`go tool sqlc`) because it produces committed
  output. golangci-lint and govulncheck stay on latest on purpose.

## Open work and backlog

These are blocked on access, not on work. Don't try to finish them from the repository:

- **T-43**: verify the Instagram endpoints against a real account and commit the response as a
  `testdata/` fixture. It needs the owner's credentials and a real collection. It would settle
  I1's magnitude (the panel's only open dissent), the gold set's provenance and T-46's
  optional-field guesses.
- **T-24**: choose the default LLM model from `GoldSetLLM` measurements. It needs an LLM API key.

Backlog, deliberately not scheduled:

- kong's command-path prefix in error messages needs a judgement from the user.
- A multi-arch image (`linux/arm64`), and signing or provenance for release artifacts.

## Where the full reasoning lives

| Topic | File |
| --- | --- |
| Operator-facing behaviour, API and config | `README.md` |
| Purpose, data retention, provenance (German) | `docs/PROJECT.md` (§6.1) |
| Review panel: findings, ratings, rulings, corrections | `docs/reviews/README.md` → `2026-09-11-consensus.md` |
| Remediation of the 62 review tasks (T-xx) | `docs/reviews/2026-09-11-working-plan.md` |
| Original architecture and task breakdown | `docs/superpowers/plans/2026-09-05-recipe-reader-implementation.md` + `…-tasks/` |
| kong command tree (E1–E13) | `docs/superpowers/plans/2026-09-19-kong-cli-commands.md` |
| Drain, log flags, `import`, import lock (D1–D8) | `docs/superpowers/plans/2026-09-21-cli-backlog.md` |
| Lock connection, env_file, errors, releases, Renovate, goose (E1–E18) | `docs/superpowers/plans/2026-09-22-release-and-cleanup.md` |
| Extraction gold set | `internal/extraction/testdata/gold/README.md` |

Before acting on a review finding, read the **consensus record**. Nine corrections there overturn
claims in the individual reviews. For example, the login test is hermetic, `ctx` can't be threaded
into instago, and junk rows don't create orphan ingredients.
