# Code Review — `persistence_reviewer`

**Date:** 2026-09-11
**Reviewer role:** Database and persistence engineer — Postgres, pgx, sqlc (panel definition: `agents.yaml` at the repo parent, role `persistence_reviewer`)
**Scope:** `internal/db/**`, `internal/repository/**`, `internal/db/migrations`, `internal/db/queries`, pool wiring in `cmd/recipe-reader/main.go`
**Mode:** single pass, no panel iteration, no consensus round with the other reviewers

sqlc and pgx are taken as settled project decisions; no ORM is proposed anywhere below.

> **Cross-examined by the chair on 2026-09-11.** Ratings below are post-round.
> Findings that moved, merged or were withdrawn carry a note under their heading.
> See the [consensus record](2026-09-11-consensus.md) for the rulings and for
> nine corrections established during the round.

## Rating scale

Each finding carries a rating from **0.1 to 10.0**. The scale is shared by all
six reviewers so the numbers are comparable across reviews, and it weighs three
things together: how likely the defect is to be hit in this project's actual
deployment, how bad the outcome is when it is hit, and how visible the failure
is once it happens — a defect that fails silently rates above an equally severe
one that announces itself.

| Band | Meaning |
| --- | --- |
| 9.0-10.0 | Critical. Exploitable or data-destroying as shipped; fix before the next deploy. |
| 7.0-8.9 | High. A real defect users or operators will hit; schedule it deliberately. |
| 4.0-6.9 | Medium. Genuine, worth fixing, but bounded in blast radius or reach. |
| 2.0-3.9 | Low. Correct to fix; nothing breaks if it waits. |
| 0.1-1.9 | Informational. A note, a nit, or a recorded non-issue. |

Ratings are this reviewer's own, assigned without seeing the other five reviews
and without a chair to reconcile them. Where two reviewers rated the same
underlying defect differently, the index records the divergence rather than
averaging it away.

## Summary

| # | Severity | Rating | Finding | Location |
| --- | --- | --- | --- | --- |
| P1 | High | ~~7.2~~ **4.0** | Dedupe check and insert race: TOCTOU between `GetBySource` and `Create` | `internal/pipeline/pipeline.go:58-81` |
| P2 | Medium | **5.0** | Lookup rows are committed outside the recipe transaction | `internal/api/handlers_recipes.go:164-190`, `internal/pipeline/pipeline.go:104-126` |
| P3 | Medium | **4.8** | `LIKE` wildcards in user input are not escaped | `internal/db/queries/recipes.sql` (`SearchRecipes`, `CountRecipes`) |
| P4 | Medium | ~~4.5~~ **3.0** | N+1: one page of search costs `2 + 2N` queries | `internal/repository/recipe_repository.go:208-215` |
| P5 | Medium | **4.2** | `pgxpool` is created with no configuration at all | `internal/db/connect.go:28` |
| P6 | Low | **2.0** | `FindOrCreate*` burns a sequence value on every conflicting call | `internal/db/queries/*.sql` |
| P7 | Low | ~~3.5~~ **5.5** | `recipes.status` has no CHECK constraint | `internal/db/migrations/0001_init.up.sql` |
| P8 | Low | **3.0** | `SELECT DISTINCT r.*` over a LEFT JOIN that is only needed when filtering | `internal/db/queries/recipes.sql` (`SearchRecipes`) |
| P9 | Low | **2.4** | `recipe_ingredients` permits exact duplicate rows | `internal/db/migrations/0001_init.up.sql` |
| P10 | Low | **1.5** | Count and page are read in two unsynchronised queries | `internal/repository/recipe_repository.go:187-202` |
| P11 | Low | **2.6** | The down migration is never exercised | `internal/db/migrations/0001_init.down.sql` |

## High

### P1. Dedupe check and insert race: TOCTOU between `GetBySource` and `Create`

**Rating: 4.0 / 10** — revised down from 7.2 in cross-examination. The race is latent, not live: the worker mutex closes the import-vs-import path, and the only other concurrent writer, `POST /api/recipes`, has no caller in the shipped frontend (`createRecipe` at `web/src/api.ts:49` is never called). Two claims in the text below do not survive: a retry after a partial run does *not* collide, and "a second process" is not a shipped configuration.

**Location:** `internal/pipeline/pipeline.go:58-81`

The import loop reads `GetBySource`, branches on `ErrNotFound`, then later calls `Create` — two separate transactions with a full extraction (an LLM round trip, seconds) in between:

```go
if _, err := p.Recipes.GetBySource(ctx, post.Source); err == nil {
    result.Skipped++
    continue
}
...
if err := p.Recipes.Create(ctx, recipe); err != nil {
    result.Failed++
```

`recipes.source` is `UNIQUE`, so the database correctly refuses the duplicate — but the pipeline then counts it as **`Failed`, not `Skipped`**, and (per `go_reviewer` #5) logs nothing, so the tally misreports a benign race as an import error. Two concurrent runs are reachable today: the API's `POST /api/import/run` and the ticker both call `RunOnce`, and while `Worker` serialises those, a second process, a manual `POST /api/recipes` with the same source, or a retry after a partial run all collide the same way.

**Fix:** make the insert itself the dedupe. Add to `CreateRecipe`:

```sql
-- name: CreateRecipe :one
INSERT INTO recipes (name, instructions, image_url, source, status)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (source) DO NOTHING
RETURNING *;
```

and treat `pgx.ErrNoRows` from it as "already present" → `Skipped`. That removes the read entirely, halves the per-post query count, and makes the outcome correct under concurrency. If you prefer to keep the pre-check as a fast path, keep it *and* classify a `23505` unique violation (`pgerrcode.UniqueViolation`, already an indirect dependency) as `Skipped`.

## Medium

### P2. Lookup rows are committed outside the recipe transaction

**Rating: 5.0 / 10**

**Location:** `internal/api/handlers_recipes.go:164-190`, `internal/pipeline/pipeline.go:104-126`

`FindOrCreateCategory` / `FindOrCreateIngredient` / `FindOrCreateUnit` run on `pgLookupRepository`, which holds `sqlc.New(pool)` — the bare pool, not the caller's transaction (`internal/repository/lookup_repository.go:28`). They are resolved *before* `Create`/`Update` opens its transaction (`recipe_repository.go:54`). When the recipe insert then fails — a unique violation on `source` (P1), a cancelled context, a constraint error — the ingredient and category rows it created are already committed and stay behind as orphans.

Practically this accumulates junk lookup rows that show up in the frontend's autocomplete pickers, fed by `ListIngredients`. With the caption-driven import writing whatever the LLM invented as an ingredient name, the pickers degrade over time.

**Fix:** give `LookupRepository` a `WithTx(tx pgx.Tx) LookupRepository` (sqlc already emits `Queries.WithTx`, used at `recipe_repository.go:60`), open the transaction in the handler/pipeline, and resolve the lookups inside it. The `emit_interface: true` setting in `sqlc.yaml` makes this a small change. Alternatively, accept the orphans deliberately and add a periodic cleanup query — but then say so in a comment, because the current code reads as if it were transactional.

### P3. `LIKE` wildcards in user input are not escaped

**Rating: 4.8 / 10**

**Location:** `internal/db/queries/recipes.sql`, `SearchRecipes` and `CountRecipes`

```sql
lower(r.name) LIKE '%' || lower(sqlc.narg(text)::text) || '%'
```

The value is properly parameterised, so this is not an injection — but `%` and `_` are *LIKE metacharacters*, not literals. Searching for `50%` matches every recipe; searching for `a_b` matches `axb`. The backslash is likewise live. It is a correctness bug in a user-facing search box, and it makes `total` from `CountRecipes` wrong in the same way.

**Fix:** escape in SQL so the query and the count cannot drift:

```sql
lower(r.name) LIKE '%' || replace(replace(replace(
    lower(sqlc.narg(text)::text), '\', '\\'), '%', '\%'), '_', '\_') || '%' ESCAPE '\'
```

Or normalise once in `Search` before building `textArg` (`recipe_repository.go:177`) — but then both queries must use the escaped value, which is exactly the drift the SQL-side fix avoids.

### P4. N+1: one page of search costs `2 + 2N` queries

**Rating: 3.0 / 10** — revised down from 4.5. This reviewer withdrew its criticism of the code comment at `recipe_repository.go:204-207`: the comment does enumerate both child queries and does not undercount them.

**Location:** `internal/repository/recipe_repository.go:208-215`, `assemble` at `:219-252`

The loop calls `assemble` per row, and each `assemble` issues `ListRecipeIngredients` + `ListRecipeCategories`. With the maximum `pageSize` of 100 that is `1` count + `1` search + `200` round trips for a single `GET /api/recipes`. The in-code comment (`:204-207`) calls this out and defers it deliberately, which is the right instinct at personal-collection scale — I am recording it because the comment underestimates the constant: it is two queries per row, not one, and the default page size of 20 already means 42 round trips on the list view that loads first.

**Fix, when it is time:** two queries total, not `2N`. Collect the page's ids and fetch both child sets with `= ANY($1::bigint[])`, then group in Go:

```sql
-- name: ListIngredientsForRecipes :many
SELECT ri.recipe_id, ri.ingredient_id, i.name AS ingredient_name, ri.amount, ri.unit_id,
       COALESCE(u.name, '') AS unit_name
FROM recipe_ingredients ri
JOIN ingredients i ON i.id = ri.ingredient_id
LEFT JOIN units u ON u.id = ri.unit_id
WHERE ri.recipe_id = ANY($1::bigint[])
ORDER BY ri.recipe_id, ri.position, ri.id;
```

`GetByID`/`GetBySource` can keep using the single-row `assemble`; only `Search` needs the batched path.

### P5. `pgxpool` is created with no configuration at all

**Rating: 4.2 / 10**

**Location:** `internal/db/connect.go:28`

`pgxpool.New(ctx, dsn)` accepts every default: `MaxConns = max(4, runtime.NumCPU())`, `MinConns = 0`, `MaxConnLifetime = 1h`, `MaxConnIdleTime = 30m`, no `ConnConfig.ConnectTimeout` beyond the driver default. Nothing is tunable without editing code, and the DSN is the only lever an operator has.

Two concrete consequences here: with `MinConns = 0` the pool drops to zero connections between the 6-hourly imports, so the first request after an idle period pays a full connect + TLS handshake; and the N+1 in P4 multiplied by the default `MaxConns` on a small container (2 CPUs → 4 connections) makes the list endpoint the pool's bottleneck under even light concurrent use.

**Fix:** parse and adjust, which also gives you a place to hang a DSN-independent knob:

```go
cfg, err := pgxpool.ParseConfig(dsn)
if err != nil {
    return nil, fmt.Errorf("db: parse dsn: %w", err)
}
cfg.MaxConns = 10
cfg.MinConns = 2
cfg.MaxConnIdleTime = 5 * time.Minute
cfg.ConnConfig.ConnectTimeout = 5 * time.Second
pool, err := pgxpool.NewWithConfig(ctx, cfg)
```

Note that `pgxpool.New` also does not connect eagerly — the `Ping` at `:32` is what proves reachability, and that part is right.

## Low

### P6. `FindOrCreate*` burns a sequence value on every conflicting call

**Rating: 2.0 / 10**

**Location:** `internal/db/queries/categories.sql`, `ingredients.sql`, `units.sql`

```sql
INSERT INTO categories (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;
```

The `DO UPDATE` is the standard trick to make `RETURNING` fire on conflict, and it is the right call — but it is worth knowing what it costs: `BIGSERIAL` advances before the conflict is detected, so every repeated lookup leaves a gap in the id sequence; the no-op update writes a new row version, producing WAL traffic and dead tuples for autovacuum; and it takes a row-level lock. With `Seed` alone that is 20 wasted ids per boot (`internal/db/seed.go:27-36`), and the import path calls these once per ingredient per post.

**Fix (optional, only if the churn shows up):** read first, insert on miss, and keep the `ON CONFLICT` as the race fallback. At this project's volume the current version is defensible — record it so the choice is deliberate rather than inherited.

### P7. `recipes.status` has no CHECK constraint

**Rating: 5.5 / 10** — chair ruling R1, merged into `go` #2. Conceded: rating a *fix* rather than a *defect* was a category error. **Condition adopted by the chair:** the merged fix must include this CHECK constraint and its backfill, not just handler validation.

**Location:** `internal/db/migrations/0001_init.up.sql`, `status TEXT NOT NULL`

`domain.RecipeStatus` has exactly two values (`internal/domain/models.go:15-17`) and the database enforces neither. `go_reviewer` #2 covers the API path that exploits this (a `PUT` without `status` persists `''`); from the schema side the fix is independent and belongs here as defence in depth:

```sql
ALTER TABLE recipes ADD CONSTRAINT recipes_status_check
    CHECK (status IN ('needs_review', 'published'));
```

as a new `0002_*.up.sql`. Migrating existing rows first (`UPDATE recipes SET status = 'needs_review' WHERE status NOT IN (...)`) keeps it applicable to a database that already took bad writes.

### P8. `SELECT DISTINCT r.*` over a LEFT JOIN that is only needed when filtering

**Rating: 3.0 / 10**

**Location:** `internal/db/queries/recipes.sql`, `SearchRecipes` / `CountRecipes`

The `LEFT JOIN recipe_categories` exists solely to support the optional `category_id` filter, but it is always joined — so when no category is given, every recipe is multiplied by its category count and then de-duplicated by a `DISTINCT` over all eight columns. That forces a sort or hash-aggregate on the full row width for the common unfiltered listing.

**Fix:** make the filter a semi-join, which removes the need for `DISTINCT` entirely and lets the planner skip the join when the argument is NULL:

```sql
AND (sqlc.narg(category_id)::bigint IS NULL OR EXISTS (
      SELECT 1 FROM recipe_categories rc
      WHERE rc.recipe_id = r.id AND rc.category_id = sqlc.narg(category_id)::bigint))
```

Then `SELECT r.*` and `COUNT(*)`. This also removes the `DISTINCT`/`ORDER BY` interaction that currently constrains which columns may be sorted on.

### P9. `recipe_ingredients` permits exact duplicate rows

**Rating: 2.4 / 10**

**Location:** `internal/db/migrations/0001_init.up.sql`

There is no unique constraint on `(recipe_id, ingredient_id)` or `(recipe_id, position)`. `recipe_categories` got this right with a composite primary key, and `AddRecipeCategory` leans on it with `ON CONFLICT DO NOTHING`; the ingredient table has no equivalent. A DTO listing "Mehl" twice produces two rows, and the UI renders both.

Whether that is a bug is a product decision — "2 EL Öl" appearing twice in a recipe is not absurd — so the honest recommendation is: decide explicitly. If duplicates are meaningless, add `UNIQUE (recipe_id, ingredient_id)`; the delete-then-reinsert in `writeAssociations` (`internal/repository/recipe_repository.go:107-122`) means no migration backfill conflict on next write.

### P10. Count and page are read in two unsynchronised queries

**Rating: 1.5 / 10**

**Location:** `internal/repository/recipe_repository.go:187-202`

`CountRecipes` and `SearchRecipes` run as two separate autocommit statements. A concurrent import between them yields a `total` that disagrees with the rows returned — the pager can show a page that does not exist. At import-once-per-six-hours cadence this is nearly unobservable, and a repeatable-read transaction to fix it costs more than the symptom. Recorded for completeness, not for action.

### P11. The down migration is never exercised

**Rating: 2.6 / 10** — merged with `delivery` D12 at the same rating; delivery owns the task, since it lands inside their `TestMain` refactor. **Wrinkle neither review stated:** `internal/db` exports no Down at all, so the fix needs a new entry point in `connect.go`, not just a test.

**Location:** `internal/db/migrations/0001_init.down.sql`

`internal/db/testdb/testdb.go:46` only ever runs `db.Migrate` (up). The `DROP TABLE IF EXISTS` ordering in the down file is correct — children before parents — but nothing proves it, and it will silently rot as soon as a `0002` migration lands. A single test that migrates up, down, and up again against a throwaway container would pin it. This overlaps `delivery_reviewer`'s territory; the schema-side point is that a down migration you never run is not a rollback plan.

## What's right

- **Transaction handling in `Create`/`Update` is correct**, and unusually careful about it: `defer tx.Rollback(ctx)` with a `//nolint:errcheck` that explains why the error is safe to drop, `queries.WithTx(tx)` so the child writes actually join the transaction, and `writeAssociations` (`recipe_repository.go:107`) doing delete-then-reinsert as one atomic replace. There is no half-updated-association bug to find here.
- **Nullable modelling is right end to end.** `unit_id BIGINT REFERENCES units(id)` is nullable in the schema, `pgtype.Int8` at the sqlc boundary, and `*int64` in the domain — and `assemble` repopulates the write-side `UnitID` (`:235-240`) so a load-modify-save round trip does not null the unit. That is the exact bug this shape usually ships with.
- **`ON DELETE` behaviour is deliberate and correct:** `CASCADE` from `recipes` to both child tables so deleting a recipe cleans up after itself, and no cascade from `ingredients`/`units` so a lookup row in use cannot be deleted out from under a recipe. There is no delete endpoint for lookups, so the restrictive default is never hit.
- **Indexing is honest.** `idx_recipes_name_lower` ships with a comment stating plainly that it does *not* serve `SearchRecipes` because the filter is a leading-wildcard `LIKE`, and naming `pg_trgm` + GIN as the real fix. An index accompanied by an accurate account of what it does not do is rarer than it should be.
- **Tests run against real Postgres** via testcontainers (`internal/db/testdb/testdb.go`), with `BasicWaitStrategies()` passed explicitly and the reason documented — that specific race (migrating before the init-script restart) is one most projects discover in CI instead.
- **`emit_interface: true`** in `sqlc.yaml` and the `ErrNotFound` sentinel translated from `pgx.ErrNoRows` at exactly one boundary keep the query layer boring and explicit, which is what I want from it.
