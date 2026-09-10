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
    make frontend   # builds web/ and copies it into internal/webui/dist for embedding
    make run        # serves on :8080

## Configuration

Configuration is handled by [kong](https://github.com/alecthomas/kong): every setting can be
supplied as a command-line flag or an environment variable, with flags taking precedence over
the environment and the environment over the built-in defaults. Run `recipe-reader --help` for
the full list. See `.env.example` for the environment-variable names and their defaults.

## HTTP API

All routes are served under `/api`, return JSON, and carry permissive CORS headers so the
Vite dev server can call them directly.

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
| `GET` | `/api/import/status` | Last run's tally (`seen`/`imported`/`skipped`/`failed`), timestamp, and whether a run is in flight. |

Recipe ingredients and categories are written by name — the API resolves them to lookup rows,
creating any it hasn't seen before, so callers never deal in lookup IDs.

The two `/api/import/*` routes return `503 {"error":"import worker not configured"}` when no
Instagram credentials are set, since there is no worker to drive. Every other route works
normally in that state, serving whatever is already in the database.

## Frontend

`web/` is a Vite + TypeScript app with no UI framework — pages build DOM nodes through a small
`el()` helper in `src/dom.ts`, which also means Instagram caption text is never parsed as HTML.

    npm --prefix web install
    npm --prefix web run dev        # dev server on :5173, proxies /api to :8080
    npm --prefix web run typecheck
    npm --prefix web run build      # emits web/dist/
    make frontend                   # build and copy into internal/webui/dist for embedding

Routing is hash-based (`#/`, `#/recipes/:id`, `#/import`), so the Go binary can serve the whole
app from one embedded directory with no server-side rewrite rules.

`src/types.ts` mirrors the Go DTOs in `internal/api/dto.go` field-for-field. Nothing enforces
that correspondence at compile time — if you change one side, change the other, or the drift
surfaces as `undefined` values in the UI.

## Docker

The frontend build is embedded into the binary with `go:embed`, so the application ships as a
single file with no assets to deploy beside it. Anything not under `/api/` is served from that
embedded directory, falling back to the app shell rather than a 404.

    docker compose up --build   # app on :8080, Postgres on :5432
    docker compose down

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
