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

Configuration is handled by [kong](https://github.com/alecthomas/kong): every setting can be
supplied as a command-line flag or an environment variable, with flags taking precedence over
the environment and the environment over the built-in defaults. Run `recipe-reader --help` for
the full list. See `.env.example` for the environment-variable names and their defaults.

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

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/healthz` | Liveness probe — `{"status":"ok"}`. |
| `GET` | `/api/recipes` | Search and list recipes. Query params: `q` (free text), `category_id`, `status`, `page`, `page_size` — all optional; malformed numerics are ignored rather than rejected. Returns `{"recipes":[...],"total":N}`. |
| `POST` | `/api/recipes` | Create a recipe. `name` and `source` are required. |
| `GET` | `/api/recipes/{id}` | Fetch one recipe. |
| `PUT` | `/api/recipes/{id}` | Replace a recipe. |
| `DELETE` | `/api/recipes/{id}` | Delete a recipe. |
| `GET` | `/api/categories` | List all categories. |
| `GET` | `/api/units` | List all units. |
| `GET` | `/api/ingredients` | List all known ingredients. |
| `POST` | `/api/import/run` | Trigger an import. Returns `202` immediately; the run happens in the background. |
| `GET` | `/api/import/status` | Last run's tally (`seen`/`imported`/`skipped`/`no_recipe`/`failed`), timestamp, and whether a run is in flight. |

Recipe ingredients and categories are written by name — the API resolves them to lookup rows,
creating any it hasn't seen before, so callers never deal in lookup IDs.

`no_recipe` counts captions that carried no recipe. They are reported apart from `skipped` and
`failed` because a saved-posts feed legitimately contains things that are not recipes: folding
them into `failed` would make a healthy run look broken, and folding them into `skipped` would
hide how much of the feed is noise. Nothing is stored for them.

The two `/api/import/*` routes return `503 {"error":"import worker not configured"}` when no
Instagram credentials are set, since there is no worker to drive. Every other route works
normally in that state, serving whatever is already in the database.

## Extraction and confidence

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

## Testing & linting

    make check   # gofmt + go vet + golangci-lint + govulncheck + go test
                 # go test spins up ephemeral Postgres containers via testcontainers-go —
                 # Docker must be running.

    npm --prefix web run typecheck
    npm --prefix web run build

CI (`.github/workflows/ci.yml`) runs the same checks on every pull request, plus a `docker build`
and a guard that fails if the committed `internal/db/sqlc/` has drifted from the queries and
migrations it was generated from.

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
