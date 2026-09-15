# Recipe Reader — Working Plan

**Derived from:** the six-reviewer panel of 2026-09-11 ([index](README.md)),
as adjudicated in the [consensus record](2026-09-11-consensus.md)
**Source findings:** 66 post-round (72 entering, 1 withdrawn, 5 merged), **62 tasks**
**Status:** in progress — bands ≥ 6.0 done, plus T-25 from Stage 5 (12 of 62)

Mark a task `[x]` when it is done. Each task cites the finding IDs it closes.

> **Revision 4 — after the first eleven tasks shipped (2026-09-12).** Bands
> ≥ 6.0 are complete. This revision does three things: records what each closed
> task actually did where it diverged from the plan, **re-cites every task whose
> file and line references the shipped work invalidated**, and replaces the
> three-line sequencing note with a full [execution order](#execution-order)
> derived from the consensus record. One round-2 claim was found to be false and
> is corrected at T-20. Of the six unanswered round-2 challenges, two are
> dissolved and one is partly answered by demonstration; three are still open —
> see [Open panel questions](#open-panel-questions).
>
> **If you read revision 3, re-read T-19, T-20, T-21, T-22, T-24, T-25, T-28,
> T-29, T-30, T-32, T-34, T-35, T-46 and T-53** — every one of them changed
> location, scope, or both.
>
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

## What shipping the first eleven changed in this plan

Ratings are untouched — none of this is a re-rating. What moved is where the
code lives and how much of each task is left.

| Task | Change | Why |
| --- | --- | --- |
| T-19 page cap + progress guard | **half closed** by T-11 | The cap shipped as `MaxPages`; the guard did not, and the gap now has a named cost — a stuck cursor re-appends the same posts and spends an LLM call each time |
| T-20 model-call timeout | **round-2 claim corrected** | `4bbe23e` supplied no `WithRequestTimeout`; the seam still has to be built, and the call is now two calls in `llm_provider.go` |
| T-21 credentials env-only | **two flags → four** | T-02 added `--api-token`, T-18 added `--llm-api-key`, both flag *and* env |
| T-22 CI image smoke test | **covers → guards** | T-05 shipped, so this now protects it from regressing rather than substituting for it |
| T-24 cheaper default model | **two sites → three** | `LLM_MODEL` takes precedence over `ANTHROPIC_MODEL` and has no default of its own |
| T-25 request hardening | **re-cited** | The `http.Server` moved into `newServer`, which a test can now call |
| T-28 per-run fetch bounds | **one knob → two** | T-11 added `MaxPages` beside `MaxItems`; neither is configurable |
| T-29 caption cap | **simpler** | One edit in `Extract`, not one per transport |
| T-32 prompt delimiting | **one prompt, two transports** | And T-04 gave the model a `confidence` field that is itself an injection surface |
| T-34 validate durations | **two thresholds → three** | T-04 added `EXTRACTION_PUBLISH_THRESHOLD` |
| T-35 `Temperature: 0` | **one edit → two** | Both transports set params independently |
| T-46 typed structs | **order fixed** | Do it after T-43, whose fixture says which fields are optional |
| T-53 stale `RunOnce` result | **one skip path → two** | T-12 added the cooldown skip |
| T-54 down-migration test | **order fixed** | After T-16, so it exercises `0002` and not only `0001` |
| T-07 dedupe | **scope clarified** | T-01 added a second, deliberately advisory dedupe read; this task is only about the pipeline's check-then-act |

## Effort key

**S** — under an hour · **M** — half a day · **L** — a day or more

## Execution order

Rating order is not execution order. The consensus record says why in four
places, and those four rulings — not the decimals — are what orders the 51
remaining tasks.

**The constraints, quoted from the record:**

1. **Group by site, not by rating.** Ruling R2 declined to collapse one root
   cause into one task, keeping it as *"one remediation item, three site
   ratings"*. The converse applies to what is left: most remaining tasks cluster
   into six files, and reading each file once beats reading it six times. The
   chair's closing note supports this — the panel was *"strong on mechanism and
   weak on proportion"*, so mechanism is the more reliable thing to sort by.
2. **One cheap task can still close an open panel question.** The round's only
   recorded dissent is I1's magnitude, and *"Resolution: none available in this
   round. It requires one live run against a real account, which is I11's
   remediation (plan task T-43)."* **T-43 is rated 3.4 and is the most
   informative task left on the board.**
3. **Two tasks are gates.** D3's own argument is that 18 containers are *"the
   reason nobody will add the integration tests D5 asks for, because each one
   costs another container"* — T-10 makes every later test cheaper, and T-27
   and T-54 ride on it directly. Ruling R7 settles E4 *"with T-42's gold set"*,
   so T-42 gates T-24 and T-40.
4. **Two tasks are indivisible.** R1: T-16 is *"both halves or it is not done"*.
   R4: T-25 is *"two edits in two files, not one"*.

### Stage 1 — unblock the test suite (do this first)

Everything after it gets cheaper. **T-10** (5.5) → **T-27** (5.2, *"nearly free
after T-10"*). T-54 also lands here mechanically but should wait for Stage 3, so
it exercises the `0002` migration T-16 adds rather than only `0001`.

### Stage 2 — settle the record

**T-43** (3.4, needs a real Instagram account). It is the only task that can
close the panel's open dissent, and its committed fixture is what makes T-19's
no-progress guard and T-46's typed structs verifiable against real response
shapes instead of guessed ones. Out of order by rating, first by value.

### Stage 3 — persistence and the schema (migration-bearing, so it fixes an order)

**T-16** (5.5, both halves, adds `0002`) → **T-54** (2.6, up→down→up across both
migrations) → **T-08** (5.0) → **T-07** (4.0). The last two are the open
`persistence` challenges from round 2: R1 is ratified by `go` only, and P1's
over-correction from 7.2 to 4.0 was never checked.

### Stage 4 — one sitting in `internal/extraction`

**T-20** (5.8) → **T-29** (5.0) → **T-35** (4.4) → **T-32** (3.2), then
**T-42** (3.8, L) and the two it settles, **T-24** (4.8) and **T-40** (3.4),
then **T-41** (3.2) and **T-51** (2.6). Read the re-cited locations first: the
LLM code moved into `llm_provider.go` and two of these are now two-transport
edits.

### Stage 5 — one sitting in `internal/config` and the composition root

**T-21** (5.6, now four flags, not two) → **T-28** (5.0) → **T-34** (4.5) →
**T-14** (5.4) → **T-17** (5.0) → ~~**T-25** (4.8, the two-file one)~~ **done**,
taken ahead of the rest of the stage — both halves shipped together, as R4
requires.

### Stage 6 — one sitting in `internal/api`

**T-30** (4.8) → **T-48** (2.8) → **T-49** (2.8) → **T-58** (2.2) → **T-61**
(2.0). T-25's `MaxBytesReader` half is no longer waiting here; it shipped with
the timeouts.

### Stage 7 — one sitting in `internal/repository` and the queries

**T-31** (4.8) → **T-37** (4.2) → **T-45** (3.0) → **T-26** (3.0) → **T-55**
(2.4) → **T-60** (2.0). **T-33** (3.0) stays on file: the plan's own note says
keep it until profiling says otherwise.

### Stage 8 — delivery and CI sweep

**T-22** (5.6) first — it now *guards* the finished T-05 rather than covering
it. Then **T-36** (4.4) → **T-38** (4.0) → **T-39** (3.6) → **T-44** (3.4) →
**T-47** (3.0) → **T-50** (2.8) → **T-59** (2.2).

### Stage 9 — `internal/instagram` cleanup (after Stage 2's fixture)

**T-19** (5.8, half already done) → **T-46** (3.0) → **T-52** (2.5) → **T-53**
(2.5) → **T-13** (2.5) → **T-56** (2.4).

### Stage 10 — nits

**T-57** (2.2) → **T-62** (1.5) → **T-63** (1.2).

## Open panel questions

Round 2 ended with **six** challenges unanswered — the "Outstanding" table in the
consensus record. Shipping the first eleven tasks did not answer any of them the
way the panel would have; it dissolved two, answered part of a third by
demonstration, and left three untouched. Recording which is which, so nobody
re-opens a settled one or assumes a live one is settled:

| Reviewer | Challenge | State |
| --- | --- | --- |
| `security` | Should S1 rise above 9.0? *"Does the panel rate the shipped default or the cautious one?"* | **Dissolved.** T-02 made the shipped default the cautious one: a loopback bind, and a refusal to start on any other address without a token. The two readings no longer differ. |
| `integration` | I2 (7.6) has never been challenged in either round | **Dissolved.** T-06 fixed it regardless of what a challenge would have concluded. |
| `delivery` | Ratify or reject R3 at 2.5; defend D3's count rising 8→18 while its rating fell 7.0→5.5; whether D5 is a legitimate finding or an aggregate needing a split | **Partly answered — by the work, not by the panel.** D5 *was* an aggregate: T-18 closed it as three independent pieces, two of which other tasks had already covered. R3 is moot for remediation (T-13 is Low either way). **D3's count is still undefended, and T-10 will settle it empirically.** |
| `integration` | Settle I1's magnitude — hold 7.6 provisional, split it, or drop to the confirmed-only rating | **OPEN.** Only T-43 can close it. The 7.6 stays provisional; the fix shipped anyway, because nothing in it depended on the magnitude. |
| `persistence` | Ratify or reject R1 at 5.5; over-correction check on P1 (7.2→4.0); P3-vs-S4 calibration at 4.8 | **OPEN.** R1 stands ratified by one party of two. T-16 is written to its condition regardless, and Stage 3 is where all three get tested in practice rather than in argument. |
| `integration` | Lane bias: it owns 3 of 7 findings ≥ 7.0, all in one package. Untested | **OPEN, and now harder to test.** All three — I1, I2 and I3, shipped as T-01, T-06 and T-11 — are fixed. Whether that lane was over-weighted is no longer observable from the code; only from whether the fixes turn out to have mattered. |

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

- [x] **T-19 · 5.8 · S** — Page cap and no-progress guard in the paging loop — `integration` I7
      **HALF DONE by T-11, and the remaining half has a named consequence.**
      The page cap shipped: `FetchOptions.MaxPages` (default 100) bounds every
      walk. The **no-progress guard did not**, and `pageMedia`
      (`internal/instagram/saved.go`) still trusts the cursor: if the endpoint
      returns a `next_max_id` equal to the `max_id` just sent, the loop
      re-requests the same page, and because those posts are not yet in the
      database `FetchOptions.Known` reports them as new, so **they are appended
      again on every pass until `MaxItems` or `MaxPages` stops it**. Not
      corruption — the pipeline dedupes on `source`, so the repeats land as
      `Skipped` — but it inflates `Seen` and spends a paid LLM call per
      duplicate. Fix: stop when `next == maxID`. Verify against T-43's fixture.
      **DONE, slightly wider than written — and the cost claim above corrected.**
      (1) The walk ends on any cursor **already seen in this run**, not only
      `next == maxID`. A cycle back to an older cursor is the same fault one
      step removed, and a last-cursor check would page through it until
      `MaxPages`. It logs a warning, because only a misbehaving server trips it.
      (2) A post already collected in this run is skipped, so overlapping pages
      — ordinary on a feed that shifts while it is walked — do not hand the
      pipeline the same post twice. **Correction:** a repeat does *not*
      generally spend an LLM call. The pipeline runs `GetBySource` per post
      before extracting, sequentially, so the first copy is stored before the
      second is checked and the repeat is `Skipped` for free. The exceptions
      are posts that store nothing — no recipe, or a failed store — which were
      extracted again on every repeat. What every repeat did cost was a slot in
      `MaxItems`. Tested against a synthetic feed with a stuck cursor, a cycle
      and an overlap — **not yet against T-43's fixture**, which does not exist
      until T-43 runs.
- [x] **T-20 · 5.8 · S** — Timeout on the model call — `extraction` E6
      **RE-CITED, and round 2 was wrong about this one.** The claim that *"the
      `WithRequestTimeout` seam T-20 needs comes with the merge"* is false:
      `4bbe23e` added no timeout option, and the ported code has none either.
      The call is no longer at `llm.go:74` — it is two calls, one per transport,
      at `internal/extraction/llm_provider.go` (`anthropicClient.recordRecipe`
      and `openAIClient.recordRecipe`). **Build the seam once rather than twice:**
      add a `Timeout` field to `LLMConfig` and apply it in
      `LLMExtractor.Extract` (`internal/extraction/llm.go`), which is the single
      point both transports pass through.
      **DONE, as described, plus one addition.** `LLMConfig.Timeout` is applied
      once in `Extract` with `context.WithTimeout`, so it bounds the SDKs'
      retries too; E6's 60s is the default. **The addition: it is configurable**
      (`LLM_TIMEOUT` / `--llm-timeout`). A fixed 60s would break the supported
      local-model case on the `openai` transport, since an Ollama on CPU can
      legitimately take longer. A timed-out call wraps
      `context.DeadlineExceeded`: in `hybrid` mode the post falls back to the
      rules and counts as degraded (`hybrid.go`), in `llm` mode it counts as
      failed, and the run moves on either way. Tested against a client that
      never answers, with an outer 5s guard so a regression fails the test
      rather than hanging it.
- [x] **T-21 · 5.6 · S** — Credentials environment-only, not CLI flags — `security` S2 — `internal/config/config.go`
      **SCOPE GREW: four flags now, not two.** S2 named
      `--instagram-password` and `--anthropic-api-key`. Since then T-02 added
      `--api-token` and T-18 added `--llm-api-key`, both as flag *and*
      environment, consistent with the rest of the file — which means both are
      visible in `ps aux` exactly as S2 describes. Sweep all four together.
      **DONE — and S2's proposed mechanism would not have worked.** S2 said to
      *"drop `name:` on both so they become environment-only — kong supports
      this"*. It does not: kong v1.16.1 names an untagged field's flag after the
      field (`build.go:162-164`), so dropping the tag leaves
      `--instagram-password` exactly where it was. The four fields are now
      `kong:"-"`, and `load` reads them from the environment in
      `readCredentials`. `--help`'s description names them, because kong has no
      flag entry to list them under. Tests: all four flags are refused as
      unknown, all four arrive from the environment, and the description names
      each one. Nothing in the repository used the flag form.
- [x] **T-22 · 5.6 · M** — Smoke-test the built image in CI — `delivery` D7 — `.github/workflows/ci.yml`
      **Now guards T-05 rather than covering it.** T-05 shipped, so the
      placeholder can no longer reach a `make build` artifact — but nothing in
      CI would catch it regressing, and `webui.IsPlaceholder()`'s startup
      warning is only visible to someone reading the log. Run the image and
      assert `/api/healthz` plus a `/` body that is not the placeholder.
      **DONE, and shown to fail when it should.** `scripts/smoke-test-image.sh`
      starts the image on a private network against a throwaway Postgres. It
      requires `/api/healthz` to answer ok; `/api/recipes` to answer, because
      liveness touches no dependency and this is what proves the migrations
      ran; and `/` to differ from `internal/webui/placeholder/index.html`. It
      compares against that file rather than a marker string, so rewording the
      placeholder cannot quietly disable the check. CI runs it after the build,
      and it runs identically locally. **Verified both ways:** the real image
      passes, and an image built without the frontend fails with "the image
      serves the placeholder frontend, not a real build". **Not done:** D7's
      second half, pushing the image to a registry on `main`. That needs
      registry credentials and `packages: write`, and the plan scoped this
      task to the smoke test.
- [x] **T-16 · 5.5 · S** — Validate `status`, in the handler **and** the schema — `go` #2 + `persistence` P7 (chair ruling R1)
      **Both halves or it is not done:** validate against the two domain values on POST *and* PUT (a non-empty bogus status like `"banana"` persists today on both paths), plus a `0002` migration adding `CHECK (status IN ('needs_review','published'))` preceded by a backfill. Validation alone leaves existing bad rows and leaves the invariant unenforced against psql and future writers.
      **CORRECTED:** a `status=''` row is *not* invisible in the list view — `web/src/pages/list.ts:54-58` sends no status filter, so it appears and silently reads as published.
      **DONE, both halves.** (1) `domain.RecipeStatus.Valid()`. POST validates
      *after* its existing default to `published`, so an omitted status still
      creates; PUT requires a legal status **and a non-empty name** — go #2's
      other half. PUT is a replacement, so an omitted field there meant "store
      empty", which is how `''` got in. (2) `0002_recipe_status_check`
      backfills out-of-domain rows to `needs_review` — P7's choice, the state
      that puts a person in front of them — and adds the CHECK in one
      transaction. `TestMigration0002_BackfillsThenEnforcesStatus` stops a
      separate database at `0001`, writes `'banana'` and `''` rows, migrates
      over them, checks the backfill and the `23514` refusal, then migrates
      back down, so the down file is exercised ahead of T-54.
- [x] **T-10 · 5.5 · M** — One Postgres container per package, not per test — `delivery` D3
      `TestMain` per package + `TRUNCATE ... RESTART IDENTITY CASCADE` between tests. **CORRECTED: 18 containers, not 8** — five `testdb.New` call sites sit inside `t.Helper()` wrappers that several tests each invoke. Rating fell (it fails loudly and hits only developers) while the payoff rose.
      **DONE — 27 container boots to 4, and 43s of wall time to 5s.**
      `testdb.New` now starts one container per test binary, lazily on first
      use, and on every call truncates each application table with
      `RESTART IDENTITY`. The table list is read from `pg_tables`, so a table a
      later migration adds is covered without editing the helper. A `TestMain`
      in each of the four database packages calls `testdb.Main`, which
      terminates the container. **No call site changed.**
      `TestTestDBNew_ResetsRowsAndSequences` guards the reset.
      **D3's count, settled by measurement.** Counted with a live
      `docker events` stream — `--since` replays from a bounded buffer and
      under-read the same run as 17: **21** boots at `4ce206e`, the last commit
      before any remediation, so the corrected 18 was itself an undercount; and
      **27** when this task started, because the ≥ 6.0 work added database
      tests at a container apiece — exactly D3's prediction.
- [x] **T-14 · 5.4 · S** — Let a failed Instagram login recover without a restart — `integration` I8 — `cmd/recipe-reader/main.go:71-87`
      **DONE — the worker is built whenever an account is configured.** A
      failed startup login used to leave the fetcher nil, so no worker existed
      and both import routes answered 503 until a restart. `newFetcher`
      (`cmd/recipe-reader/fetcher.go`) now returns the fetcher *and* the login
      error. `run()` builds the worker regardless and records the error with
      `Worker.RecordFailure`, so `/api/import/status` shows it at once rather
      than after the first scheduled run, hours later. A 503 now means only "no
      account configured", which makes I8's two states distinguishable.
      **How the retry happens.** T-06's re-login path was not enough on its
      own. `LoginOrRestore` already kept the credentials, but a client with no
      session would have sent an unauthenticated request and relied on this
      unofficial endpoint answering `login_required` to reach the re-login —
      which nobody has verified. `Client` now tracks whether it holds a session,
      and `do` logs in *first* when it does not, under the same 15-minute floor
      that rations every login. The tests run offline: instago refuses an empty
      password before building a request, so a failed startup login needs no
      network. Both paths are asserted: refused by the floor, and a login
      attempted before any request.
- [x] **T-27 · 5.2 · S** — `-race` in `make test` — `delivery` D4 — `Makefile:20-21`. Nearly free after T-10.
      **DONE, and it was.** `make test` runs `go test -race ./...`, matching
      CI. With T-10 in, the race-instrumented suite takes 46s wall including
      the instrumented compile — against 43s for the uninstrumented suite
      before T-10.
- [x] **T-08 · 5.0 · M** — Resolve lookups inside the recipe transaction — `persistence` P2 — `internal/repository/lookup_repository.go:28`. Note the T-03 link is withdrawn; this stands on its own.
      **DONE — by moving resolution into the write, not by `WithTx`.** P2
      proposed a `LookupRepository.WithTx(pgx.Tx)` with the transaction opened
      in the handler and the pipeline. That would have put a `pgx.Tx` in both
      upper layers to fix one repository's boundary. Instead
      `RecipeRepository.Create` and `Update` resolve any category, ingredient or
      unit given **by name with no id** on the queries of the transaction they
      already open; an id still wins, so a recipe loaded through `GetByID`
      round-trips unchanged. The handler's `dtoToRecipe` and the pipeline's
      `toRecipe` now map names only, and `Pipeline.Lookups` is gone. The
      caller's recipe is updated **only on commit** — before, `Create` set
      `recipe.ID` ahead of a commit that could still fail.
      *Side effect on T-15:* the pipeline's `resolve-lookups` log stage folds
      into `store`, since both now fail inside one call.
      *Tests:* `TestRecipeHandlers_FailedCreateLeavesNoOrphanLookups` is P2's
      duplicate-source scenario through the API. The repository test places the
      failure *after* resolution, a foreign-key violation on a missing unit id,
      so only the transaction can take the new rows back; it also checks the
      caller's value is left as passed.
- [x] **T-28 · 5.0 · S** — Wire the per-run fetch bounds to flags — `integration` I6
      **RE-CITED, and there are two knobs now.** `MaxItemsPerRun` became
      `instagram.FetchOptions.MaxItems`, and T-11 added `MaxPages` beside it.
      Both are set only from their package defaults (50 and 100) at
      `cmd/recipe-reader/main.go`, and neither is reachable from configuration.
      A backfill is exactly when an operator wants to raise them.
      **DONE, both knobs, and a zero is refused.** `IMPORT_MAX_ITEMS` /
      `--import-max-items` (default 50) and `IMPORT_MAX_PAGES` /
      `--import-max-pages` (default 100) match the package defaults and reach
      `FetchOptions` in `newFetcher`. **One addition:** `Config.validate`
      rejects either below 1. `FetchOptions.withDefaults` silently replaces a
      zero with the default, so `IMPORT_MAX_ITEMS=0` — plausibly meant as
      "pause imports" — would have imported fifty posts. Tested: defaults, env
      and flag overrides, the refusal, and the values reaching the fetcher.
      **Still open:** `.env.example` does not list them, and could not be
      edited in this session; it needs a manual update.
- [ ] **T-17 · 5.0 · S** — Lifecycle owner for the detached import goroutine — `go` #4 — `internal/api/handlers_import.go` (`context.WithoutCancel` + `go d.Worker.RunOnce`)
      **CORRECTED in round 2:** this is *not* a use-after-close. `puddle/v2@v2.2.2/pool.go:179-195` destroys only idle resources, leaving an in-flight connection untouched, and the process usually exits first. The rating holds at 5.0 on different reasoning — high likelihood, small blast radius, **zero diagnosability**, since the truncated run is never reported.
- [x] **T-29 · 5.0 · S** — Cap caption length (by runes) — `extraction` E3
      **RE-CITED, and simpler than it was.** The caption no longer reaches the
      API at `llm.go:83`; it enters at `LLMExtractor.Extract`
      (`internal/extraction/llm.go`) and is handed to whichever transport is
      configured. Cap it there — **one edit, not one per provider**. Shares a
      sitting with T-20, which wants the same function.
      **DONE.** `capCaption` (`internal/extraction/caption.go`) cuts to
      `maxCaptionRunes` = 8000 (E3's figure), by runes, and logs a warning when
      it cuts — a caption that long is not a real Instagram caption. It is
      tested on four-byte emoji for exact rune count and valid UTF-8. `Extract`
      applies it on the line every transport passes through, which a
      capturing fake confirms.

## Band 4.0 – 4.9 (10 tasks)

- [x] **T-25 · 4.8 · S** — Bound request bodies and set server timeouts — `security` S3 + `go` #8
      **Two edits in two files** (chair ruling R4): the four timeout fields on
      the `http.Server`, and `http.MaxBytesReader` at both `json.NewDecoder`
      sites in `internal/api/handlers_recipes.go` (→ 413, not 400).
      **RE-CITED:** the server is no longer built in `run()` — T-18 moved it to
      `newServer` in `cmd/recipe-reader/main.go`, which is also now a function a
      test can call, so the timeout fields are assertable.
      **DONE, both halves in one change.** (1) `newServer` sets
      `ReadHeaderTimeout` 10s, `ReadTimeout` 30s, `WriteTimeout` 60s and
      `IdleTimeout` 120s. S3's figures were taken for the first two; two
      deliberate departures: `WriteTimeout` — which S3 did not name — is set
      *above* `ReadTimeout`, because net/http starts it when the headers are
      read, so it has to outlast a body read the server still allows; and
      `IdleTimeout` is set on its own rather than left to inherit `ReadTimeout`.
      `TestNewServer_SetsTimeouts` asserts all four and both orderings. (2) Both
      decoder sites go through one `decodeRecipeBody` helper capping the body at
      1 MiB, where a `*http.MaxBytesError` answers **413** and anything else
      stays 400. Tested one byte over the cap on POST and PUT, and exactly at
      it, without a database — the cap fires before any repository is touched.
      **Verified in the built image** (`docker compose up --build`): a 2 MiB
      POST answers 413 with `Connection: close`, a normal POST still answers
      201, and a client that never finishes its headers is dropped at 10.0s.
      **Not done, deliberately:** S3's aside about `DisallowUnknownFields`. The
      reviewer called it *"not a vulnerability"*, it is not one of R4's two
      edits, and it changes what the API accepts from every client — a contract
      decision, not a hardening one.
- [ ] **T-30 · 4.8 · S** — Stop echoing internal error strings — `security` S4 — `internal/api/handlers_import.go`
      Still live: `handleImportStatus` returns `status.LastErr.Error()` verbatim,
      which now carries wrapped DSN fragments and `instago` response bodies.
      **Leave the rate-limit message alone** — T-12's cooldown string is composed
      for the caller and leaks nothing.
- [ ] **T-31 · 4.8 · S** — Escape `LIKE` metacharacters — `persistence` P3 — `internal/db/queries/recipes.sql`
- [ ] **T-34 · 4.5 · S** — Validate `--import-interval` and the thresholds — `go` #7 — `NewWorker` in `internal/pipeline/worker.go` still takes any duration and `Start`'s ticker panics on a non-positive one. **There are three thresholds to validate now:** T-04 added `EXTRACTION_PUBLISH_THRESHOLD` beside `EXTRACTION_CONFIDENCE_THRESHOLD`, and neither is range-checked. Shares a sitting with T-21 and T-28 in `internal/config`.
- [ ] **T-24 · 4.8 · S** — Default to Haiku 4.5 for batch extraction — `extraction` E4
      **Lowered in round 2** by its own reviewer: this is the only cost finding
      whose failure announces itself, on the first invoice. Settle the model
      choice with T-42's gold set (ruling R7).
      **RE-CITED: three places now, not two.** `defaultLLMModel` moved to
      `internal/extraction/llm_provider.go`, the kong default is
      `ANTHROPIC_MODEL` in `internal/config/config.go`, and T-18 added
      `LLM_MODEL` beside it — which takes precedence and has no default at all,
      so a `LLM_PROVIDER=anthropic` deployment that sets only `LLM_MODEL=` still
      falls through to the expensive default.
- [ ] **T-35 · 4.4 · S** — `Temperature: 0` for extraction — `extraction` E5
      **RE-CITED: two edits now.** Temperature is unset in *both* transports,
      `anthropicClient.recordRecipe` and `openAIClient.recordRecipe`
      (`internal/extraction/llm_provider.go`). Setting one and not the other
      would make extraction reproducible on one provider and not the other —
      the same drift the shared `record_recipe` schema exists to prevent.
- [ ] **T-36 · 4.4 · S** — Stamp a version into the binary — `delivery` D8 — `Makefile:16-17`, `Dockerfile:20`
- [ ] **T-37 · 4.2 · S** — Configure the connection pool — `persistence` P5 — `internal/db/connect.go:28`
- [ ] **T-07 · 4.0 · S** — Make the insert itself the dedupe — `persistence` P1
      `ON CONFLICT (source) DO NOTHING` on `CreateRecipe`; treat `pgx.ErrNoRows` as `Skipped`. **CORRECTED:** the race is latent, not live — the worker mutex closes the import-vs-import path and `createRecipe` (`web/src/api.ts`) has no caller in the shipped UI. Still worth doing: the fix is simpler and one query cheaper than the check-then-act it replaces.
      **There are two dedupe reads now.** T-01 added `FetchOptions.Known`, wired
      to `GetBySource`, so the fetcher can page past imported posts. That one is
      deliberately advisory — it answers "not known" on any error and the
      pipeline re-checks authoritatively. **Leave it alone; this task is only
      about the pipeline's check-then-act.**
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
      **The prompt moved under this task.** `systemPrompt` is in
      `internal/extraction/llm.go` and is now shared verbatim by both
      transports, so delimiting it is one edit that covers both. T-04 also gave
      the model a `confidence` field to fill in — worth reading as an injection
      surface of its own, since a caption that talks the score up publishes
      without review.
- [ ] **T-41 · 3.2 · S** — Better recipe title than "first line" — `extraction` E10 — `internal/extraction/rules.go:61,75-82`
- [ ] **T-45 · 3.0 · S** — Replace the search `DISTINCT` with a semi-join — `persistence` P8
- [ ] **T-46 · 3.0 · M** — Typed structs for Instagram responses — `go` #14 — `pageMedia` and `extractMedia` in `internal/instagram/saved.go` still walk `map[string]any`. **Do it after T-43:** its committed fixture is what tells you which fields are actually optional, and T-15's `ErrSchemaDrift` gives the typed version somewhere to report a mismatch.
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
      **Order:** after T-16, so the test exercises the `0002` migration it adds rather than only `0001`.
- [ ] **T-51 · 2.6 · S** — Keyword-match categories in rules-only mode — `extraction` E12
- [ ] **T-13 · 2.5 · S** — Fix the Instagram client test's name and cover the restore branch — `delivery` D2 + `integration` I12
      **CORRECTED — premise withdrawn.** Revision 1 said this test makes a real network call on every run. It does not and never has: `instago@v1.0.2/auth.go:48-50` returns `BadCredentials` before any request is built, verified independently by two reviewers including a dead-proxy run. **The suite is already hermetic.** What remains is Low: the name `RestoresExistingSession` asserts the *missing*-session path, `err == nil` is near-tautological, and the restore branch has zero coverage.
      **Unchanged by the ≥ 6.0 work**, though `client_test.go` grew around it: T-11 and T-06 added timeout, slot and re-authentication tests beside the untouched original, and `LoginOrRestore` now takes a `ctx`.
- [ ] **T-52 · 2.5 · S** — Log the discarded `LoadSettings` error — `go` #13 — still discarded in `Client.LoginOrRestore` (`internal/instagram/client.go`), and now more worth saying out loud: a session file that fails to load sends the client straight to a fresh login, which is the behaviour T-06's re-login floor exists to ration.
- [ ] **T-53 · 2.5 · S** — `RunOnce` should not return a stale result when it skips — `go` #10
      **SCOPE GREW: two skip paths now.** T-12 added a second one — `RunOnce`
      returns `lastResult` both when a run is already in flight and when the
      rate-limit cooldown is in effect (`internal/pipeline/worker.go`). The
      cooldown path at least logs and the API refuses with 429 before reaching
      it, so the finding is unchanged in kind and slightly wider in reach.
- [ ] **T-55 · 2.4 · S** — Decide whether duplicate ingredient rows are meaningful — `persistence` P9
- [ ] **T-56 · 2.4 · S** — Cache the resolved collection id — `integration` I9 — `PipelineFetcher.FetchNewPosts` (`internal/instagram/fetcher_adapter.go`) still calls `ResolveCollectionID` on every run, which is one extra private-API request per import against an endpoint T-12 now backs off from.
- [ ] **T-57 · 2.2 · S** — Record retention/provenance intent in `docs/PROJECT.md` — `security` S8
- [ ] **T-58 · 2.2 · S** — Re-panic on `http.ErrAbortHandler` — `go` #11
- [ ] **T-59 · 2.2 · S** — Drop the fixed `/tmp` path from `make lint` — `delivery` D10
- [ ] **T-60 · 2.0 · S** — Optional: read-then-insert for `FindOrCreate*` — `persistence` P6
- [x] **T-61 · 2.0 · S** — `dtoToRecipe` should take `context.Context` — `go` #9
      **Closed by T-08, not worked on its own.** `dtoToRecipe` no longer
      resolves lookups, so it needs neither the request nor a context — it
      takes only the DTO.

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
| 4.0 – 4.9 | 10 | 1 |
| 3.0 – 3.9 | 12 | 0 |
| 2.0 – 2.9 | 15 | 0 |
| < 2.0 | 2 | 0 |
| **Total** | **62** | **12** |
