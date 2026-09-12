# Recipe Reader — Expert Panel Review, 2026-09-11

Six independent reviews of the Go codebase, one per specialist defined in
`agents.yaml`, which lives one level above the repository root.

**Process note — read this first.** The panel design in `agents.yaml` has seven
members: six specialists who review independently, then a `review_chair` who
cross-examines them. **Both stages have now run**, in one round with no
iteration. The six reviews below are the independent passes with post-round
ratings applied; the [**consensus record**](2026-09-11-consensus.md) carries the
chair's rulings, the merges, and **nine corrections established during the round
that contradict what the independent reviews stated** — including one finding
whose premise turned out to be false.

A **second round** tested the ratings themselves. It is **partial**: two of six
reviewers reported before an infrastructure limit cut the rest off, so
`security` S1's possible up-rating and `integration` I1's unresolved magnitude
are still open. Two ratings moved — `go` #6 up to **6.4**, `extraction` E4 down
to **4.8** — and the panel established that the fix for E7 already exists on an
unmerged branch.

Read the consensus record first if you are acting on any of this.

## Rating scale

Every finding carries a rating from **0.1 to 10.0**, on a scale shared by all
six reviewers so the numbers are comparable across reviews. It weighs three
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

**Nothing was rated 9.0 or above.** No reviewer found a defect that destroys
data or is remotely exploitable as the code stands. The ceiling after the
cross-examination is **8.5**.

## The reviews

| Review | Focus | Findings | Mean | Highest |
| --- | --- | --- | --- | --- |
| [**consensus record**](2026-09-11-consensus.md) | Chair's rulings, merges, corrections, open dissent | — | — | — |
| [`go_reviewer`](2026-09-11-go-reviewer.md) | Idiomatic Go, error handling, concurrency, API surface | 13 | 3.5 | 5.8 |
| [`persistence_reviewer`](2026-09-11-persistence-reviewer.md) | Schema, migrations, sqlc queries, transactions, pgx | 9 | 3.3 | 5.0 |
| [`extraction_reviewer`](2026-09-11-extraction-reviewer.md) | Rule/LLM/hybrid extraction, confidence, cost control | 12 | 5.0 | 7.8 |
| [`security_reviewer`](2026-09-11-security-reviewer.md) | Prompt injection, secrets, trust boundaries, CVEs | 8 | 4.4 | 8.5 |
| [`integration_reviewer`](2026-09-11-integration-reviewer.md) | Instagram client resilience, rate limits, pagination | 11 | 5.2 | 7.6 |
| [`delivery_reviewer`](2026-09-11-delivery-reviewer.md) | Test strategy, CI, packaging, reproducible builds | 13 | 4.2 | 7.2 |

**66 findings, mean 4.28** — down from 72 and 4.48 entering the round (one
withdrawn, five merged, thirteen ratings lowered, two raised). Extraction and
integration still carry the highest means: their subject matter is the
unofficial Instagram API and the LLM pipeline, the parts of this system whose
behaviour is governed by something outside the repository. Delivery fell
furthest, from 4.7 to 4.2, because its second-highest finding rested on a
premise that proved false.

## Findings rated 7.0 and above

| Rating | ID | Finding | Review | Movement |
| --- | --- | --- | --- | --- |
| **8.5** | S1 | No authentication on any endpoint, combined with `Access-Control-Allow-Origin: *` | security | held — and understated; **possible up-rating unresolved** |
| **7.8** | E2 | Confidence hard-coded to 0.9, so every LLM result auto-publishes unreviewed | extraction | held, claim narrowed |
| **7.6** | I1 | A backlog over 50 posts never imports — and the tally looks healthy | integration | 8.6 → 7.6, **magnitude unresolved** |
| **7.6** | I2 | No session refresh: an expired Instagram session disables imports until restart | integration | unchallenged |
| **7.4** | I3 | No timeout at any layer — the HTTP client has none and the API takes no context | integration | held, absorbs `go` #3 |
| **7.2** | D1 | `make build` silently produces a binary serving the placeholder UI | delivery | 7.8 → 7.2, reproduced |
| **7.0** | E1 | Non-recipe captions still create rows | extraction | 8.2 → 7.0 |

Three of these share a shape worth naming: **the failure is invisible.** I1
reports `Imported: 0` as a healthy run, E2 auto-publishes unreviewed
extractions without a warning, and D1 produces a binary that looks complete.

Three findings left this table in the round — `persistence` P1 (7.2 → 4.0, the
race is latent), `delivery` D2 (7.2 → 2.5, premise false), and `delivery` D3
(7.0 → 5.5, a cost rather than a defect).

## Toolchain baseline

Every reviewer worked against the same clean baseline, verified on 2026-09-11:

| Check | Result |
| --- | --- |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `golangci-lint run ./...` | 0 issues |
| `govulncheck ./...` | 0 reachable vulnerabilities (1 unfixable module advisory, not called) |

Every finding in every review is something the configured tooling cannot detect.

## How the overlaps were resolved

The chair merged five duplicate pairs and preserved one three-way split
deliberately. Full reasoning is in the [consensus record](2026-09-11-consensus.md);
the outcomes:

| Defect | Entering | Resolution |
| --- | --- | --- |
| A unit test really logs in to Instagram | `delivery` D2 **7.2** vs `integration` I12 **3.0** | **2.5, merged.** Both premises false — the test makes no network call. Chair ruling R3. |
| Errors discarded without logging | `go` #5 **6.8**, `extraction` E8 **5.2**, `integration` I10 **3.6** | **Split preserved: 5.8 / 6.0 / 3.6.** One theme, three sites, three signals. Chair ruling R2. |
| `status` accepts out-of-domain values | `go` #2 **6.5** vs `persistence` P7 **3.5** | **5.5, merged.** Both moved and crossed; fix must be both halves. Chair ruling R1. |
| Unbounded request handling | `security` S3 **5.4** vs `go` #8 **4.0** | **4.8, merged under security** (superset scope). Chair ruling R4. |
| No context or timeout on the Instagram path | `integration` I3 **7.4** vs `go` #3 **6.9** | **7.4, merged under integration.** The proposed fix is not implementable. |
| `LLMExtractor` untestable / its code untested | `delivery` D5 **6.0** vs `extraction` E7 **4.6** | **Both stand: 6.0 and 5.5** (E7 raised). A blocker does not inherit what it blocks. |
| The down migration is never run | `persistence` P11 **2.6** and `delivery` D12 **2.6** | **2.6, merged; delivery owns.** Needs a new exported Down API. |

**One causal claim was withdrawn.** The index previously asserted that
`extraction` E1 creates the junk rows `persistence` P2 leaves behind. It does
not — the sentinel path returns an empty ingredients list, so junk rows carry no
ingredients. See correction 3 in the consensus record.

## Working plan

[**2026-09-11-working-plan.md**](2026-09-11-working-plan.md) turns these 72
findings into **63 tasks grouped by rating band**, with the nine duplicate
findings merged into single work items and a short sequencing note for the
three tasks that unblock others.

## Suggested reading order

Start with the [consensus record](2026-09-11-consensus.md) — it carries nine
corrections, three of which would have produced misdirected work if the plan had
been executed as originally written.

Then: **S1 (8.5)** is the panel's highest-rated finding, held under the hardest
challenge of the round and worse than first written. **E2 (7.8)** is the one
that quietly fills the database with unreviewed data. **I1 (7.6)** is the one
that makes the product not do its job — though its magnitude is the round's one
unresolved dissent. **D1 (7.2)** is the cheapest fix in the High band.
