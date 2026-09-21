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
- **sqlc** — no install needed. It is a pinned tool dependency in `go.mod`, so
  `make sqlc-generate` and CI both run `go tool sqlc` at exactly the same version. Bump it with
  `go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc@latest`, which shows up as a commit.

## Setup

    cp .env.example .env
    # Edit .env. Everything is optional: set INSTAGRAM_USERNAME / INSTAGRAM_PASSWORD to enable
    # importing, and ANTHROPIC_API_KEY to enable the LLM extraction fallback. Without them the
    # app still runs — it serves the API and UI over whatever is already in the database, and
    # extraction falls back to rules only.

    POSTGRES_PASSWORD=dev make db-up   # Postgres on 127.0.0.1:5432 via docker compose
    make run                           # builds the frontend, then serves on 127.0.0.1:8080

`POSTGRES_PASSWORD` has no default and compose refuses to start without it. Put the same password
in your `.env` `DB_DSN` so the locally run binary can reach the container.

## Commands

The binary is a small [kong](https://github.com/alecthomas/kong) command tree. `serve` is the
default: it runs when no command is named and takes its flags without one, so `recipe-reader` and
`recipe-reader --http-addr 127.0.0.1:9090` start the server exactly as they always have.

| Command | What it does |
| --- | --- |
| `serve` (default) | Runs the HTTP API, the embedded frontend and the background import worker. Takes every setting under [Configuration](#configuration). |
| `healthcheck` | Probes the server already listening on `HTTP_ADDR` and exits 0 if `/api/healthz` answers ok, 1 otherwise. Reads `HTTP_ADDR` and nothing else. |
| `migrate` | Applies pending migrations and seeds the lookup tables, then exits: ahead of a rollout, or after a restore. `serve` does the same at every start. Reads `DB_DSN` and nothing else. With compose: `docker compose run --rm app migrate`. Ctrl-C or `SIGTERM` stops it between two migrations — the one in flight always finishes — and it then exits 1 reporting the cancellation rather than success. Re-running is safe and picks up where it left off. |

`recipe-reader --help` lists the commands; `recipe-reader <command> --help` lists a command's flags
and the environment variable behind each. `--version` works with or without a command. Flags go
after the command name: `recipe-reader healthcheck --http-addr 127.0.0.1:9090`. Because `serve`
takes its flags unnamed, a flag in front of another command would otherwise be read as a `serve`
flag and silently dropped, so it is refused instead. Every failure, a rejected command line as
much as a failed command, exits 1.

`healthcheck` replaces the `--health-check` flag of earlier versions, which is now refused as an
unknown flag. The image's own `HEALTHCHECK` ships in the same image as the binary and was switched
with it; only a probe configured outside the image, such as an orchestrator's exec probe, needs
updating.

## Configuration

Configuration is handled by [kong](https://github.com/alecthomas/kong): settings can be supplied as
a command-line flag or an environment variable, with flags taking precedence over the environment
and the environment over the built-in defaults. Run `recipe-reader serve --help` for the full list,
grouped as HTTP, Database, Instagram, Extraction, LLM, Import and Logging.
See `.env.example` for the environment-variable names and their defaults.

**Credentials are environment-only.** `API_TOKEN`, `INSTAGRAM_PASSWORD`, `LLM_API_KEY` and
`ANTHROPIC_API_KEY` have no flag form: a value passed on the command line is visible to every user
on the host through `ps`, and lands in shell history. `recipe-reader --help` names them in its
description, and `recipe-reader serve --help` in the description of the group each belongs to,
since there is no flag entry to list them under.

**Logging is configured for every command.** The two settings sit on the root of the command tree
rather than on `serve`, so they apply to `healthcheck` and `migrate` too, and they are the one kind
of flag that may be given *before* the command name: `recipe-reader --log-level debug migrate` and
`recipe-reader migrate --log-level debug` are the same command. Logs go to stderr.

| Setting | Flag | Default | Values |
| --- | --- | --- | --- |
| `LOG_LEVEL` | `--log-level` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `--log-format` | `text` | `text` for a human, `json` for a log collector |

The handler is installed once the command line has been parsed, since that is when the two values
are known. A command line that is itself rejected is therefore reported in the default text format —
one line, and then the process is over.

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

Every request is logged once, after the handler has run, with its method, path, **status** and
duration — including a request whose handler panicked, which is logged with the `500` it was
answered with. A `500` also logs its cause on the line before it; the client only ever sees the
generic message, since the cause carries SQL and driver detail. A successful `GET /api/healthz` is
logged at debug level, below the default, so the container's health check does not add a line
every 30 seconds; a failing one is logged like any other request.

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

The rule-based score is continuous too. It used to return only `0`, `0.5` or `1.0`, which
collapsed both float thresholds to three behaviours — every setting in `(0.5, 1.0]` meant the same
thing, so tuning one from `0.6` to `0.9` changed nothing until it crossed an invisible cliff. It
now weighs how many ingredients were found, what fraction of the ingredient section's lines
actually parsed, how many of them carried an amount, and how long the instructions are. The
boundaries callers were tuned against still hold: no ingredients and no instructions is exactly
`0`, one section alone cannot exceed `0.5`, and a clean full caption still reaches `1.0`. A
caption that offered no usable title scores three quarters of what it otherwise would, because a
nameless caption is weak evidence of a recipe.

A caption with no recipe in it produces `extraction.ErrNoRecipe` and stores nothing. Previously
such a post became a recipe row whose instructions were the literal string `NO_RECIPE_FOUND` —
and since `source` is `UNIQUE` and the pipeline skips anything already present, that row
permanently blocked the post from being re-imported by a better extractor.

### Titles

The recipe name is what the search filter runs against, so a bad title degrades search and not
only display. The rules extractor no longer takes the caption's first line: it scans down to the
first section header, skipping hashtag blocks and long hooks, and prefers a line that reads like a
name — short, carrying a letter, not itself a header ending in `:`. Whatever it picks is stripped
of its leading emoji run and trailing hashtag block. A caption that offers nothing usable gets
`Unbenanntes Rezept` and a reduced confidence.

### Categories

The rules propose categories from what a caption's author said about the dish: the words of its
title and any lines before the first section header, and its hashtags wherever they are. `#vegan
#backen` under a banana bread gives `Vegan` and `Backen`; `Grüner Power-Smoothie` gives `Getränk`.
The ingredient and instruction sections are not read — "40 Minuten backen" is how a gratin ends,
and one "vegane Butter" does not make a dish vegan. Only the nine seeded categories can be proposed,
a test holds the keyword list to the seed, and the import still drops any name the database does not
hold. The list is short on purpose: a missing category is where rules-only mode already was, while a
wrong one files a recipe where nobody looks for it. Before this, rules-only mode — which is what the
default `hybrid` runs without an API key — proposed no categories at all, and the category filter
had nothing to filter.

### Prompt injection

The caption is attacker-controlled: anyone can post content designed to be saved. It reaches the
model inside a `<caption>` span the system prompt names, with a statement that everything between
the tags is data rather than instructions — including the confidence field, since a caption that
talks the score up is a caption that publishes without review. A caption carrying a `<caption>` or
`</caption>` tag of its own has it neutralised first, so it cannot close the span early.

Two things already bounded the blast radius and still do: `ToolChoice` is pinned to
`record_recipe`, so there is no second tool to steer the model into, and the output schema is
closed. Two more are new. The categories an import may attach are now closed to the vocabulary
already in the database — the seeded set plus anything a human has created — so an injected
caption can no longer write an arbitrary row into the table every user's picker reads. Ingredient
and unit names cannot be a closed set, so they are bounded instead: a name is collapsed to one
line and truncated, which keeps a row a label rather than a payload.

### Measuring extraction quality

`internal/extraction/testdata/gold/` holds a gold set — captions with the extraction a human says
is correct for each — and `gold_test.go` scores the extractors against it, on ingredient-level
precision and recall rather than exact equality.

    go test ./internal/extraction -run GoldSetRules -v      # hermetic, runs in CI

    RECIPE_READER_EVAL_LLM=1 ANTHROPIC_API_KEY=sk-... \
      go test ./internal/extraction -run GoldSetLLM -v      # opt-in; costs money

Both print a per-case table. The aggregate floors are a regression signal, not a target; read
`testdata/gold/README.md` before changing them, and note the provenance caveat there — the corpus
is written rather than collected, because the Instagram endpoints have still not been run against
a real account.

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
login attempt on it. A saved session file that exists but cannot be read is logged as a warning
naming the file before the password login that replaces it; a missing one — the first boot — is
only noted.

With `INSTAGRAM_COLLECTION` set, the collection's id is looked up once and reused rather than listed
on every run, which was one extra private-API request per import. It is looked up again after a day,
so a collection renamed away and replaced under the configured name is picked up without a restart,
and after any failed fetch other than a rate limit, an authentication problem or a timeout — what the
endpoint answers for a deleted collection has never been observed, so the cache does not bet on it.

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

Everything the frontend handler serves carries `X-Content-Type-Options: nosniff`,
`X-Frame-Options: DENY`, `Referrer-Policy: no-referrer` and a strict Content-Security-Policy:

    default-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none';
    form-action 'self'; frame-ancestors 'none'

It can be that strict because the bundle is: one same-origin script, one same-origin stylesheet, no
inline code. A test fails if `index.html` ever gains an inline script, style or event handler, rather
than leaving the browser to block it silently. `frame-ancestors 'none'` is what stops another page
framing the app to trick a click onto a destructive button. Images from other origins are not
allowed — the stored `image_url` is not displayed, and why is recorded in
[`docs/PROJECT.md` §6.1](docs/PROJECT.md).

`src/types.ts` mirrors the Go DTOs in `internal/api/dto.go` field-for-field. Nothing enforces
that correspondence at compile time — if you change one side, change the other, or the drift
surfaces as `undefined` values in the UI.

## Docker

The frontend build is embedded into the binary with `go:embed`, so the application ships as a
single file with no assets to deploy beside it. Anything not under `/api/` is served from that
embedded directory, falling back to the app shell rather than a 404.

    API_TOKEN=$(openssl rand -hex 32) POSTGRES_PASSWORD=$(openssl rand -hex 16) \
      docker compose up --build        # app on :8080, Postgres on 127.0.0.1:5432
    docker compose down

Two variables are mandatory and have no defaults; compose fails with a message naming each one
rather than starting something open to the network.

`API_TOKEN`, because the compose stack publishes port 8080 and the container therefore binds every
interface — the case the server refuses without a token.

`POSTGRES_PASSWORD`, because it used to be the literal `recipes`, for user `recipes` on database
`recipes`, with Postgres published on every host interface. On a laptop that is a convenience; on
the VPS a working compose file invites you to copy it to, it is an internet-exposed database with
a three-way-guessable credential. The published port is now bound to `127.0.0.1` as well — the app
reaches `db` by name over the compose network and never needed it published at all; it is there
only so `make db-up` can serve a binary running on the host.

`POSTGRES_USER` and `POSTGRES_DB` still default to `recipes`, and `DB_DSN` is built from all three
so the app and the database cannot disagree. Set `DB_DSN` yourself to override it wholesale, which
is also the answer for a password containing characters a URL would have to percent-encode.

The image builds the frontend and the Go binary in separate stages and ships only the binary on
Alpine — about 20 MB, running as a non-root user. Credentials come from the environment (see
`docker-compose.yml`), and the Instagram session is kept in the `app-data` volume so the app does
not log in again on every restart, which Instagram rate-limits.

`make db-up` starts only the Postgres service, for running the Go binary locally against the same
database the container would use. It needs `POSTGRES_PASSWORD` too.

### Postgres version

The database is **Postgres 18** (`postgres:18-alpine`). The tag names only the major version, so
patch releases arrive with `docker compose pull`; they share the on-disk format. The test
containers and `scripts/smoke-test-image.sh` run the same image, so tests cover the version you
deploy.

The volume is mounted at `/var/lib/postgresql`, not `/var/lib/postgresql/data` as it was on 17. The
18 image keeps its data one level down, in a per-version directory (`18/docker`), and refuses to
start with a volume at the old path.

### Upgrading from Postgres 17

A volume created by the 17 compose file does not start under 18. A major version changes the
on-disk format, and the 18 image exits with an error naming the old data rather than starting an
empty database over it. Move the data with a dump and restore. For a database this size it takes
seconds, and it avoids needing both versions' binaries, as `pg_upgrade` would.

Every compose command needs `POSTGRES_PASSWORD` and `API_TOKEN` set, as usual; `down` and `exec`
fail without them too. Replace `recipes` with your `POSTGRES_USER` / `POSTGRES_DB` if you changed
them.

    # 1. Still on the 17 compose file: dump the database, then stop.
    docker compose up -d --wait db
    docker compose exec -T db pg_dump -U recipes -d recipes -Fc > recipes.dump
    docker compose down

    # 2. Remove the 17 volume. Compose prefixes it with the project name (the directory
    #    name by default), so check `docker volume ls` first. Keep recipes.dump until step 3
    #    has succeeded: from here on it is the only copy.
    docker volume rm recipe-reader_db-data

    # 3. On the 18 compose file: start the database alone, restore into it, then start the app.
    docker compose up -d --wait db
    docker compose exec -T db pg_restore -U recipes -d recipes --no-owner --exit-on-error < recipes.dump
    docker compose up -d

Restore before the app starts. On first start the app runs migrations and seeds the lookup
tables, and the restore would then collide with the tables and rows it created. The restored `schema_migrations`
table leaves the app nothing to migrate, and the ID sequences carry on from where they were. If the
dump predates a migration, `docker compose run --rm app migrate` applies the missing ones before the
app starts; against an up-to-date dump it changes nothing.

### Health check

The image declares a `HEALTHCHECK`, and the probe is the binary itself:

    recipe-reader healthcheck       # exit 0 if /api/healthz on HTTP_ADDR answers {"status":"ok"}

The runtime image has no `curl` or `wget`, and adding one to probe ourselves would grow it for one
`GET`. The probe dials loopback when `HTTP_ADDR` names every interface (`:8080`, as in the
container), gives up after 3s, and checks the body as well as the status so that something else
answering on the port does not pass. It polls every 2s while the container starts and every 30s
after, so `docker compose ps` reports the app `healthy` within seconds. The compose `app` service
inherits it rather than restating it.

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

    make check   # gofmt + go vet + golangci-lint + frontend typecheck/build
                 # + govulncheck + go test -race
                 # go test starts one ephemeral Postgres container per database package
                 # via testcontainers-go — Docker must be running.

    make frontend-check   # just the frontend half: npm run typecheck && npm run build

`check` covers the frontend because CI does: a TypeScript error or a broken Vite build used to
pass the local gate and fail only after the push. The `npm ci` behind it is keyed on
`web/package-lock.json`, so it reruns only when the lockfile actually changes.

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
the image's own `HEALTHCHECK` reports `healthy`, `/api/recipes` answers (so the migrations ran
against a real database), `/` serves the built
frontend rather than the placeholder page, and `--version` and `/api/healthz` agree on a version
that is not the unstamped `dev` — the link-time stamp is the one part of the build no unit test can
reach. The expected version is optional; without it the script only refuses `dev`. It runs the same
way locally as in CI.

## Database

The connection pool is configured rather than taken as `pgxpool` hands it over: **10** maximum
connections (above pgxpool's `max(4, NumCPU)`, which is four on a small container), a floor of **2** so the pool does not drain to nothing between the six-hourly imports and
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

**Looking up a category, ingredient or unit reads before it writes.** Recipe writes resolve names
inside their own transaction, and the lookup used to be an `INSERT ... ON CONFLICT DO UPDATE` that
went through the insert path every time: it spent a sequence value, wrote a row version and took a
row lock even when the row existed. Held until the recipe committed, that lock made two writes naming
the same ingredient wait for each other, and two naming a pair in opposite order could deadlock. The
insert now runs only when the read found nothing, with `DO UPDATE` kept for the one race the read
cannot see.

**A recipe may list the same ingredient twice** — "200 g Zucker" for the dough, "50 g Zucker" for
the topping — and both lines are kept, each with its own amount. What the schema enforces instead
(migration `0003`) is that no two lines of a recipe share a position, so their order is total.

**A list page's `total` and its rows are read by two statements**, not from one snapshot, so a write
landing between them can leave the pager a page out until the next load. That is accepted rather
than overlooked: the window is milliseconds on a collection that changes every few hours.

## Changing the database schema

1. Add `internal/db/migrations/000N_*.up.sql` and the matching `.down.sql`.
2. Add or edit the relevant `internal/db/queries/*.sql`.
3. Run `make sqlc-generate`.
4. Commit the migration, the query, and the regenerated `internal/db/sqlc/` files together — CI
   fails if the generated code does not match its inputs.

`TestMigrations_UpDownUp` runs every migration up, all of them down over a database holding a row in
every table, and up again, through the same embedded source the binary uses (`db.MigrateDown`), so
a new migration's down file is exercised without any extra work. If the migration has to transform
data already in the table — `0002` and `0003` both do — give it a test of its own that stops at the
previous version, writes the rows that need transforming, and migrates over them; `testdb.NewDatabase`
provides the empty database for that.

## Architecture

The binary's own layering is deliberately thin. `cmd/recipe-reader/main.go` holds the kong command
tree and parses the command line against it, refusing a flag placed before a command it does not
belong to; `main` turns any failure into one log line and exit status 1. The commands themselves
live in `internal/cli`, one type per command whose fields are its flags, and each command's `Run`
delegates at once: `serve` to `internal/server`, the composition root that wires database,
extraction, Instagram, import worker and HTTP API; `healthcheck` to `internal/healthcheck`;
`migrate` to `internal/db`. The settings are declared and validated in `internal/config`.

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
own recipe collection — not as a scraping service for other people's data. What is kept, for how
long, and what would have to change before the collection could be shared is recorded in
[`docs/PROJECT.md` §6.1](docs/PROJECT.md).

**They have not been verified against a live account.** Nothing in this repository records a
successful run: that `feed/saved/posts/` is the right endpoint, that saved entries wrap the media
in a `media` key, and that `next_max_id` is this endpoint's cursor are all still assumptions. What
has changed is that a wrong assumption is now diagnosable rather than silent. The response is
decoded into named types instead of walked as `map[string]any`; a page that does not decode at all
comes back as `ErrSchemaDrift`; an item that does not decode is counted with a reason ("no media
code" and "undecodable item" point at different upstream changes); and items returned with none of
them readable is an error rather than an empty success.

Verifying this is a release gate and is still open. Run it against a real account, commit the
response as a `testdata/` fixture, and the three assumptions above become a test.
