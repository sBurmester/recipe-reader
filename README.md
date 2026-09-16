# Recipe Reader

Imports recipes from an Instagram account's saved posts, extracts structured recipe data
(ingredients, instructions, categories) via a rule-based parser with an optional LLM fallback,
and serves a searchable web UI. Single Go binary with the frontend embedded; Postgres everywhere
— dev, test, and production.

## Requirements

- **Go** — latest stable, see `go.mod`
- **Node.js 26+** — for the frontend build
- **Docker + Docker Compose** — Postgres for dev and production, and `go test` starts ephemeral
  Postgres containers through testcontainers-go
- **golangci-lint** — for `make lint`
- **sqlc** — only if you change `internal/db/queries/*.sql` or `internal/db/migrations/*.sql`
  and need to regenerate `internal/db/sqlc/`. `make sqlc-generate` expects it on `PATH`; CI runs
  it through `go run` instead, so no install is needed there.

## Setup

    cp .env.example .env
    # Edit .env. Everything is optional: set INSTAGRAM_USERNAME / INSTAGRAM_PASSWORD to enable
    # importing, and ANTHROPIC_API_KEY to enable the LLM extraction fallback. Without them the
    # app still runs — it serves the API and UI over whatever is already in the database, and
    # extraction falls back to rules only.

    make db-up      # starts Postgres via docker compose
    make run        # builds the frontend, then serves on 127.0.0.1:8080

## Configuration

Configuration is handled by [kong](https://github.com/alecthomas/kong): settings can be supplied
as a command-line flag or an environment variable, with flags taking precedence over the
environment and the environment over the built-in defaults. Run `recipe-reader --help` for the
full list. See `.env.example` for the environment-variable names and their defaults.

**Credentials are environment-only.** `API_TOKEN`, `INSTAGRAM_PASSWORD`, `LLM_API_KEY` and
`ANTHROPIC_API_KEY` have no flag form: a value passed on the command line is visible to every user
on the host through `ps`, and lands in shell history. `recipe-reader --help` names them in its
description, since there is no flag entry to list them under.

**Values that parse but cannot be meant are refused at startup**, with one line rather than a stack
trace or a silent substitution:

| Setting | Rule | What accepting it used to do |
| --- | --- | --- |
| `IMPORT_INTERVAL` | must be positive | `0` panicked `time.NewTicker` on the startup goroutine |
| `IMPORT_MAX_ITEMS`, `IMPORT_MAX_PAGES` | at least 1 | `0`, plausibly meant as "pause", imported the default 50 instead |
| `EXTRACTION_CONFIDENCE_THRESHOLD` | between 0 and 1 | `80`, meaning a percentage, called the paid LLM for every post forever |
| `EXTRACTION_PUBLISH_THRESHOLD` | between 0 and 1 | `80` sent every recipe to `needs_review` |
| `HTTP_ADDR` | non-loopback needs `API_TOKEN` | see [Access control](#access-control) |

Both threshold endpoints are legal: `0` for the confidence threshold means "never fall back to the
LLM", and `1` for the publish threshold means "publish nothing unreviewed".

### Access control

The API's authorization model is "you can reach it", so what the server binds to and who may
reach it are one decision:

- **`HTTP_ADDR`** defaults to `127.0.0.1:8080` — loopback only. On that address the writes are
  left unauthenticated, which is the single-user desktop case this project is built for.
- **`API_TOKEN`** is the bearer token required on `POST`, `PUT`, `PATCH` and `DELETE`. Reads stay
  open. **The server refuses to start on a non-loopback address without one**, so a
  network-reachable deployment cannot accidentally ship with open write endpoints.
- **`CORS_ORIGINS`** is a comma-separated browser-origin allowlist, defaulting to
  `http://localhost:5173` for the Vite dev server. Only a listed origin gets an
  `Access-Control-Allow-Origin` header back; everything else is refused at preflight. The bundled
  frontend is same-origin and needs no entry — set this to empty in production unless a separately
  hosted UI calls the API.

Every write must also send `Content-Type: application/json`. That is not a body-parsing
requirement: `application/json` is not one of the CORS-"simple" content types, so requiring it is
what forces a cross-origin write to be preflighted and brought under the allowlist above.

When `API_TOKEN` is set, the bundled UI asks for the token on the first write that returns `401`
and keeps it in `localStorage`. It is never compiled into the bundle.

## HTTP API

All routes are served under `/api` and return JSON. Reads are open; the four mutating methods
require `Content-Type: application/json` and, when `API_TOKEN` is configured, an
`Authorization: Bearer <token>` header. See [Access control](#access-control).

Request bodies on `POST /api/recipes` and `PUT /api/recipes/{id}` are capped at **1 MiB**; a
larger one is answered `413` and the connection is closed. The server also bounds slow clients: the
headers must arrive within 10s and the whole request within 30s, a response must be written within
60s of the headers, and idle keep-alive connections are closed after 120s.

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/healthz` | Liveness probe — `{"status":"ok","version":"..."}`. Touches no dependency, so it reports that the process is serving, not that the database is reachable. `version` is omitted from an unstamped build. |
| `GET` | `/api/recipes` | Search and list recipes. Query params: `q` (free text), `category_id`, `status`, `page`, `page_size` — all optional; malformed numerics are ignored rather than rejected. Returns `{"recipes":[...],"total":N}`. |
| `POST` | `/api/recipes` | Create a recipe. `name` and `source` are required; `status` defaults to `published` and otherwise must be `needs_review` or `published`. A `source` that already exists is answered `409`. |
| `GET` | `/api/recipes/{id}` | Fetch one recipe. |
| `PUT` | `/api/recipes/{id}` | Replace a recipe. `name` is required and `status` must be `needs_review` or `published` — a replacement has no defaults. |
| `DELETE` | `/api/recipes/{id}` | Delete a recipe. |
| `GET` | `/api/categories` | List all categories. |
| `GET` | `/api/units` | List all units. |
| `GET` | `/api/ingredients` | List all known ingredients. |
| `POST` | `/api/import/run` | Trigger an import. Returns `202` immediately; the run happens in the background. Answers `429` with `Retry-After` while an Instagram rate-limit cooldown is in effect. |
| `GET` | `/api/import/status` | Last run's tally (`seen`/`imported`/`skipped`/`no_recipe`/`degraded`/`failed`), timestamp, whether a run is in flight, `cooldown_until`/`cooldown_seconds` while rate-limited, and `error`/`error_message` when the last run failed. |

Recipe ingredients and categories are written by name — the API resolves them to lookup rows,
creating any it hasn't seen before, so callers never deal in lookup IDs.

`degraded` counts imported recipes the rules produced after the LLM call failed. It overlaps `imported`
rather than partitioning `seen`: a run where the two are equal is a run where the LLM never worked at
all. Before this existed, that failure was invisible in both directions — the tally reported success
while quality dropped, so an expired API key read as a healthy run over weak captions.

`no_recipe` counts captions that carried no recipe. They are reported apart from `skipped` and
`failed` because a saved-posts feed legitimately contains things that are not recipes: folding
them into `failed` would make a healthy run look broken, and folding them into `skipped` would
hide how much of the feed is noise. Nothing is stored for them.

The two `/api/import/*` routes return `503 {"error":"import worker not configured"}` when no
Instagram account is configured (`INSTAGRAM_USERNAME` unset), since there is no worker to drive.
Every other route works normally in that state, serving whatever is already in the database.

A failed run is reported as a **classification, never as the error text**. `error` is one of
`rate_limited`, `instagram_auth`, `instagram_schema_drift`, `fetch_failed`, `cancelled` or
`import_failed`, and `error_message` is a fixed sentence per code; both fields are absent when the
last run succeeded. The underlying chain wraps whatever the Instagram client returned — the endpoint
it called, fragments of the upstream response, and with a database error in it, parts of the DSN —
and this endpoint has no authenticated callers to restrict that to. The full chain is logged
server-side, where it is useful. Add codes, do not rename them: the frontend branches on them.

A login that fails at startup does not disable imports. The server logs it, the import status
reports it as `instagram_auth` straight away, and the next import — scheduled or triggered — logs in
before it fetches. Login attempts are rationed to one every 15 minutes, because repeated logins are
what Instagram flags; a run triggered sooner than that reports the floor instead of trying.

Each run is bounded. `IMPORT_MAX_ITEMS` (default `50`) caps how many new posts it collects — posts
already imported do not count against it — and `IMPORT_MAX_PAGES` (default `100`) caps how many
feed pages it walks. Raise them for a one-off backfill of a long saved-posts history; both must be
at least 1.

## Extraction

`EXTRACTION_MODE` selects the engine, and each value now does what its name says:

| Mode | Behaviour |
| --- | --- |
| `rule` | The rule-based parser only. No API key needed, no LLM calls. |
| `llm` | The LLM extractor only. **Refuses to start without an API key** — asking for the LLM and configuring no key is a broken deployment, not a rules-only one. |
| `hybrid` (default) | Rules first, LLM only when the rules fall short. With no API key it runs rules-only and says so at startup. |

Until this was fixed, only `hybrid` was ever inspected: `EXTRACTION_MODE=llm` fell through to exactly the
same nil-LLM hybrid as `EXTRACTION_MODE=rule`, so asking for the LLM selected the *weakest* extractor, on
every post of every run, with a paid key sitting unused. The mode is now an enum (a typo is rejected at
startup) and the selected mode is logged at boot.

### Providers

The LLM extractor speaks two transports, selected with `LLM_PROVIDER`:

- **`anthropic`** (default) — the native Messages API with a forced `record_recipe` tool call.
- **`openai`** — any OpenAI-compatible `/chat/completions` endpoint. Point `LLM_BASE_URL` at OpenAI, Groq,
  Together, OpenRouter, Fireworks, a local Ollama on `/v1`, vLLM or LM Studio. `LLM_MODEL` is required here.

`LLM_API_KEY` / `LLM_MODEL` / `LLM_BASE_URL` configure whichever provider is selected. The older
`ANTHROPIC_API_KEY` and `ANTHROPIC_MODEL` still work and remain authoritative for the `anthropic`
provider; they are not borrowed by the `openai` one.

Both transports build their `record_recipe` schema from the same shared property set, so the two cannot
drift into asking the model for different fields.

`LLM_TIMEOUT` (default `60s`) bounds each extraction call, the SDK's own retries included. A call that
runs past it fails that one post — in `hybrid` mode the post falls back to the rules and is counted as
degraded — and the import moves on instead of waiting indefinitely. Raise it for a local model running
on CPU.

A caption longer than 8,000 characters is cut to that length before it is sent. A real Instagram caption
is at most 2,200, so this only ever trims input that did not come from an ordinary post, and it keeps
any one post from running up an unbounded input-token bill.

### Confidence

Every extraction carries a confidence between 0 and 1, and two separate thresholds act on it:

- **`EXTRACTION_CONFIDENCE_THRESHOLD`** (default `0.6`) — below this, the rule-based result is
  considered too weak and the LLM extractor is tried.
- **`EXTRACTION_PUBLISH_THRESHOLD`** (default `0.8`) — below this, a recipe is stored as
  `needs_review` rather than `published`.

They used to be one number, which made `needs_review` unreachable for any LLM result that
contained a recipe at all. The LLM's confidence is now the minimum of two signals: the model's own
estimate, reported through the `record_recipe` tool schema, and a structural score computed from
what actually came back — how many ingredients, how long the instructions are, and how many
ingredients got an amount. A confident model cannot publish a threadbare result, and a rich result
cannot talk an uncertain model up.

A caption with no recipe in it produces `extraction.ErrNoRecipe` and stores nothing. Previously
such a post became a recipe row whose instructions were the literal string `NO_RECIPE_FOUND` —
and since `source` is `UNIQUE` and the pipeline skips anything already present, that row
permanently blocked the post from being re-imported by a better extractor.

## Importing a backlog

The fetcher pages from the head of the saved feed and skips posts already in the database, so the
per-run cap counts only *new* posts. An account with more saved posts than the cap therefore
drains across successive runs. Before this, the cap counted every post the feed returned, so the
window stayed pinned to the newest 50 for the life of the account: everything older was
unreachable, and each run reported `Seen: 50, Skipped: 50, Imported: 0`, which reads as healthy.

When Instagram throttles a run, the error is recognised as a rate limit rather than an ordinary 4xx:
the posts already collected are still imported, and the worker then stands down for 30 minutes.
`POST /api/import/run` answers `429` with `Retry-After` during that window rather than accepting a run
it would silently skip. Backing off is the only defence an unofficial client has against being flagged,
and before this the next scheduled tick walked straight back into the throttle.

If the feed returns items and none of them can be read, that is reported as an error rather than an
empty success — these endpoints are undocumented, so a field rename upstream would otherwise arrive as
a run that imported nothing for no stated reason.

Each run is bounded by a page cap as well, and every call into the Instagram client is bounded in
time — the library exposes no context, no settable HTTP timeout and no transport hook, so the call
runs in a goroutine the client stops waiting for. One call is admitted at a time; a call that
arrives while an abandoned one is still outstanding fails with a clear error rather than racing
it, and recovers by itself once the abandoned call returns. An expired Instagram session is
re-established once, in place, at most once every 15 minutes — repeated logins are what Instagram
flags — and a challenge or two-factor prompt is reported as `ErrReauthRequired` without spending a
login attempt on it.

## Frontend

`web/` is a Vite + TypeScript app with no UI framework — pages build DOM nodes through a small
`el()` helper in `src/dom.ts`, which also means Instagram caption text is never parsed as HTML.

    npm --prefix web install
    npm --prefix web run dev        # dev server on :5173, proxies /api to :8080
    npm --prefix web run typecheck
    npm --prefix web run build      # emits web/dist/
    make frontend                   # build and copy into internal/webui/dist for embedding

`make build` and `make run` depend on `frontend`, so they cannot produce a binary that silently
serves the placeholder page. `npm ci` inside that target only reruns when `web/package-lock.json`
changes. The build paths that bypass the Makefile — `go build ./...`, `go install`, an IDE build —
still can produce one, so the binary logs a warning at startup when it is carrying the
placeholder. The placeholder itself lives in `internal/webui/placeholder/`, outside the directory
the frontend build overwrites, so a build cannot quietly replace it and turn that warning off.

Routing is hash-based (`#/`, `#/recipes/:id`, `#/import`), so the Go binary can serve the whole
app from one embedded directory with no server-side rewrite rules.

`src/types.ts` mirrors the Go DTOs in `internal/api/dto.go` field-for-field. Nothing enforces
that correspondence at compile time — if you change one side, change the other, or the drift
surfaces as `undefined` values in the UI.

## Docker

The frontend build is embedded into the binary with `go:embed`, so the application ships as a
single file with no assets to deploy beside it. Anything not under `/api/` is served from that
embedded directory, falling back to the app shell rather than a 404.

    API_TOKEN=$(openssl rand -hex 32) docker compose up --build   # app on :8080, Postgres on :5432
    docker compose down

The compose stack publishes port 8080, so the container binds every interface — the case the
server refuses without a token. `API_TOKEN` is therefore mandatory here, and compose fails with
that message if it is unset rather than starting something open to the network.

The image builds the frontend and the Go binary in separate stages and ships only the binary on
Alpine — about 20 MB, running as a non-root user. Credentials come from the environment (see
`docker-compose.yml`), and the Instagram session is kept in the `app-data` volume so the app does
not log in again on every restart, which Instagram rate-limits.

`make db-up` starts only the Postgres service, for running the Go binary locally against the same
database the container would use.

### Version stamping

The binary identifies itself. `make build` and `make docker` stamp `git describe --tags --always
--dirty` into `main.version` with `-ldflags`, and it is reported two ways:

    recipe-reader --version          # v0.1.0-4-g1a2b3c4-dirty
    curl -s localhost:8080/api/healthz   # {"status":"ok","version":"v0.1.0-4-g1a2b3c4"}

A plain `go build` or `go run` leaves it at `dev`, and `/api/healthz` then omits the field rather
than reporting an empty one. The `--dirty` suffix is deliberate: a build from an unclean tree is
not the commit it names.

`.dockerignore` excludes `.git`, so an image build has no history to describe itself from — the
version is passed in as a build argument instead:

    docker build --build-arg VERSION="$(git describe --tags --always --dirty)" -t recipe-reader .
    VERSION="$(git describe --tags --always --dirty)" docker compose up --build

`make docker` and CI both do this. A bare `docker build` with no `--build-arg` produces `dev`.

## Testing & linting

    make check   # gofmt + go vet + golangci-lint + govulncheck + go test -race
                 # go test starts one ephemeral Postgres container per database package
                 # via testcontainers-go — Docker must be running.

    make cover       # the twenty least-covered functions
    make cover-html  # the same profile as a browsable report

    npm --prefix web run typecheck
    npm --prefix web run build

Coverage is measured and reported, and **nothing is gated on it**. `make test` writes `cover.out`
and prints the total; `go test` prints each package's percentage as it goes, which is the per-package
ranking that says where a test is worth writing next. There is deliberately no threshold: a
percentage target produces tests written to move the number rather than to catch anything. CI prints
the total in the job summary and uploads `cover.out` as an artifact, including on a failed run.

CI (`.github/workflows/ci.yml`) runs the same checks on every pull request, a guard that fails if
the committed `internal/db/sqlc/` has drifted from the queries and migrations it was generated
from, and a `docker build` followed by a smoke test of the image:

    docker build --build-arg VERSION="$(git describe --tags --always --dirty)" -t recipe-reader:ci .
    scripts/smoke-test-image.sh recipe-reader:ci "$(git describe --tags --always --dirty)"

The script starts the image against a throwaway Postgres and fails unless `/api/healthz` answers,
`/api/recipes` answers (so the migrations ran against a real database), `/` serves the built
frontend rather than the placeholder page, and `--version` and `/api/healthz` agree on a version
that is not the unstamped `dev` — the link-time stamp is the one part of the build no unit test can
reach. The expected version is optional; without it the script only refuses `dev`. It runs the same
way locally as in CI.

## Database

The connection pool is configured rather than taken as `pgxpool` hands it over: **10** maximum
connections (above pgxpool's `max(4, NumCPU)`, because one page of search still costs `2+2N`
queries), a floor of **2** so the pool does not drain to nothing between the six-hourly imports and
leave the next request paying a full connect, and a **5s** connect timeout. Every one of these is
overridden by the DSN when it names the corresponding parameter, so they are defaults and not
decisions taken away from the operator:

    DB_DSN="postgres://recipes:recipes@localhost:5432/recipes?sslmode=disable&pool_max_conns=25&pool_min_conns=5"

`pool_max_conns`, `pool_min_conns`, `pool_max_conn_lifetime`, `pool_max_conn_idle_time`,
`pool_health_check_period` and `connect_timeout` are all read from the DSN. A `pool_max_conns` below
the floor lowers the floor with it rather than producing a configuration that refuses to start.

**Search treats `%`, `_` and `\` as literals.** They are `LIKE` metacharacters, and the search box
hands them straight to the pattern: searching for `50%` used to match every recipe and `a_b` used to
match `axb`. The escaping is done in SQL, in both `SearchRecipes` and `CountRecipes`, so the page
and its `total` cannot disagree about what matched.

**The insert is the duplicate check.** `CreateRecipe` carries `ON CONFLICT (source) DO NOTHING`, so
the unique index on `source` decides, with no window between a check and a write for a second
importer to slip through. The repository reports the conflict as `ErrDuplicateSource`; the import
pipeline counts it as `skipped`, and `POST /api/recipes` answers `409`.

## Changing the database schema

1. Add `internal/db/migrations/000N_*.up.sql` and the matching `.down.sql`.
2. Add or edit the relevant `internal/db/queries/*.sql`.
3. Run `make sqlc-generate`.
4. Commit the migration, the query, and the regenerated `internal/db/sqlc/` files together — CI
   fails if the generated code does not match its inputs.

## Architecture

See
[the implementation plan](docs/superpowers/plans/2026-09-05-recipe-reader-implementation.md)
for the full architectural rationale — persistence strategy, extraction strategy, and frontend
stack, each chosen explicitly and recorded there, along with the per-task breakdown in
`docs/superpowers/plans/2026-09-05-recipe-reader-tasks/`.

## Instagram integration caveat

The saved-posts and collection fetching in `internal/instagram/saved.go` uses Instagram's
unofficial private API through the `instago` library. Those endpoints are not documented or
supported, so they can break without notice if Instagram changes them, and using them carries
real Terms of Service risk. Treat this as personal automation against your own account for your
own recipe collection — not as a scraping service for other people's data.
