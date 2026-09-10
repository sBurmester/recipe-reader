# Recipe Reader

Imports recipes from Instagram saved posts, extracts structured data, and serves a searchable web UI. See `docs/superpowers/plans/2026-09-05-recipe-reader-implementation.md` for the implementation plan.

## Development

    cp .env.example .env
    make db-up      # starts Postgres via docker-compose
    make frontend   # builds web/ and embeds it
    make run

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

## Testing

    make check   # gofmt + go vet + golangci-lint + govulncheck + go test
                 # go test spins up ephemeral Postgres containers via testcontainers-go — Docker must be running.
