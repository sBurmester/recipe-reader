# Code Review — `go_reviewer`

**Date:** 2026-09-11
**Reviewer role:** Senior Go engineer — idiomatic API design, error handling, concurrency (panel definition: `agents.yaml` at the repo parent, role `go_reviewer`)
**Scope:** all non-generated Go sources, ~2,100 LOC across 25 files (`internal/db/sqlc/**` excluded as generated)
**Mode:** single pass, no panel iteration, no cross-examination

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

## Toolchain baseline

| Check | Result |
| --- | --- |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `golangci-lint run ./...` (govet, staticcheck, unused, errcheck) | 0 issues |

Everything below is something the configured linters cannot detect.

## Summary

| # | Severity | Rating | Finding | Location |
| --- | --- | --- | --- | --- |
| 1 | Medium | ~~5.2~~ **3.0** | `int32` truncation on the SQL offset turns a query param into a 500 | `internal/repository/recipe_repository.go:198` |
| 2 | Medium | ~~6.5~~ **5.5** | `PUT /api/recipes/{id}` skips create's validation and writes an out-of-domain status | `internal/api/handlers_recipes.go:104` |
| 3 | Medium | ~~6.9~~ **7.4** | Instagram client takes no `context.Context`; imports cannot be cancelled or bounded | `internal/instagram/saved.go:77` |
| 4 | Medium | **5.0** | Detached import goroutine outlives the pool it queries | `internal/api/handlers_import.go:24` |
| 5 | Medium | ~~6.8~~ **5.8** | Every per-post import failure is discarded silently | `internal/pipeline/pipeline.go:58` |
| 6 | Medium | ~~5.5~~ **6.4** | `--extraction-mode=llm` silently does nothing | `cmd/recipe-reader/main.go:61` |
| 7 | Medium | **4.5** | `--import-interval=0` panics the process at startup | `internal/pipeline/worker.go:36` |
| 8 | Low | ~~4.0~~ **4.8** | `http.Server` has no timeouts | `cmd/recipe-reader/main.go:101` |
| 9 | Low | **2.0** | `dtoToRecipe` takes `*http.Request` where it wants a `context.Context` | `internal/api/handlers_recipes.go:159` |
| 10 | Low | **2.5** | `Worker.RunOnce` returns the *previous* result when it skips | `internal/pipeline/worker.go:54` |
| 11 | Low | **2.2** | `withRecovery` swallows `http.ErrAbortHandler` | `internal/api/middleware.go:29` |
| 12 | Low | **2.8** | `withLogging` records no status code | `internal/api/middleware.go:15` |
| 13 | Low | **2.5** | `LoginOrRestore` discards the `LoadSettings` error | `internal/instagram/client.go:29` |
| 14 | Low | **3.0** | `map[string]any` spelunking hides upstream schema drift | `internal/instagram/saved.go:89` |
| 15 | Low | **1.2** | Doc-comment inconsistency across five files | `internal/repository/lookup_repository.go:1` |

The two to fix first are **#3** (an import that cannot be cancelled) and **#5** (failures that leave no trace) — together they mean a broken import is both unstoppable and undiagnosable.

## Medium

### 1. `int32` truncation on the SQL offset turns a query param into a 500

**Rating: 3.0 / 10** — revised down from 5.2 in cross-examination: reaching the defect needs a hand-crafted `?page=` above 10^8, and the worst outcome is a single 500 with nothing persisted.

**Location:** `internal/repository/recipe_repository.go:198`

`Offset: int32((page - 1) * pageSize)`. `page` comes straight from `strconv.Atoi` (`internal/api/handlers_recipes.go:29`) and `Search` only clamps the lower bound (`recipe_repository.go:170`); `pageSize` is clamped, `page` is not. Verified wrap behaviour:

```
page=214748365  -> int32 offset=-16           → Postgres "OFFSET must not be negative" → 500
page=3000000000 -> int32 offset=-129542164    → same
```

A value that wraps to a small positive offset is worse: it returns page-1 rows while claiming to be page N, with no error at all.

**Fix:** clamp in `Search`, next to the existing guards, before the conversion.

```go
const maxOffset = math.MaxInt32
if off := int64(page-1) * int64(pageSize); off > maxOffset {
    return nil, total, nil // or clamp page to the last valid one
}
```

### 2. `PUT /api/recipes/{id}` skips create's validation and writes an out-of-domain status

**Rating: 5.5 / 10** — chair ruling R1, merged with `persistence` P7. Revised down from 6.5: the claim that a `status=''` row is invisible in the list view is false (`web/src/pages/list.ts:54-58` sends no status filter), then back up because P7 established the POST path is affected too. **The fix is both halves** — handler validation *and* the CHECK constraint.

**Location:** `internal/api/handlers_recipes.go:104-131`, compare `:75-99`

`handleCreateRecipe` requires name+source (`:81`) and defaults an empty status to `published` (`:86`). `handleUpdateRecipe` does neither, and `recipes.status` carries no CHECK constraint (`internal/db/migrations/0001_init.up.sql`). A PUT body without `status` therefore persists `status = ''`. That row matches neither `?status=published` nor `?status=needs_review` and is effectively invisible in the list view. `name: ""` is accepted on the same path while POST rejects it.

The bundled frontend happens to round-trip both fields (`web/src/pages/detail.ts:132`, with an empty-name guard at `:116`), so nothing hits this today — but this is a public HTTP API and the guard belongs on the server. No test covers it: `internal/api/handlers_recipes_test.go` always sends an explicit status.

**Fix:** validate `dto.Status` against `domain.StatusNeedsReview` / `domain.StatusPublished` (400 on anything else) and require a non-empty name, in both handlers.

### 3. The Instagram client takes no `context.Context`, so an import cannot be cancelled or bounded

**Rating: 7.4 / 10** — conceded to `integration` I3 and merged there. This reviewer had set 6.9 to stay inside its own "Medium" label, which the chair rejected as fitting the evidence to the band. Note the fix proposed below **is not implementable**: `PrivateRequest` takes no context and the `*http.Client` is unexported with no setter.

**Location:** `internal/instagram/fetcher_adapter.go:25`, `internal/instagram/saved.go:28,73,77`

`FetchNewPosts(_ context.Context)` discards the context outright, and `FetchSavedPosts` / `FetchCollectionPosts` / `pageMedia` have no `ctx` parameter at all. The pagination loop (`saved.go:80-111`) has no page cap, no per-request timeout, and no `ctx.Err()` check — its only exits are `maxItems` reached, empty `items`, or an empty `next_max_id`, all decided by an unofficial, explicitly unverified endpoint (`saved.go:26-27`). A hung or looping remote leaves the worker stuck with no way out, SIGTERM included.

**Fix:** thread `ctx` through `pageMedia` into `PrivateRequest` (or wrap the call with `http.NewRequestWithContext` if instago allows), check `ctx.Err()` at the top of each page iteration, and add a page counter cap alongside `maxItems`.

### 4. The detached import goroutine outlives the pool it queries

**Rating: 5.0 / 10** — held in round 2, **mechanism corrected**. This is *not* a use-after-close: `puddle/v2@v2.2.2/pool.go:179-195` destroys only idle resources, leaving an in-flight connection untouched, and the process usually exits first. The text below also overstates the symptom — the bogus tally is never reported, because the process is gone before `Status()` could be read. The rating holds on different reasoning: high likelihood, small blast radius, zero diagnosability.

**Location:** `internal/api/handlers_import.go:24-25`, `cmd/recipe-reader/main.go:49,121`

`go d.Worker.RunOnce(context.WithoutCancel(r.Context()))` launches an untracked goroutine. The shutdown path waits for the HTTP server to drain (`main.go:121`) and then returns, firing `defer pool.Close()` (`main.go:49`) while an in-flight import still holds that pool. `Worker.Start`'s ticker goroutine has the same gap: ctx cancellation stops the *loop*, but an in-flight `RunOnce` is never awaited.

Consequence is failed queries and a bogus tally at shutdown rather than corruption — the transaction rolls back — but it is an avoidable race.

**Fix:** give `Worker` a `sync.WaitGroup`, `wg.Add(1)` around every `RunOnce` launch, and call `worker.Wait()` in `run()` before returning. `WithoutCancel` is the right call for the request-scoped part; it just needs a lifecycle owner.

### 5. Every per-post import failure is discarded silently

**Rating: 5.8 / 10** — revised down from 6.8 (top-of-band inflation). Chair ruling R2: one theme, three sites, three ratings — this 5.8, `extraction` E8 at 6.0, `integration` I10 at 3.6.

**Location:** `internal/pipeline/pipeline.go:58-82`

Four `result.Failed++; continue` branches, each dropping `err` on the floor. `grep -rn "slog|log\." internal/pipeline internal/instagram internal/extraction internal/repository` returns nothing — there is no logging anywhere outside `main` and the API middleware. An operator seeing `"failed": 17` on `/api/import/status` has no way to learn whether that was the LLM, Instagram, or Postgres.

Related: the loop never checks `ctx.Err()`, so a cancelled context is reported as N per-post failures rather than an aborted run.

**Fix:** log at each branch, and abort early on cancellation.

```go
slog.Warn("import: post failed", "source", post.Source, "stage", "extract", "error", err)
// and at the top of the loop body:
if err := ctx.Err(); err != nil {
    return result, err
}
```

### 6. `--extraction-mode=llm` silently does nothing

**Rating: 6.4 / 10** — **raised** in round 2 (chair ruling R6). Both reporting reviewers independently nominated this as the panel's most under-rated finding. It does not merely fail to use the LLM — it silently selects the *opposite* of what was asked, on every post of every run, with a paid key unused and nothing in the logs. **Verified by the chair:** Task 18's note 3 records this as an open question needing a decision, and the task shipped marked done.

**Location:** `internal/config/config.go:26`, `cmd/recipe-reader/main.go:61`

The flag documents `rule, llm, hybrid`; `main.go` only ever tests `== "hybrid"`. `llm` — and any typo — falls through to rules-only with no warning, which looks identical to a missing API key.

**Fix:** let kong validate it, `enum:"rule,llm,hybrid" default:"hybrid"`, and switch on all three in `run()`. For `llm`, pass the LLM extractor as the sole extractor rather than wrapping it in the hybrid.

### 7. `--import-interval=0` panics the process at startup

**Rating: 4.5 / 10**

**Location:** `internal/pipeline/worker.go:36`

`time.NewTicker(w.interval)` runs on the *calling* goroutine and panics for `d <= 0`. `NewWorker` states the precondition in its doc comment (`:26`) without enforcing it, and nothing between `config.Load` and `Start` checks it — so a config typo aborts with a stack trace instead of the clean `slog.Error("fatal", ...)` path `main` was built for. The same gap applies to an `ExtractionThreshold` outside `0..1`.

**Fix:** validate in `config.load` (kong supports this on the struct) or return an error from `NewWorker`.

## Low

### 8. `http.Server` has no timeouts

**Rating: 4.8 / 10** — conceded to `security` S3 and merged there, S3's scope being a strict superset. Note this is **two edits in two files**, not one: the timeout fields on the server literal, and `MaxBytesReader` at two handler sites.

**Location:** `cmd/recipe-reader/main.go:101`

`&http.Server{Addr: cfg.HTTPAddr, Handler: mux}`. Set at minimum `ReadHeaderTimeout` (slowloris), plus `ReadTimeout` / `WriteTimeout` / `IdleTimeout`. All handlers are fast — the import is already asynchronous — so the write timeout constrains nothing real.

### 9. `dtoToRecipe` takes `*http.Request` where it wants a `context.Context`

**Rating: 2.0 / 10**

**Location:** `internal/api/handlers_recipes.go:159`

It uses the request only for three `r.Context()` calls (`:165`, `:172`, `:182`). Passing the whole request into a mapping helper makes it untestable without an `httptest` request and hides what it actually depends on.

**Fix:** change the signature to `(ctx context.Context, dto RecipeDTO)` and pass `r.Context()` at both call sites.

### 10. `Worker.RunOnce` returns the *previous* result when it skips

**Rating: 2.5 / 10**

**Location:** `internal/pipeline/worker.go:54-60`

The caller cannot distinguish "ran and produced this" from "was busy, here is stale data," and the one production caller (`internal/api/handlers_import.go:25`) discards the value anyway.

**Fix:** return `(ImportResult, bool)`, or drop the return entirely and let `Status()` be the single reporting path. The test at `internal/pipeline/worker_test.go:46` asserts the skip via a call counter, so it would not need to change.

### 11. `withRecovery` swallows `http.ErrAbortHandler`

**Rating: 2.2 / 10**

**Location:** `internal/api/middleware.go:29`

That sentinel is how a handler (or `httputil.ReverseProxy`) deliberately aborts a connection; turning it into a logged 500 misreports it.

```go
if rec := recover(); rec != nil {
    if rec == http.ErrAbortHandler {
        panic(rec)
    }
    ...
}
```

### 12. `withLogging` records no status code

**Rating: 2.8 / 10**

**Location:** `internal/api/middleware.go:15`

Every request, 500s included, logs identically. A three-line `http.ResponseWriter` wrapper capturing `WriteHeader` would make the log line diagnostic.

### 13. `LoginOrRestore` discards the `LoadSettings` error

**Rating: 2.5 / 10**

**Location:** `internal/instagram/client.go:29`

A corrupt or truncated session file is indistinguishable from a missing one; both quietly trigger a fresh password login — exactly the event the session file exists to avoid. Log it before falling through.

### 14. `map[string]any` spelunking hides upstream schema drift

**Rating: 3.0 / 10**

**Location:** `internal/instagram/saved.go:89-134`

The comma-ok form is used consistently, so there is no panic risk — that is the important part. The cost is that any upstream schema drift makes items vanish with no signal: `extractMedia` returns `false`, the caller `continue`s. Given the endpoint is explicitly flagged as unverified, unmarshalling into a typed struct and counting/logging the drops would turn a silent empty import into a diagnosable one.

### 15. Doc-comment inconsistency

**Rating: 1.2 / 10**

**Locations:** `internal/repository/lookup_repository.go:1`, `internal/pipeline/worker.go:1`, `internal/instagram/client.go:1`, `internal/instagram/saved.go:1`, `internal/instagram/fetcher_adapter.go:1`

These five open with a redundant `// internal/path/file.go` comment instead of a doc comment. `LookupRepository` and `NewLookupRepository` (`lookup_repository.go:14,27`) are the only exported API in the project without doc comments — everything else is documented to an unusually high standard, which is what makes these stand out.

## What's right

Recorded deliberately, since the brief calls for honest severity rather than a padded list:

- `Create` / `Update` are correctly transactional: `defer tx.Rollback` + `WithTx`, with a `//nolint:errcheck` that actually explains itself (`internal/repository/recipe_repository.go:58,82`).
- `ErrNotFound` is a proper sentinel, translated from `pgx.ErrNoRows` at exactly one boundary.
- Error wrapping with `%w` is consistent and carries identifying context (`recipe %d`, `unit %q`).
- `Worker`'s mutex discipline is correct — the pipeline genuinely runs outside the lock, and `Status` cannot block behind a long import.
- `assemble` repopulating the write-side `UnitID` (`recipe_repository.go:235-240`) is a subtle load-modify-save bug that was anticipated rather than discovered later.
- The empty-slice-not-nil discipline in `internal/api/dto.go:81-86` is applied deliberately, with the reason stated.
- The N+1 in `Search` (`recipe_repository.go:204-207`) is knowingly documented and correctly out of scope at this data size — it belongs to `persistence_reviewer` rather than being double-counted here.
