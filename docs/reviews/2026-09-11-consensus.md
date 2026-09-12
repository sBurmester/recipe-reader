# Recipe Reader — Panel Consensus Record

**Chair:** `review_chair`
**Date:** 2026-09-11
**Round:** one cross-examination, single pass, no iteration
**Entering:** 72 findings from six independent reviews
**Leaving:** 66 findings — 1 withdrawn, 5 merged away, 15 ratings moved, 51 unchanged

I brought no findings of my own. My job was to put the contested points to each
specialist, demand file:line evidence, merge duplicates, and bring every
finding to an explicit outcome — consensus or recorded dissent. Twenty-five
challenges went out across six reviewers. Every one came back answered.

## What the round changed

| | Entering | Leaving |
| --- | --- | --- |
| Findings | 72 | 66 |
| Mean rating | 4.48 | **4.28** |
| Rated ≥ 7.0 | 10 | **7** |
| Highest | 8.6 (`integration` I1) | **8.5 (`security` S1)** |

Thirteen ratings moved down, two moved up, one finding was withdrawn outright
and five were merged into others. **No reviewer defended an original rating
without new evidence**, and four of the six corrected a factual error in their
own review.

The cross-examination was worth running: the panel's two highest-rated findings
both moved, one of them because its premise turned out to be false.

## Corrections to the record

These are facts established during the round that contradict what the
independent reviews stated. They are recorded here because several of them
invalidate reasoning that was already acted on.

1. **The Instagram login test makes no network call — it never has.**
   `delivery` D2 (7.2) and `integration` I12 (3.0) both asserted that
   `go test ./...` issues a real HTTPS login to Instagram. It does not.
   `instago@v1.0.2/auth.go:48-50` returns `&igerrors.BadCredentials{}` on empty
   credentials **before** `PreLoginFlow()` and before any request is built.
   Both reviewers verified this independently — `integration` by tracing the
   dependency and timing the test at 0.00s, `delivery` by tracing it and then
   running `Login("","","")` under a dead proxy, which returned
   `*igerrors.BadCredentials` rather than a dial error. The suite was already
   hermetic on exactly the axis both claimed it was not.
   **Both reviews also cited `client_test.go:48-64` — in a 26-line file.**
   `delivery` named this itself: "the tell that neither of us opened it."

2. **A `status=''` row is not invisible in the list view.** `go` #2 claimed it
   disappears from the UI. `web/src/pages/list.ts:54-58` sends no status filter
   and `grep -rn "status=" web/src/` returns nothing, so the bad row appears in
   the default list and silently reads as published.

3. **Junk rows do not create orphan ingredient rows.** `extraction` E1 claimed
   they feed `persistence` P2. They do not: the LLM sentinel path returns an
   empty ingredients list (`llm.go:18`) and a header-less caption produces no
   ingredient lines (`rules.go:52-57`), so junk rows typically carry zero
   ingredients. **The E1 → P2 causal link asserted in the index and the working
   plan is withdrawn.**

4. **The Instagram client does not jitter its request pacing.**
   `integration` credited `randomDelay()` in its "What's right" section. It is a
   permanent no-op: it only sleeps when `delayMin > 0`, and `delayMin`/`delayMax`
   (`instago@v1.0.2/client.go:67-68`) are never assigned, with no setter. The
   real pacing is a flat, unjittered 1.000s — a *more* fingerprintable cadence
   than the one credited.

5. **`go` #3's proposed fix is not implementable.** It recommended threading
   `ctx` into `PrivateRequest` or wrapping with `http.NewRequestWithContext`.
   `PrivateRequest` has no ctx parameter, `PrivateRequestOpts` has no ctx field,
   the `*http.Client` is unexported, and there is no `SetHTTPClient` — the only
   transport hook is `SetProxy`, which builds its own transport. The real
   options are a watchdog goroutine that leaks on a hang, a forward proxy, or a
   fork.

6. **The test suite starts 18 Postgres containers, not 8.** `delivery` D3
   counted lexical `testdb.New(` sites; five sit inside `t.Helper()` wrappers
   that several tests each invoke. The corrected count is worse than reported,
   while the rating went *down* — see the ruling below.

7. **`POST` writes skip CORS preflight entirely.** `security` S1 understated
   its own finding: `handleCreateRecipe` never inspects `Content-Type` and
   `handleImportRun` reads no body, so `POST` with `Content-Type: text/plain` is
   a CORS-*simple* request. `withCORS` never gets the chance to gate it.

8. **`createRecipe` is defined but never called.** `web/src/api.ts:49` has no
   caller anywhere in `web/src` — there is no create form in the shipped UI.
   This is what collapsed `persistence` P1 from High to Medium.

9. **Minor:** `delivery`'s `Makefile` citations in D1, D4, D9 and D10 are off by
   one to two lines (`build:` is at `Makefile:16-17`, not `17-18`). The targets
   are as described.

## Chair rulings

Five items could not be settled by the reviewers alone, because both parties
moved and crossed, or because they disagreed about the merge itself.

### R1. `go` #2 + `persistence` P7 → **5.5** (one finding, both halves)

The two reviewers crossed: `go` revised its handler finding *down* to 5.0 after
discovering correction 2, while `persistence` conceded *up* to 6.5 — to a number
`go` had already abandoned. I set 5.5, above `go`'s figure because
`persistence` established reach that `go` did not price in (**neither handler
validates a non-empty status, so `{"status":"banana"}` persists on the POST path
too**), and below the abandoned 6.5 because the invisibility premise that
supported it is false.

`persistence` attached a condition which I adopt: **the fix is both halves or it
is not done.** Handler validation on POST *and* PUT, plus a `0002` migration
adding `CHECK (status IN ('needs_review','published'))` preceded by a backfill.
Validation alone leaves existing bad rows and leaves the invariant unenforced
against psql, future migrations, and any writer that is not this handler.

### R2. Silent errors — one theme, three sites, three ratings preserved

`go` #5 **5.8**, `extraction` E8 **6.0**, `integration` I10 **3.6**.

`go` revised down from 6.8 (conceding top-of-band inflation) and `extraction`
revised up from 5.2; they crossed. I am not collapsing them, because both
reviewers independently proposed the same structure and the argument for it is
sound: one root cause — there is no logging anywhere outside `main` and the API
middleware, confirmed by grep — but three genuinely different symptoms needing
three different signals. `extraction` earns the top slot on the axis the scale
weights most: E8 is the only site where the failure is invisible *in both
directions*, because the tally reports success while quality degrades. Record as
one remediation item, three site ratings.

### R3. `delivery` D2 + `integration` I12 → **2.5** (one finding, premise withdrawn)

Both original ratings rested on correction 1 and both are void. `delivery`
declined to concede to I12 on the grounds that it "reaches a defensible number
by the same incorrect route" — which I accept as a matter of method: the panel
records the corrected mechanism, not an average of two ratings that shared one
false assumption. On the surviving defect the two land at 2.0 and 3.0. I set
2.5: `delivery`'s figure prices only the uncovered restore branch, while
`integration` additionally named the near-tautological assertion (`err == nil`
is satisfied by any error). Both are real; both are in the Low band; the decimal
is below this scale's resolution and the substance is the retraction.

### R4. `go` #3 → `integration` I3 at **7.4**; `go` #8 → `security` S3 at **4.8**

Both concessions accepted, both to the reviewer whose scope was the superset.
`integration` I3 carries the dependency evidence that makes the finding
actionable (correction 5). `security` S3 is a strict superset of `go` #8 — the
timeout fields *plus* two unbounded `json.Decoder` calls in a different file —
and `security` lowered its own figure from 5.4 to 4.8 on the grounds that it had
double-counted S1's missing authentication as a severity multiplier. I note for
the plan that **this is two edits in two files, not one**, correcting my own
framing in the challenge.

### R5. `integration` I5 → **2.6**, demoted to a constraint-note under I3

Accepted. The 60-second blocking sleep at `instago@v1.0.2/client.go:555-559` is
rare, bounded to once per request, and arguably correct backoff; its only bad
property is that it is uninterruptible, which is I3's point rather than a second
finding. It survives as a constraint the I3 fix must respect: **any timeout must
exceed 60s or it will fire on every 408 retry.** I reject the framing in my own
challenge that a defect with no local fix does not belong on the scale — the
scale measures exposure in this deployment, not authorship.

## Unresolved

One item does not reach consensus, and I am recording the dissent rather than
manufacturing agreement.

**`integration` I1 — magnitude unproven (rating 7.6, mechanism agreed).**
The mechanism is established by code alone and is not in dispute: `maxID` is a
function-local (`saved.go:79`), no cursor is persisted anywhere in the
repository, and `pipeline.PostFetcher` has no return path by which "already
imported" could reach the fetcher — so every run restarts at the feed head and
stops at 50. What is *not* established is the magnitude. The claim that a
500-post backlog leaves 450 permanently unreachable additionally assumes a
stable, newest-first server ordering — which is precisely the endpoint behaviour
that `integration`'s own I11 (3.4) says nobody has verified against a real
account. The reviewer named this contradiction unprompted and dropped the rating
from 8.6 to 7.6 rather than defend it.

**Resolution: none available in this round.** It requires one live run against a
real account, which is I11's remediation (plan task T-43). Until then the panel
records: mechanism CONFIRMED, magnitude CONDITIONAL. Fixing I1 is still correct
— nothing about the fix depends on the magnitude — but the 7.6 should be read as
provisional.

## Final ratings

Ratings ≥ 7.0, after the round:

| Rating | ID | Finding | Movement |
| --- | --- | --- | --- |
| **8.5** | S1 | No authentication on any endpoint, combined with `ACAO: *` | held (understated) |
| **7.8** | E2 | Confidence hard-coded to 0.9 — every LLM result auto-publishes | held (narrowed) |
| **7.6** | I1 | Backlog over 50 posts never imports | 8.6 → 7.6, magnitude unresolved |
| **7.6** | I2 | No session refresh after expiry | unchallenged |
| **7.4** | I3 | No timeout at any layer (absorbs `go` #3) | held, fix corrected |
| **7.2** | D1 | `make build` ships a placeholder-UI binary | 7.8 → 7.2, reproduced |
| **7.0** | E1 | Non-recipe captions still create rows | 8.2 → 7.0 |

Everything that moved:

| ID | Was | Now | Ruling |
| --- | --- | --- | --- |
| `security` S1 | 8.5 | **8.5** | held — and understated; see correction 7 |
| `extraction` E2 | 7.8 | **7.8** | held; claim narrowed to "LLM results containing a recipe" |
| `integration` I1 | 8.6 | **7.6** | revised; magnitude unresolved |
| `integration` I3 | 7.4 | **7.4** | held; absorbs `go` #3 (6.9) |
| `delivery` D1 | 7.8 | **7.2** | revised; reproduced from a clean clone |
| `extraction` E1 | 8.2 | **7.0** | revised; failure is badged, not silent |
| `extraction` E8 | 5.2 | **6.0** | **raised** |
| `go` #5 | 6.8 | **5.8** | revised |
| `go` #2 + `persistence` P7 | 6.5 / 3.5 | **5.5** | chair ruling R1 |
| `delivery` D3 | 7.0 | **5.5** | revised; count corrected upward to 18 |
| `extraction` E7 | 4.6 | **5.5** | **raised** |
| `security` S3 + `go` #8 | 5.4 / 4.0 | **4.8** | merged, ruling R4 |
| `persistence` P1 | 7.2 | **4.0** | revised; race is latent |
| `security` S5 | 4.5 | **3.2** | revised |
| `persistence` P4 | 4.5 | **3.0** | revised; withdrew its criticism of the code comment |
| `go` #1 | 5.2 | **3.0** | revised |
| `integration` I5 | 4.6 | **2.6** | reclassified, ruling R5 |
| `delivery` D2 + `integration` I12 | 7.2 / 3.0 | **2.5** | chair ruling R3; premise withdrawn |
| `delivery` D11 | 3.0 | **3.0** | narrowed to the sqlc half; other two halves withdrawn |
| `security` S9 | 0.5 | **withdrawn** | a clean scan is a scan result, not a finding |

The 51 unchallenged findings keep their original ratings.

## Chair's assessment

Two observations I owe the panel.

**The round earned its keep on evidence, not on ratings.** The nine corrections
above matter more than the 0.20 shift in mean. Three of them — the hermetic test
suite, the unimplementable `ctx` fix, and the withdrawn E1 → P2 link — would each
have produced wasted or misdirected work if the plan had been executed as
written.

**The panel's calibration ran hot, and it knew it.** Thirteen of fifteen
movements were downward, and the two highest-rated findings both came down.
`go` named the mechanism precisely when challenged on setting 6.9 to stay inside
its own "Medium" label: *fitting the evidence to the band instead of the band to
the evidence.* That is worth carrying into the next review — the independent
passes were strong on mechanism and weak on proportion, which is exactly the
failure mode a chair exists to catch.

The one thing I would not want lost: `security` S1 held at 8.5 under the
hardest challenge I put to anyone, and got worse in the process. It is the
finding to act on first.

---

# Round 2 — Rating Calibration (PARTIAL)

**Date:** 2026-09-12
**Status:** **INCOMPLETE — four of six reviewers did not report.**

A second round was convened to test the ratings themselves rather than the
mechanisms, on the chair's concern that round 1 moved 13 ratings down and only
2 up — which is either honest correction or a deflation spiral in which each
challenged reviewer simply retreats. Every reviewer was given the full
post-round-1 panel table for cross-calibration and an explicit **underrating
check**: name anything, yours or another's, that is now rated too low, and argue
it up.

**`go_reviewer` and `extraction_reviewer` reported. `persistence_reviewer`,
`security_reviewer`, `integration_reviewer` and `delivery_reviewer` terminated
on an infrastructure rate limit before answering.** Their challenges are
recorded below as outstanding. Nothing has been inferred on their behalf.

## The chair was wrong about the premise

`go_reviewer` rejected the arithmetic behind its own challenge, and it is right:

> "Two of the four movements the chair lists — #3 from 6.9 to 7.4 and #8 from
> 4.0 to 4.8 — are upward concessions in which I abandoned my own lower number,
> so my round-1 record is three down and two up, not four retreats; and the
> reason I 'own nothing in the High band' is that both of my High-band findings
> were merged into other reviewers' rows (I3, S3) as a matter of attribution,
> not of calibration. The mean of 3.5 is likewise an artifact: nine of my
> fifteen findings are Low or Informational because I reported the nits instead
> of suppressing them."

`extraction_reviewer` made the same denominator argument independently: its 5.0
mean is partly because it "did not record the kind of sub-2.0 nits that pull
`go` (15 findings, floor 1.2) and `persistence` (11 findings, floor 1.5) down."

**Per-reviewer means are not comparable across this panel, and the chair should
not have presented them as if they were.** A reviewer who reports nits is
penalised by the mean; a reviewer whose strongest findings are merged away loses
its own top-of-band. Compare distributions at the top, not averages.

## Rulings

### R6. `go` #6 → **6.4** (raised; convergent from two reviewers)

Both reporting reviewers independently nominated the *same* finding as the
panel's most under-rated, and both argued it up: `go` to 6.2, `extraction` to
6.4. That convergence is the strongest calibration signal the round produced.

`EXTRACTION_MODE=llm` with a valid API key present does not merely fail to use
the LLM — it silently selects the *opposite* of what was requested, returning
the weakest extractor while a paid key sits unused, on every post of every run,
with nothing in the logs to contradict the operator (`grep -rn "slog\."
internal/` returns two lines, both in `api/middleware.go`).

I take `extraction`'s 6.4 over `go`'s 6.2. `go` capped itself at 6.2 because the
resulting `needs_review` badge is "a partial signal" — but `extraction` showed
that signal is actively misleading rather than partially helpful: it points at
the captions, not at the configuration, so it directs the operator away from the
cause. The cap argument does not hold.

**Verified by the chair, and worse than either reviewer knew.** The project
recorded this defect and shipped it:
`docs/superpowers/plans/2026-09-05-recipe-reader-tasks/18-main-go-wiring-graceful-shutdown.md`
note 3 reads *"Open question — `EXTRACTION_MODE=llm` silently runs rules-only …
needs a decision."* The task is marked `[x] done`. The one mechanism that
normally catches a defect like this — someone noticing — has already failed once
here.

### R7. `extraction` E4 → **4.8** (lowered by its own reviewer)

Accepted. `extraction` broke its own three-way tie at 5.5 in its own lane's
disfavour: a wrong default model "is the only one of the three whose failure
*announces itself*, on the first invoice," and the scale rates a loud defect
below an equally severe silent one.

### R8. `go` #4 holds **5.0** — the chair's challenge rested on a false premise

I put it to `go_reviewer` that a use-after-close on a live connection pool
should rate above 5.0. It read the dependency and refused:

`pgxpool.Pool.Close` delegates to `puddle/v2@v2.2.2/pool.go:179-195`, which sets
`p.closed = true` and then destroys **only `p.idleResources`**. A connection
currently executing a query is not in that list, is not destroyed, and its
socket is not closed; `releaseAcquiredResource` later takes the `p.closed`
branch and destroys it cleanly. There is no use-after-close. In practice the
process exits first anyway (`main.go:121-122`).

This also corrects `go` #4's own text, which predicted "failed queries and a
bogus tally at shutdown" — the tally is never reported at all, because the
process is gone before `Status()` could be read. The rating holds at 5.0 on
different reasoning: high likelihood, small blast radius, **zero diagnosability**.

### R9. R1 ratified at 5.5 — by `go` only

`go_reviewer` accepts the merged 5.5 and the both-halves condition, adding
evidence that strengthens it: `handleCreateRecipe` *does* validate — name and
source, not status — so a **non-empty** bogus status persists through the POST
path that `go` #2 had cited as the correct one. `persistence_reviewer`'s
ratification did not arrive; R1 stands as ratified by one party of two.

## Held under challenge

| Finding | Rating | Basis |
| --- | --- | --- |
| `extraction` E2 | **7.8** | The sharpest defence of the round — see below |
| `extraction` E1 | **7.0** | Survives the P1 collapse test: fires on the default no-API-key path |
| `extraction` E8 | **6.0** | Declined to raise against `delivery` D5; "two different risk shapes with comparable expected cost" |
| `extraction` E7 | **5.5** | Held on new evidence that cuts against it — see below |
| `go` #5 | **5.8** | Correctly ordered below E8 under ruling R2 |
| `go` #1 | **3.0** | Re-verified; silent-wrap case confirmed at page−1 = 214748365 |

**On E2 — the consistency test.** I challenged `extraction` to apply its own
"the failure is visible" argument (which lowered E1 from 8.2) to E2, which it
had held at 7.8. It distinguished them on evidence rather than retreating:

> "My visibility criterion asks whether the *defect* announces itself, not
> whether the *output* is on screen. E1 renders a review badge, an empty
> ingredient list, and the literal sentinel string `NO_RECIPE_FOUND` in the
> instructions box … E2 renders a card and a form that are pixel-identical to a
> correct extraction, and because `source` is never displayed in the UI the
> reviewer has no affordance to compare it against the caption it came from."

`grep -rn "source\|image_url" web/src/` returns only the type definition and the
PUT payload echo — **the source permalink is never rendered anywhere**, so there
is no link from any page back to the originating post. The distinction holds.

`extraction` also established that the LLM path is the *normal* path, not an
edge case: `confidenceFor` awards 1.0 only when a caption carries both German
headers, so every caption without that layout scores ≤ 0.5, falls under the 0.6
threshold, and goes to the LLM.

## Discovery: the E7 fix already exists, unmerged

`extraction_reviewer` found, and the chair verified, an unmerged local branch
`feat/provider-agnostic-llm-extractor` (commit `4bbe23e`, "Task 25") carrying a
211-line `internal/extraction/llm_provider.go` and a 132-line
`llm_provider_test.go` that drives `Extract` end to end against `httptest`
stubs — exactly the seam E7 asks for.

Three things the chair confirmed directly:

1. **It does not compile against `main`.** The branch changes the constructor to
   `func NewLLMExtractor(cfg LLMConfig) (*LLMExtractor, error)`, while
   `cmd/recipe-reader/main.go:62` still calls the two-argument form — and
   `git show --stat 4bbe23e -- cmd/recipe-reader/main.go` shows the composition
   root is untouched by the commit.
2. **Merging it does not fix E2.** `Confidence: 0.9` and the `0` sentinel
   override survive unchanged on the branch, with a comment stating the values
   are deliberately provider-independent.
3. **The project already knows.** Task 18's note 4 records that Task 25 "is
   **not** merged (it lives on the unmerged local branch
   `feat/provider-agnostic-llm-extractor`)".

This changes T-18's shape from *build a seam* to *merge and rewire*, and it
means T-04 (E2) must be done separately regardless.

## Outstanding — not answered

These challenges went out and did not come back. They are open, not resolved:

| Reviewer | Question left open |
| --- | --- |
| `security` | **Should S1 rise above 9.0?** It said in round 1 that a routable host makes it >9.0 — and `docker-compose.yml` ships `ports: ["8080:8080"]` and `["5432:5432"]`. Does the panel rate the shipped default or the cautious one? |
| `integration` | **Settle I1's magnitude** — hold 7.6 provisional, split it, or drop to the confirmed-only rating. Round 1's recorded dissent remains recorded. |
| `integration` | Lane bias: it owns 3 of 7 findings ≥7.0, all in one package. Untested. |
| `integration` | I2 (7.6) has never been challenged in either round. |
| `persistence` | Ratify or reject R1 at 5.5; over-correction check on P1 (7.2→4.0); P3-vs-S4 calibration at 4.8. |
| `delivery` | Ratify or reject R3 at 2.5; defend D3's count rising 8→18 while its rating fell 7.0→5.5; whether D5 is a legitimate finding or an aggregate needing a split. |

**The chair's position:** round 2 is partial and is recorded as partial. The two
rulings it did produce (R6, R7) rest on reported evidence and are sound. The
S1 and I1 questions are the two most consequential still open, and neither
should be treated as settled by silence.
