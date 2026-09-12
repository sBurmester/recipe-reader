# Recipe Reader — Working Plan

**Derived from:** the six-reviewer panel of 2026-09-11 ([index](README.md)),
as adjudicated in the [consensus record](2026-09-11-consensus.md)
**Source findings:** 66 post-round (72 entering, 1 withdrawn, 5 merged), **62 tasks**
**Status:** in progress — bands ≥ 6.0 done (11 of 62)

Mark a task `[x]` when it is done. Each task cites the finding IDs it closes.

> **Revision 3 — post round-2 calibration (partial).** Round 2 tested the
> ratings themselves. Only `go_reviewer` and `extraction_reviewer` reported;
> four reviewers were cut off by an infrastructure limit, so **S1's possible
> up-rating and I1's unresolved magnitude remain open** — see "Outstanding" in
> the consensus record. Changes this revision: **T-23 raised to 6.4**, **T-24
> lowered to 4.8**, **T-18 reshaped** (the fix already exists on an unmerged
> branch), T-17's mechanism corrected.
>
> **Revision 2 — post cross-examination.** Ratings and band placement below are
> the chair's final figures. Nine corrections were established during the round;
> three of them changed tasks in this plan materially, and are flagged inline
> with **CORRECTED**. If you read revision 1, re-read T-01, T-11 and T-13.

## What the cross-examination changed in this plan

| Task | Was | Now | Why |
| --- | --- | --- | --- |
| T-13 Instagram login test | 7.2 | **2.5** | The premise was false — the test makes no network call and never has |
| T-07 dedupe TOCTOU | 7.2 | **4.0** | The race is latent; `createRecipe` has no caller in the shipped UI |
| T-10 shared test container | 7.0 | **5.5** | A developer cost, not a defect — though the count is 18 containers, not 8 |
| T-03 junk recipe rows | 8.2 | **7.0** | The row is badged `needs_review`, so the failure is visible |
| T-01 import backlog | 8.6 | **7.6** | Mechanism confirmed, **magnitude unresolved** |
| T-05 placeholder binary | 7.8 | **7.2** | Reproduced from a clean clone; the served page names the fix |
| T-11 Instagram timeouts | 7.4 | **7.4** | Rating held, but **the fix had to be rewritten** |
| T-16 status validation | 6.5 | **5.5** | Both reviewers moved and crossed; chair ruling |
| T-15 structured logging | 6.8 | **6.0** | Re-ordered: the hybrid's silent fallback is the worst of the three sites |
| T-25 request hardening | 5.4 | **4.8** | Double-counted the missing-auth severity |
| T-26 int32 offset | 5.2 | **3.0** | Needs a deliberately absurd input |
| T-32 prompt injection | 4.5 | **3.2** | No XSS sink, no second tool, closed schema |
| T-33 search N+1 | 4.5 | **3.0** | The reviewer withdrew its criticism of the code's own comment |
| T-64 govulncheck note | 0.5 | **removed** | A clean scan is not a finding |

## Effort key

**S** — under an hour · **M** — half a day · **L** — a day or more

## Suggested sequencing

Strict rating order is still not the best execution order:

1. **T-18 step 1 (E7, 5.5)** — make `LLMExtractor` injectable. One variadic
   parameter; it also supplies the timeout seam T-20 needs.
2. **T-10 (D3, 5.5)** — share the Postgres container. Now known to be 18
   containers, so the payoff is larger than revision 1 assumed, even though the
   rating fell.
3. **T-11 (I3, 7.4)** — bound the Instagram calls, but read the corrected fix
   first: the obvious approach does not exist in the dependency's API.

---

## Band ≥ 8.0 — fix first (1 task)

- [x] **T-02 · 8.5 · M** — Put authentication in front of the API, and tighten CORS
      *Closes:* `security` S1
      (1) a static bearer token from config on the mutating routes, compared with
      `subtle.ConstantTimeCompare`; (2) replace `Access-Control-Allow-Origin: *`
      with a config allowlist, echoing the request `Origin` only on a match, plus
      `Vary: Origin`; (3) default `HTTP_ADDR` to `127.0.0.1:8080`.
      `internal/api/middleware.go:42-52`, `internal/api/router.go:31-50`, `internal/config/config.go:18`
      **CORRECTED — worse than first written:** `handleCreateRecipe` never
      inspects `Content-Type` and `handleImportRun` reads no body, so a `POST`
      with `Content-Type: text/plain` is a CORS-*simple* request and skips
      preflight entirely. Step 2 alone does **not** close the write path; step 1
      is load-bearing.

## Band 7.0 – 7.9 (6 tasks)

- [x] **T-04 · 7.8 · M** — Derive a real confidence value instead of asserting 0.9
      *Closes:* `extraction` E2
      Add a `confidence` field to the `record_recipe` tool schema; combine the
      model's self-report with structural checks (ingredient count, instruction
      length, how many ingredients got an amount). Split the single threshold in
      two: one for LLM fallback, one for publish-without-review.
      `internal/extraction/llm.go:100-131`, `internal/pipeline/pipeline.go:91-94`
      *Precise claim:* `needs_review` is unreachable for any LLM result that
      actually contains a recipe — it remains reachable via the zero-confidence
      sentinel and the non-LLM paths.

- [x] **T-01 · 7.6 · L** — Import the backlog: page until overlap, not until a fixed count
      *Closes:* `integration` I1
      Persist a cursor (or stop paging on the first fully-`Skipped` page) so an
      account with more than 50 saved posts imports its history. Keep the per-run
      cap as a safety limit; drive the *stop* decision from overlap.
      `internal/instagram/fetcher_adapter.go:25-38`, `internal/instagram/saved.go:77-116`
      **CORRECTED — magnitude unresolved.** The mechanism is proven by code
      (`maxID` is function-local, no cursor is persisted anywhere, and
      `PostFetcher` has no return path for "already imported"). The claim that a
      500-post backlog leaves 450 unreachable additionally assumes a stable,
      newest-first server ordering — unverified. **Do T-43 to settle it.** The
      fix is correct either way; only the urgency is provisional.
      → Do T-11 first; this rewrites the same loop.
      **DONE — no cursor persisted; overlap handled by skipping.** `MaxItems`
      now counts only *new* posts: `FetchOptions.Known` (wired in `main.go` to
      `RecipeRepository.GetBySource`) makes the loop page past what is already
      imported, so a backlog drains across runs. A persisted cursor was
      rejected deliberately — there is no settings table to put one in, and
      resuming mid-feed would stop newly saved posts at the head from being
      seen. The cost is that a steady-state run re-walks known pages up to
      `MaxPages`, at the dependency's flat 1s pacing. **T-43 still settles the
      magnitude**; nothing in the fix depends on it.

- [x] **T-06 · 7.6 · M** — Re-authenticate when the Instagram session expires
      *Closes:* `integration` I2
      Detect the auth-failure class from `igerrors` and re-run `LoginOrRestore`
      **once** before failing the run, re-persisting the session. Not a retry
      loop — repeated logins are what Instagram flags.
      `internal/instagram/client.go:28-39`, `cmd/recipe-reader/main.go:71-77`

- [x] **T-11 · 7.4 · L** — Bound every Instagram call: timeout, cancellation, page cap
      *Closes:* `integration` I3, `go` #3, `integration` I5 (as a constraint)
      **CORRECTED — the obvious fix does not exist.** Revision 1 said to thread
      `ctx` into `PrivateRequest` or wrap with `http.NewRequestWithContext`.
      Verified against `instago@v1.0.2`: `PrivateRequest` has no ctx parameter,
      `PrivateRequestOpts` has no ctx field, the `*http.Client` is unexported
      (`client.go:41-42`), and there is no `SetHTTPClient` — the only transport
      hook is `SetProxy`, which builds its own transport. The real options are:
      a watchdog goroutine that leaks on a hang; a forward proxy via `SetProxy`
      that enforces the timeout; or a fork/upstream PR adding a context-aware
      entry point. Pick one deliberately.
      Locally, still do: thread `ctx` through `pageMedia`/`FetchSavedPosts`/
      `FetchCollectionPosts`, check `ctx.Err()` between pages, add `maxPages`.
      **Constraint from I5:** any timeout must exceed 60s, or it will fire on
      every 408 retry — the dependency sleeps 60s uninterruptibly at
      `client.go:555-559`.
      Note the compounding: `worker.go:53-60` only clears `running` when `Run`
      returns, so one hung fetch wedges every future import until restart.
      → **Unblocks T-01, T-12, T-14, T-19.**
      **DONE — watchdog chosen.** `Client.run` (`internal/instagram/client.go`)
      runs each call in a goroutine and stops waiting at a 2-minute deadline,
      above I5's 60s floor. The abandoned goroutine still writes to instago's
      shared state when it returns, so a one-slot channel admits a single call
      at a time: a caller that cannot get the slot fails with a diagnosable
      error rather than racing it, and the slot comes back on its own if the
      abandoned call finishes. `ctx` is threaded through `pageMedia`,
      `FetchSavedPosts`, `FetchCollectionPosts`, `ListCollections` and
      `ResolveCollectionID`, checked between pages, and `MaxPages` (default
      100) caps a run — which also closes **T-19**'s page cap.

- [x] **T-05 · 7.2 · S** — Make `make build` depend on the frontend
      *Closes:* `delivery` D1
      `build: frontend`, so a fresh clone cannot silently ship the placeholder.
      If `npm ci` on every build is too slow, add a loud guard instead — a marker
      file from the real Vite build, or a startup `slog.Warn` on a placeholder
      hash match. `make run` (`Makefile:36-37`) has the same missing prerequisite.
      `Makefile:16-17` (not 17-18), `internal/webui/embed.go:15`
      *Reproduced:* clean clone → `make build` → exit 0 → `strings` finds the
      placeholder in the binary.

- [x] **T-03 · 7.0 · S** — Stop importing captions that contain no recipe
      *Closes:* `extraction` E1
      Gate on confidence before `toRecipe`, and count the skip distinctly.
      Prefer a typed outcome (`ErrNoRecipe`) over the `NO_RECIPE_FOUND` string.
      `internal/pipeline/pipeline.go:66-82`, `internal/extraction/llm.go:18,129-131`
      **CORRECTED:** revision 1 claimed these rows leave orphan ingredient rows
      behind (linking this to T-08). They do not — the sentinel path returns an
      empty ingredients list. **The T-03 → T-08 link is withdrawn.**
      *Aggravator found in the round:* `source` is UNIQUE and the pipeline skips
      anything already present, so a junk row permanently blocks that post from
      being re-imported by a better extractor.

## Band 6.0 – 6.9 (4 tasks)

- [x] **T-23 · 6.4 · S** — Make `--extraction-mode` mean what it says
      *Closes:* `go` #6 — **RAISED in round 2** (chair ruling R6), the only
      finding both reporting reviewers independently nominated as under-rated.
      `EXTRACTION_MODE=llm` with a valid API key does not merely skip the LLM —
      it silently selects the *opposite* of what was asked, returning the
      weakest extractor on every post of every run while the paid key sits
      unused, and nothing logs the selected mode.
      Add `enum:"rule,llm,hybrid"` to the kong tag and switch on all three in
      `run()`; for `llm`, pass the LLM extractor as the sole extractor. Log the
      selected mode at startup.
      `internal/config/config.go:26`, `cmd/recipe-reader/main.go:61`
      **DONE.** `enum:"rule,llm,hybrid"` on the kong tag, all three branches in
      `newExtractor`, and the selected mode logged at startup. `llm` with no API
      key is now a **startup refusal**, not a silent downgrade — and extractor
      construction moved ahead of `db.Migrate` so a misconfiguration is not
      buried under a migration error. Verified in the built image.
      **This is a recorded, undecided defect that shipped.** Task 18's note 3
      says *"Open question — `EXTRACTION_MODE=llm` silently runs rules-only …
      needs a decision"*, and the task is marked `[x] done`. Make the decision.
      → Shares an edit site with T-18; do them together.

- [x] **T-12 · 6.2 · M** — Handle HTTP 429 as a rate limit, not a generic 4xx
      *Closes:* `integration` I4 — return collected posts with a typed
      `ErrRateLimited`, add a `Worker` cooldown, make `handleImportRun` refuse
      during it. `internal/instagram/saved.go:85-88`, `internal/pipeline/pipeline.go:49-52`
      **DONE.** `instagram.ErrRateLimited` covers all three throttle shapes
      (`ClientThrottled`, `RateLimitError`, `PleaseWaitFewMinutes`). `pageMedia`
      returns what it collected alongside any error and `Pipeline.Run` imports
      it before returning the error, so a throttled walk is not re-fetched after
      the cooldown. `Worker` stands down for 30 minutes; `POST /api/import/run`
      answers 429 with `Retry-After`; `Worker.Status()` became a struct.

- [x] **T-15 · 6.0 · M** — Add structured logging inside `internal/`
      *Closes:* `extraction` E8 (6.0), `go` #5 (5.8), `integration` I10 (3.6)
      One theme, three sites, three signals — the chair preserved the split
      because each needs a different one. **Order re-ranked:** the hybrid's
      swallowed LLM error is the worst of the three, because it is the only site
      invisible in *both* directions — the tally reports success while quality
      degrades, so an expired API key looks like a healthy run.
      (1) `internal/extraction/hybrid.go:34-37` — mark the degraded result;
      (2) `internal/pipeline/pipeline.go:58-82` — stage-tagged warn at all four
      `Failed` branches, plus a `ctx.Err()` check at the top of the loop;
      (3) `internal/instagram/saved.go:93-105` — a dropped-item counter, where
      `dropped > 0 && len(out) == 0` should be an error, not an empty success.
      **DONE, all three.** (1) `slog.Warn` plus a new `ExtractedRecipe.Degraded`
      flag, counted into `ImportResult.Degraded` and reported by the status
      endpoint and the import page — the "invisible in both directions" part of
      E8 needed a product-visible signal, not just a log line. (2) Four
      stage-tagged warns (`dedupe-lookup`, `extract`, `resolve-lookups`,
      `store`) plus a per-post `ctx.Err()` check. (3) `ErrSchemaDrift` when a
      page returns items and none are readable; a warn when only some are.

- [x] **T-18 · 6.0 · L** — Test the riskiest untested code
      *Closes:* `delivery` D5 (6.0), `extraction` E7 (5.5)
      **RESHAPED in round 2 — step 1 is already written.** The seam E7 asks for
      exists on the unmerged local branch `feat/provider-agnostic-llm-extractor`
      (commit `4bbe23e`): a 211-line `internal/extraction/llm_provider.go` plus a
      132-line `llm_provider_test.go` driving `Extract` against `httptest` stubs.
      Chair-verified caveats before you merge it:
      · it **does not compile against `main`** — the branch changes the
        constructor to `NewLLMExtractor(cfg LLMConfig) (*LLMExtractor, error)`
        while `cmd/recipe-reader/main.go:62` still calls the two-argument form,
        and the commit leaves the composition root untouched;
      · it **does not fix T-04 (E2)** — `Confidence: 0.9` and the `0` sentinel
        survive unchanged on the branch, deliberately;
      · Task 18's note 4 already records the branch as unmerged.
      So: (1) merge `4bbe23e` and rewire `main.go` — same edit site as T-23;
      (2) cover `pageMedia` against a fake (needs T-11's seam); (3) extract
      `run()`'s wiring into a testable `newServer(cfg)`.
      → The `WithRequestTimeout` seam T-20 needs comes with the merge.
      **DONE — ported rather than merged, and one round-2 claim is wrong.**
      · Step 1: `llm_provider.go` and `llm_provider_test.go` were taken from
        `4bbe23e` and `main.go` rewired, but not via `git merge`. The branch
        keeps `Confidence: 0.9` and the `NO_RECIPE_FOUND` string deliberately,
        and merging it as-is would have **reverted T-04 and T-03**. The seam is
        in; the confidence work sits on top; the shared `record_recipe` schema
        now carries the required `confidence` field for *both* transports,
        built from one property set so they cannot drift.
      · Step 2 was already closed by T-11's `requester` seam.
      · Step 3: `newExtractor(cfg)` and `newServer(cfg, deps)` are split out of
        `run()` and covered in `cmd/recipe-reader/main_test.go`.
      · **CORRECTION to round 2:** "the `WithRequestTimeout` seam T-20 needs
        comes with the merge" is false. `4bbe23e` adds no timeout option —
        `LLMConfig` is `{Provider, APIKey, Model, BaseURL}` and nothing on the
        branch bounds the model call. **T-20 still has to build its own seam.**
      · `docs/superpowers/plans/…/25-provider-agnostic-llm-extractor.md` is
        marked done, with the superseded Confidence requirement recorded.

## Band 5.0 – 5.9 (12 tasks)

- [ ] **T-19 · 5.8 · S** — Page cap and no-progress guard in the paging loop — `integration` I7 — `internal/instagram/saved.go:80-111`. Folds into T-01/T-11.
- [ ] **T-20 · 5.8 · S** — Timeout on the model call — `extraction` E6 — `internal/extraction/llm.go:74`. Use T-18's `WithRequestTimeout` seam.
- [ ] **T-21 · 5.6 · S** — Credentials environment-only, not CLI flags — `security` S2 — `internal/config/config.go:21-29`
- [ ] **T-22 · 5.6 · M** — Smoke-test the built image in CI — `delivery` D7 — `.github/workflows/ci.yml:83-90`. Covers T-05, migrations and embed wiring at once.
- [ ] **T-16 · 5.5 · S** — Validate `status`, in the handler **and** the schema — `go` #2 + `persistence` P7 (chair ruling R1)
      **Both halves or it is not done:** validate against the two domain values on POST *and* PUT (a non-empty bogus status like `"banana"` persists today on both paths), plus a `0002` migration adding `CHECK (status IN ('needs_review','published'))` preceded by a backfill. Validation alone leaves existing bad rows and leaves the invariant unenforced against psql and future writers.
      **CORRECTED:** a `status=''` row is *not* invisible in the list view — `web/src/pages/list.ts:54-58` sends no status filter, so it appears and silently reads as published.
- [ ] **T-10 · 5.5 · M** — One Postgres container per package, not per test — `delivery` D3
      `TestMain` per package + `TRUNCATE ... RESTART IDENTITY CASCADE` between tests. **CORRECTED: 18 containers, not 8** — five `testdb.New` call sites sit inside `t.Helper()` wrappers that several tests each invoke. Rating fell (it fails loudly and hits only developers) while the payoff rose.
- [ ] **T-14 · 5.4 · S** — Let a failed Instagram login recover without a restart — `integration` I8 — `cmd/recipe-reader/main.go:71-87`
- [ ] **T-27 · 5.2 · S** — `-race` in `make test` — `delivery` D4 — `Makefile:20-21`. Nearly free after T-10.
- [ ] **T-08 · 5.0 · M** — Resolve lookups inside the recipe transaction — `persistence` P2 — `internal/repository/lookup_repository.go:28`. Note the T-03 link is withdrawn; this stands on its own.
- [ ] **T-28 · 5.0 · S** — Wire `MaxItemsPerRun` to a flag — `integration` I6 — `cmd/recipe-reader/main.go:75`
- [ ] **T-17 · 5.0 · S** — Lifecycle owner for the detached import goroutine — `go` #4 — `internal/api/handlers_import.go:24-25`
      **CORRECTED in round 2:** this is *not* a use-after-close. `puddle/v2@v2.2.2/pool.go:179-195` destroys only idle resources, leaving an in-flight connection untouched, and the process usually exits first. The rating holds at 5.0 on different reasoning — high likelihood, small blast radius, **zero diagnosability**, since the truncated run is never reported.
- [ ] **T-29 · 5.0 · S** — Cap caption length (by runes) — `extraction` E3 — `internal/extraction/llm.go:83`

## Band 4.0 – 4.9 (10 tasks)

- [ ] **T-25 · 4.8 · S** — Bound request bodies and set server timeouts — `security` S3 + `go` #8
      **Two edits in two files:** the four timeout fields on `cmd/recipe-reader/main.go:101`, and `http.MaxBytesReader` at `internal/api/handlers_recipes.go:77` and `:111` (→ 413, not 400).
- [ ] **T-30 · 4.8 · S** — Stop echoing internal error strings — `security` S4 — `internal/api/handlers_import.go:48`
- [ ] **T-31 · 4.8 · S** — Escape `LIKE` metacharacters — `persistence` P3 — `internal/db/queries/recipes.sql`
- [ ] **T-34 · 4.5 · S** — Validate `--import-interval` and the threshold — `go` #7 — `internal/pipeline/worker.go:36`
- [ ] **T-24 · 4.8 · S** — Default to Haiku 4.5 for batch extraction — `extraction` E4 — `internal/extraction/llm.go:16`, `internal/config/config.go:30`. **Lowered in round 2** by its own reviewer: this is the only cost finding whose failure announces itself, on the first invoice. Settle the model choice with T-42's gold set.
- [ ] **T-35 · 4.4 · S** — `Temperature: 0` for extraction — `extraction` E5 — `internal/extraction/llm.go:74-85`
- [ ] **T-36 · 4.4 · S** — Stamp a version into the binary — `delivery` D8 — `Makefile:16-17`, `Dockerfile:20`
- [ ] **T-37 · 4.2 · S** — Configure the connection pool — `persistence` P5 — `internal/db/connect.go:28`
- [ ] **T-07 · 4.0 · S** — Make the insert itself the dedupe — `persistence` P1
      `ON CONFLICT (source) DO NOTHING` on `CreateRecipe`; treat `pgx.ErrNoRows` as `Skipped`. **CORRECTED:** the race is latent, not live — the worker mutex closes the import-vs-import path and `createRecipe` (`web/src/api.ts:49`) has no caller in the shipped UI. Still worth doing: the fix is simpler and one query cheaper than the check-then-act it replaces.
- [ ] **T-38 · 4.0 · S** — Measure coverage, report it, do not gate on it — `delivery` D6

## Band 3.0 – 3.9 (12 tasks)

- [ ] **T-42 · 3.8 · L** — Gold-set eval for extraction — `extraction` E11. Settles T-24 and T-40.
- [ ] **T-39 · 3.6 · S** — Harden the compose Postgres defaults — `security` S6 — `docker-compose.yml:4-11`
- [ ] **T-43 · 3.4 · M** — Verify the Instagram endpoints against a real account — `integration` I11
      **Promoted in importance by the round:** this is what settles T-01's unresolved magnitude. Commit the response as a fixture; it also gives T-18 a realistic case.
- [ ] **T-40 · 3.4 · S** — Continuous rule-confidence score — `extraction` E9 — `internal/extraction/rules.go:116-125`
- [ ] **T-44 · 3.4 · S** — Include the frontend in `make check` — `delivery` D9
- [ ] **T-32 · 3.2 · S** — Delimit the untrusted caption in the prompt — `security` S5
      Also validate returned categories against the seeded set. *Found in the round:* injected captions pollute **three** shared lookup tables, not one.
- [ ] **T-41 · 3.2 · S** — Better recipe title than "first line" — `extraction` E10 — `internal/extraction/rules.go:61,75-82`
- [ ] **T-45 · 3.0 · S** — Replace the search `DISTINCT` with a semi-join — `persistence` P8
- [ ] **T-46 · 3.0 · M** — Typed structs for Instagram responses — `go` #14 — `internal/instagram/saved.go:89-134`
- [ ] **T-47 · 3.0 · S** — Pin `sqlc` — `delivery` D11 **(narrowed)**
      The golangci-lint and govulncheck halves are withdrawn as policy-aligned. What holds: `Makefile:11` regenerates with an unpinned local binary while `ci.yml:33` uses `sqlc@latest` and fails on any byte difference. The project already pins Go at `ci.yml:26`, so this is consistent with its own practice.
- [ ] **T-26 · 3.0 · S** — Clamp `page` before the `int32` conversion — `go` #1 — `internal/repository/recipe_repository.go:198`. Overflow begins at `page=107374184` with the default page size.
- [ ] **T-33 · 3.0 · M** — Batch the search's child queries — `persistence` P4 — `= ANY($1::bigint[])`. Keep on file until profiling says so.

## Band 2.0 – 2.9 (15 tasks)

- [ ] **T-48 · 2.8 · S** — Security headers on the frontend handler — `security` S7
- [ ] **T-49 · 2.8 · S** — Log the response status in `withLogging` — `go` #12
- [ ] **T-50 · 2.8 · S** — `HEALTHCHECK` in the image and on the compose `app` service — `delivery` D13
- [ ] **T-54 · 2.6 · S** — Test the down migration (up → down → up) — `persistence` P11 + `delivery` D12
      Delivery owns it; lands inside T-10's `TestMain`. **Not just a test:** `internal/db` exports no Down at all, so this needs a new entry point in `connect.go`.
- [ ] **T-51 · 2.6 · S** — Keyword-match categories in rules-only mode — `extraction` E12
- [ ] **T-13 · 2.5 · S** — Fix the Instagram client test's name and cover the restore branch — `delivery` D2 + `integration` I12
      **CORRECTED — premise withdrawn.** Revision 1 said this test makes a real network call on every run. It does not and never has: `instago@v1.0.2/auth.go:48-50` returns `BadCredentials` before any request is built, verified independently by two reviewers including a dead-proxy run. **The suite is already hermetic.** What remains is Low: the name `RestoresExistingSession` asserts the *missing*-session path, `err == nil` is near-tautological, and `client.go:29-31` has zero coverage.
- [ ] **T-52 · 2.5 · S** — Log the discarded `LoadSettings` error — `go` #13
- [ ] **T-53 · 2.5 · S** — `RunOnce` should not return a stale result when it skips — `go` #10
- [ ] **T-55 · 2.4 · S** — Decide whether duplicate ingredient rows are meaningful — `persistence` P9
- [ ] **T-56 · 2.4 · S** — Cache the resolved collection id — `integration` I9
- [ ] **T-57 · 2.2 · S** — Record retention/provenance intent in `docs/PROJECT.md` — `security` S8
- [ ] **T-58 · 2.2 · S** — Re-panic on `http.ErrAbortHandler` — `go` #11
- [ ] **T-59 · 2.2 · S** — Drop the fixed `/tmp` path from `make lint` — `delivery` D10
- [ ] **T-60 · 2.0 · S** — Optional: read-then-insert for `FindOrCreate*` — `persistence` P6
- [ ] **T-61 · 2.0 · S** — `dtoToRecipe` should take `context.Context` — `go` #9

## Band < 2.0 (2 tasks)

- [ ] **T-62 · 1.5 · S** — Accept or document the count/page inconsistency — `persistence` P10 (no action recommended)
- [ ] **T-63 · 1.2 · S** — Doc comments on `LookupRepository`; drop five redundant filename comments — `go` #15

*Removed in revision 2:* T-64 (govulncheck advisory) — withdrawn by
`security_reviewer` under cross-examination. A clean scan is a scan result, not
a finding. The GO-2026-5932 provenance note lives in the security review's
"What's right" section, carrying no rating.

---

## Progress

| Band | Tasks | Done |
| --- | --- | --- |
| ≥ 8.0 | 1 | 1 |
| 7.0 – 7.9 | 6 | 6 |
| 6.0 – 6.9 | 4 | 4 |
| 5.0 – 5.9 | 12 | 0 |
| 4.0 – 4.9 | 10 | 0 |
| 3.0 – 3.9 | 12 | 0 |
| 2.0 – 2.9 | 15 | 0 |
| < 2.0 | 2 | 0 |
| **Total** | **62** | **11** |
