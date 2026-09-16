# Code Review — `integration_reviewer`

**Date:** 2026-09-11
**Reviewer role:** Third-party integration and resilience engineer for unofficial API clients (panel definition: `agents.yaml` at the repo parent, role `integration_reviewer`)
**Scope:** `internal/instagram/**`, the fetch path in `internal/pipeline/**`, the Instagram wiring in `cmd/recipe-reader/main.go`, and the behaviour of `github.com/felipeinf/instago@v1.0.2` where this project depends on it
**Mode:** single pass, no panel iteration, no consensus round with the other reviewers

I read the dependency rather than assuming its behaviour; the pacing and retry facts below are cited to its source in the module cache.

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
| I1 | High | ~~8.6~~ **7.6** | A backlog larger than 50 posts is never imported — no cursor, no backfill | `internal/instagram/fetcher_adapter.go:25-38`, `internal/instagram/saved.go:77-116` |
| I2 | High | **7.6** | No session refresh: an expired session disables imports until restart | `internal/instagram/client.go:28-39`, `cmd/recipe-reader/main.go:71-77` |
| I3 | High | **7.4** | No timeout at any layer — the HTTP client has none and the API takes no context | `instago@v1.0.2/client.go:92`, `internal/instagram/saved.go:77` |
| I4 | Medium | **6.2** | HTTP 429 is not distinguished from any other 4xx — no backoff, the run just fails | `instago@v1.0.2/client.go:565-568`, `internal/pipeline/pipeline.go:49-52` |
| I5 | Medium | ~~4.6~~ **2.6** | A single 408 blocks the import for an uninterruptible 60 seconds | `instago@v1.0.2/client.go:555-559` |
| I6 | Medium | **5.0** | `MaxItemsPerRun` is unreachable — no flag, never set by the composition root | `cmd/recipe-reader/main.go:75`, `internal/config/config.go` |
| I7 | Medium | **5.8** | The pagination loop has no page cap and no progress guard | `internal/instagram/saved.go:80-111` |
| I8 | Medium | **5.4** | Login failure at boot permanently disables the import worker | `cmd/recipe-reader/main.go:71-87` |
| I9 | Low | **2.4** | `ResolveCollectionID` re-lists collections on every single run | `internal/instagram/fetcher_adapter.go:33` |
| I10 | Low | **3.6** | Schema drift is silent — no counter, no log, no signal | `internal/instagram/saved.go:93-105` |
| I11 | Low | **3.4** | The endpoints remain unverified against a real account, as the code itself notes | `internal/instagram/saved.go:26-27` |
| I12 | Low | ~~3.0~~ **2.5** | A unit test performs a real login attempt against Instagram | `internal/instagram/client_test.go:57` |

## High

### I1. A backlog larger than 50 posts is never imported

**Rating: 7.6 / 10 — MECHANISM CONFIRMED, MAGNITUDE UNRESOLVED.** Revised down from 8.6. The mechanism is proven by code alone. The magnitude claim ("the other 450 are unreachable") additionally assumes a stable, newest-first server ordering — the very endpoint behaviour this reviewer's own I11 says nobody has verified. Recorded as the round's one open dissent; settle it with task T-43.

**Location:** `internal/instagram/fetcher_adapter.go:25-38`, `internal/instagram/saved.go:77-116`, `internal/pipeline/pipeline.go:55-83`

`FetchNewPosts` fetches the newest `maxItems` (default 50) saved posts on every run and hands all of them to the pipeline, which dedupes by `source`. The name is honest about this — "It returns every post it finds — the 'new' in the name is the pipeline's job" — but the consequence is not stated: **there is no cursor persisted between runs**, so the window is always anchored at the newest post.

Point this at an account with 500 saved recipes and it imports the newest 50 and then stops. Every subsequent run re-fetches the same 50, finds them all present, and reports `Seen: 50, Skipped: 50, Imported: 0`. The remaining 450 are unreachable — not delayed, unreachable — because nothing ever pages past the first 50 items again. The status endpoint reports a healthy run while doing nothing, which is the worst version of this failure.

Newly saved posts do arrive correctly (they appear at the top), so the symptom is specifically: **the historical backlog never imports, and the tally looks fine.** For a tool whose entire premise is "import my saved recipes," that is a functional failure, not a performance one.

**Fix:** page until you reach known territory rather than until a fixed count. Persist the newest imported `source` (or Instagram's `max_id` cursor) and have the fetcher stop when it encounters it — the standard "fetch until overlap" pattern:

```go
// FetchNewPosts pages until it sees a post already in the database, or hits
// the per-run safety cap, whichever comes first.
```

Give the pipeline a `Fetcher` that can ask "do I already have this source?", or invert it: have `pageMedia` yield pages and let the pipeline stop paging when a whole page is `Skipped`. Either way, keep `MaxItemsPerRun` as a safety cap (see I6) but drive the *stop* decision from overlap, not from the cap. Backfilling 500 posts then happens across a few runs instead of never.

### I2. No session refresh: an expired session disables imports until restart

**Rating: 7.6 / 10**

**Location:** `internal/instagram/client.go:28-39`, `cmd/recipe-reader/main.go:71-77`

`LoginOrRestore` runs exactly once, in `run()`, at process start:

```go
if err := igClient.LoginOrRestore(...); err != nil {
    slog.Warn("instagram login failed; import worker will not run", "error", err)
} else {
    fetcher = &instagram.PipelineFetcher{...}
}
```

After that, nothing ever re-authenticates. Instagram sessions expire, get invalidated by a password change, or are dropped when the account is challenged. With `IMPORT_INTERVAL` defaulting to `6h`, this process is expected to run for weeks — and the moment the session goes stale, every fetch returns an auth error forever. The pipeline reports it as a fetch failure, the worker records it in `lastErr`, and the only remedy is an operator noticing and restarting the process.

**Fix:** detect the auth-failure class and re-run `LoginOrRestore` once before failing the run. `instago` exposes typed errors in `igerrors` — map the login-required / checkpoint cases and retry the fetch a single time after a successful re-login, with the session re-persisted. Deliberately *not* a retry loop: repeated login attempts are the behaviour Instagram flags, so one attempt per run at most, and a distinguishable error when it fails so the status endpoint can say "re-authentication required" rather than a generic failure.

### I3. No timeout at any layer

**Rating: 7.4 / 10** — held, and absorbs `go` #3 (6.9). **The fix in this section needs rewriting:** `PrivateRequest` has no ctx parameter, `PrivateRequestOpts` has no ctx field, the `*http.Client` is unexported, and there is no `SetHTTPClient` — only `SetProxy`, which builds its own transport. The real options are a leaky watchdog, a forward proxy, or a fork.

**Location:** `instago@v1.0.2/client.go:92-93`, `internal/instagram/saved.go:77-116`, `internal/instagram/fetcher_adapter.go:25`

Three layers, none of which bounds a request:

1. The dependency's HTTP clients are constructed as `&http.Client{Jar: jar}` — **no `Timeout` field set**. A connection that opens and then stalls blocks in `Read` indefinitely.
2. `PrivateRequest(opts PrivateRequestOpts)` takes no `context.Context`, so there is nothing to thread even if the caller had a deadline.
3. This project's own wrapper discards the context it is given (`FetchNewPosts(_ context.Context)`) and its methods take none.

The result is an import that cannot be interrupted by anything: not `SIGTERM`, not the API handler returning, not a shutdown. `go_reviewer` #3 raises the same code from the idiom side; the integration consequence is what makes it High here — an unofficial endpoint that hangs rather than refusing is a normal event, and this client has no answer for it. It compounds with `Worker`'s single-flight guard (`internal/pipeline/worker.go:55-60`): one hung fetch blocks every future import for the life of the process, and `/api/import/status` reports `running: true` forever.

**Fix:** `PrivateRequest`'s missing context cannot be fixed from here without an upstream change, so bound it at the layer you control. `instago`'s client is constructed by `ig.NewClient()` (`internal/instagram/client.go:21`) — if it exposes its `*http.Client` or accepts one, set `Timeout: 30 * time.Second` on it; that single field turns every hang into an error the existing code paths already handle. If it does not, wrap each `pageMedia` iteration in a watchdog (`select` on a result channel and `ctx.Done()`), accepting the leaked goroutine as the lesser evil, and open an upstream issue or PR adding `PrivateRequestWithContext`. Either way, add the `ctx.Err()` check at the top of the paging loop so cancellation is honoured between pages even when it cannot interrupt one.

## Medium

### I4. HTTP 429 is not distinguished from any other 4xx

**Rating: 6.2 / 10**

**Location:** `instago@v1.0.2/client.go:565-568`, consumed at `internal/instagram/saved.go:85-88` → `internal/pipeline/pipeline.go:49-52`

The dependency's response handling is:

```go
if resp.StatusCode >= 400 {
    if err := igerrors.MapPrivateHTTPError(opts.Endpoint, resp, rawBytes); err != nil {
        return nil, err
    }
    return nil, &igerrors.ClientError{...}
}
```

`429` falls into that branch like a `404` — no `Retry-After` parsing, no backoff, no retry. It propagates up through `pageMedia` → `FetchNewPosts` → `Pipeline.Run`, which returns early (`"pipeline: fetch posts: %w"`), so **one rate-limit response aborts the entire import run**, discarding the posts already collected in `out`. The next attempt is a full `IMPORT_INTERVAL` away (6h by default) — or immediately, if a user clicks the import button, which is precisely the wrong response to a rate limit.

**Fix:** classify the error at this project's boundary and act on it. On a rate-limit error, return the posts gathered so far together with a typed `ErrRateLimited` rather than discarding them, so a partial import still makes progress; have `Worker` honour a cooldown before the next attempt and have `handleImportRun` refuse to trigger during it (`429` to the caller, with the retry time). Jittered exponential backoff belongs here rather than in a tight retry loop — with an unofficial API, backing off *longer* is the safe direction.

### I5. A single 408 blocks the import for an uninterruptible 60 seconds

**Rating: 2.6 / 10** — reclassified (chair ruling R5) from a standalone 4.6 to a constraint-note under I3. The sleep is rare, bounded to once per request, and arguably correct backoff; its only bad property is being uninterruptible, which is I3's point. **Constraint the I3 fix must respect:** any timeout must exceed 60s or it will fire on every 408 retry.

**Location:** `instago@v1.0.2/client.go:555-559`

```go
if resp.StatusCode == http.StatusRequestTimeout && !retried408 {
    retried408 = true
    time.Sleep(60 * time.Second)
    continue
}
```

A bare `time.Sleep(60s)` inside the dependency, with no context, in the goroutine running the import. It cannot be cancelled or shortened. Combined with I3 this is a second uninterruptible stall path, and with 50 items across several pages a run that hits a few 408s spends minutes asleep while `Status()` reports `running: true`.

**Fix:** nothing to fix inside the dependency from here — the point of recording it is that any timeout you set (I3) must account for it, and any user-facing "import is running" indicator should not be interpreted as progress. If you upstream a context-aware `PrivateRequest`, this sleep is the first thing that should become `select { case <-time.After(60*time.Second): case <-ctx.Done(): }`.

### I6. `MaxItemsPerRun` is unreachable

**Rating: 5.0 / 10**

**Location:** `internal/instagram/fetcher_adapter.go:16,26-29`, `cmd/recipe-reader/main.go:75`, `internal/config/config.go`

`PipelineFetcher` has a `MaxItemsPerRun` field with a documented default of 50, and the composition root never sets it:

```go
fetcher = &instagram.PipelineFetcher{Client: igClient, CollectionName: cfg.InstagramCollection}
```

There is no corresponding flag in `Config`, so the value is 50 in every deployment with no way to change it short of a recompile. Given I1 — where the cap is the thing that silently truncates the backlog — this is the one knob an operator would most want, and it is the one that is not wired.

**Fix:** add `ImportMaxItems int` to `Config` (`name:"import-max-items" env:"IMPORT_MAX_ITEMS" default:"50"`), pass it at the construction site, and add it to `.env.example` alongside `IMPORT_INTERVAL`. Keep the field's internal default as the guard for a zero value, which it already handles correctly.

### I7. The pagination loop has no page cap and no progress guard

**Rating: 5.8 / 10**

**Location:** `internal/instagram/saved.go:80-111`

```go
for len(out) < maxItems {
    ...
    items, _ := res["items"].([]any)
    if len(items) == 0 { break }
    for _, raw := range items { ... if post, ok := extractMedia(media); ok { out = append(out, post) } }
    next, _ := res["next_max_id"].(string)
    if next == "" { break }
    maxID = next
}
```

The loop advances only when `extractMedia` succeeds, but it *continues* on `next_max_id` alone. If the endpoint returns pages of items that all fail extraction — a schema change renaming `code`, which is exactly the drift to expect from an unofficial API (I10, I11) — `out` never grows, the count-based condition never trips, and the loop pages as long as the server supplies a cursor. With no timeout (I3) and no context check, that is an unbounded request loop against Instagram, which is the single fastest way to get an account flagged.

The terminating conditions are all controlled by the remote: empty `items`, empty `next_max_id`, or enough successful extractions. None are controlled by this code.

**Fix:** add the two guards that make the loop's termination local:

```go
const maxPages = 20
for page := 0; len(out) < maxItems && page < maxPages; page++ {
    if err := ctx.Err(); err != nil { return out, err }
    ...
    if next == "" || next == maxID { break }  // no-progress guard
}
```

The `next == maxID` check costs nothing and catches a server that repeats a cursor.

### I8. Login failure at boot permanently disables the import worker

**Rating: 5.4 / 10**

**Location:** `cmd/recipe-reader/main.go:71-87`

If `LoginOrRestore` fails at startup — network blip, Instagram checkpoint, transient 5xx — `fetcher` stays nil, so `worker` is never constructed, so `Deps.Worker` is nil and both import endpoints answer `503` for the life of the process (`internal/api/handlers_import.go:20-22,34-36`). Degrading to a read-only server is the right *shape* of response and the comment says so; making it permanent is the problem. A transient failure at boot is indistinguishable from a missing configuration.

**Fix:** construct the worker whenever credentials are configured and let the *fetch* fail instead, so a later run can succeed once Instagram is reachable again — which pairs naturally with the re-login path in I2. `/api/import/status` should then report "not authenticated" as a state rather than 503-ing, so the UI can tell "never configured" from "login failed, will retry".

## Low

### I9. `ResolveCollectionID` re-lists collections on every run

**Rating: 2.4 / 10**

**Location:** `internal/instagram/fetcher_adapter.go:33`

When `INSTAGRAM_COLLECTION` is set — and `.env.example` ships with `INSTAGRAM_COLLECTION=Rezepte`, so this is the intended path — every import run issues an extra `collections/list/` request to translate the same name to the same id. Collection ids are stable. It is one additional request per run against an API where request volume is the risk being managed, and it adds a second failure mode to every run (a failed list aborts the fetch before it starts).

**Fix:** resolve once and cache it on `PipelineFetcher`, re-resolving only if a fetch fails with a not-found error.

### I10. Schema drift is silent

**Rating: 3.6 / 10**

**Location:** `internal/instagram/saved.go:89-105`, `extractMedia` at `:118-139`

Every field access is a comma-ok assertion that discards the failure: `items, _ :=`, `id, _ :=`, `caption, _ :=`, `imageURL, _ :=`. Nothing panics — which is the right property, and `go_reviewer` #14 credits it — but nothing is *reported* either. If Instagram renames `code`, `extractMedia` returns `false` for every item, `pageMedia` returns an empty slice, the pipeline reports `Seen: 0`, and the import silently does nothing forever. The status endpoint shows a successful run with zero results, which is also what a genuinely empty collection looks like.

For an endpoint the code itself flags as unverified, "broke silently and looks healthy" is the failure mode to design against.

**Fix:** count the drops and surface them. `pageMedia` returning `(posts []SavedPost, dropped int, err error)`, with a `slog.Warn` when `dropped > 0` and the first offending item's keys logged at debug, turns an invisible breakage into a one-line diagnosis. A run where `dropped > 0 && len(out) == 0` is strong evidence of drift and deserves to be an error rather than an empty success.

### I11. The endpoints remain unverified against a real account

**Rating: 3.4 / 10**

**Location:** `internal/instagram/saved.go:25-27`

```go
// FetchSavedPosts pages through the account's "All Posts" saved collection.
// See the Task 11 header note: the endpoint is unofficial and unverified —
// run Step 6 against a real account before depending on this in production.
```

The note is honest and correctly placed, and the verification it asks for has not happened — nothing in the repository records a successful run against a live account. Three specifics are unproven and each would fail silently per I10: that `feed/saved/posts/` is the correct endpoint and returns `items` at the top level; that saved-feed entries wrap the media in a `media` key (the code hedges with `media = item` at `:100`, which suggests uncertainty rather than knowledge); and that `next_max_id` is the cursor field for this endpoint.

**Fix:** treat verification as a release gate, and capture the result — a recorded JSON fixture from one real response, committed as `testdata/`, turns all three unknowns into a test and gives `extractMedia` a realistic case. That is also the fixture I10's drift detection should be measured against.

### I12. A unit test performs a real login attempt against Instagram

**Rating: 2.5 / 10** — chair ruling R3, merged with `delivery` D2. **The premise below is withdrawn:** the test makes no network call and never has — `instago@v1.0.2/auth.go:48-50` returns `BadCredentials` before any request is built. What survives is the misleading name, the near-tautological assertion, and the uncovered restore branch.

**Location:** `internal/instagram/client_test.go:48-64`

`TestClient_LoginOrRestore_RestoresExistingSession` calls `c.LoginOrRestore("", "", sessionPath)` with no session file present, which falls through to `c.raw.Login("", "", "")` — a real HTTPS request to Instagram, executed on every `go test ./...`, including in CI (`.github/workflows/ci.yml:61-62`). The test passes when the login fails, so it passes offline for the wrong reason and passes online by sending unauthenticated login traffic from a shared runner IP to the service this project is trying not to annoy.

The name also describes the opposite of what is asserted — the comment concedes it tests the *missing*-session path.

**Fix:** covered from the delivery angle in that review; from the integration angle the requirement is simply that nothing in the test suite ever contacts Instagram. Make the login path injectable (an interface with a fake), or drop to asserting only that no session file is written.

## What's right

- **The dependency paces its own requests**, and I verified this rather than assuming it was missing: `PrivateRequest` calls `c.randomDelay()` and then `time.Sleep(c.requestTimeout)` (1 second) before each non-login request (`instago@v1.0.2/client.go:422-427`). So there *is* jittered inter-request spacing of roughly a second, which is the single most important anti-flagging behaviour and it comes for free. The gaps are 429 handling (I4) and the absence of a longer backoff — not pacing itself.
- **Session reuse is the right default and correctly implemented.** `LoginOrRestore` tries `LoadSettings` first and only logs in on failure, then persists the result (`client.go:28-39`), with a doc comment naming the reason: repeated logins are rate-limited and look suspicious. The dependency writes that file with `0600` (`instago@v1.0.2/settings.go:33`), and `docker-compose.yml:35-36` mounts a named volume so it survives restarts, with the reasoning written down. This is the part of an unofficial-client integration that most projects get wrong, and it is handled deliberately here.
- **Dedupe is keyed on a stable, canonical identifier.** `Source` is built as `https://www.instagram.com/p/<code>/` from the media `code` (`saved.go:136`), not from a URL the API returned, so it is stable across responses and API shape changes; it is `UNIQUE` in the schema and doubles as the import idempotency key. Re-importing the same post is a no-op by construction. (The check-then-insert race on it is `persistence_reviewer` P1 — the *key choice* is right.)
- **Instagram is genuinely optional.** Missing credentials or a failed login degrade to a working read-only API over whatever is already stored (`main.go:66-77`), and the import handlers answer `503` rather than panicking on a nil worker — with `Deps.Worker`'s nilability documented at the type (`internal/api/router.go:19-20`) and tested (`internal/api/handlers_import_test.go`). The degraded mode was designed, not discovered.
- **The dependency direction is deliberate.** `PipelineFetcher` satisfies `pipeline.PostFetcher` structurally without importing `pipeline` (`fetcher_adapter.go:10-13`), so the pipeline knows nothing about Instagram and this package knows nothing about the pipeline. When the Instagram client needs replacing — and with an unofficial API it will — the blast radius is this package plus four lines in `main`.
- **`maxItems` is enforced twice**, by the loop condition and by the `out[:maxItems]` truncation afterwards (`saved.go:112-114`), so a page that overshoots the budget cannot inflate a run.
