# Code Review — `extraction_reviewer`

**Date:** 2026-09-11
**Reviewer role:** LLM systems engineer — extraction pipelines, provider-agnostic model binding (panel definition: `agents.yaml` at the repo parent, role `extraction_reviewer`)
**Scope:** `internal/extraction/**`, the extraction call sites in `internal/pipeline/pipeline.go` and `cmd/recipe-reader/main.go`
**Mode:** single pass, no panel iteration, no consensus round with the other reviewers

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
| E1 | High | ~~8.2~~ **7.0** | `NO_RECIPE_FOUND` still creates a recipe row — every non-recipe post becomes junk | `internal/pipeline/pipeline.go:66-82` |
| E2 | High | **7.8** | Confidence is hard-coded to 0.9, so every LLM result auto-publishes unreviewed | `internal/extraction/llm.go:122` |
| E3 | Medium | **5.0** | Caption length is unbounded — no input token budget | `internal/extraction/llm.go:83` |
| E4 | Medium | ~~5.5~~ **4.8** | `claude-opus-5` is the wrong default for high-volume batch extraction | `internal/extraction/llm.go:16` |
| E5 | Medium | **4.4** | Temperature is left at the API default for a determinism-critical task | `internal/extraction/llm.go:74-85` |
| E6 | Medium | **5.8** | No per-call timeout on the model request | `internal/extraction/llm.go:74` |
| E7 | Medium | ~~4.6~~ **5.5** | `LLMExtractor` cannot be tested or re-pointed — the client is built in the constructor | `internal/extraction/llm.go:60-68` |
| E8 | Medium | ~~5.2~~ **6.0** | The hybrid's LLM failure is swallowed silently | `internal/extraction/hybrid.go:34-37` |
| E9 | Low | **3.4** | Rule confidence is a three-valued score driving a fine-grained threshold | `internal/extraction/rules.go:116-125` |
| E10 | Low | **3.2** | The recipe name is whatever the first line happens to be | `internal/extraction/rules.go:61,75-82` |
| E11 | Low | **3.8** | No eval harness or gold set — extraction quality is unmeasured | `internal/extraction/*_test.go` |
| E12 | Low | **2.6** | Rules-only mode produces no categories, ever | `internal/extraction/rules.go:60-70` |

## High

### E1. `NO_RECIPE_FOUND` still creates a recipe row

**Rating: 7.0 / 10** — revised down from 8.2. Mechanism confirmed with a full trace, but the row lands in `needs_review` and the UI badges it, so this is visible queue noise rather than silent corruption. **Correction:** the claim below that junk rows also create orphan ingredient rows is withdrawn — the sentinel path returns an empty ingredients list. The E1 → `persistence` P2 causal link is void.

**Location:** `internal/pipeline/pipeline.go:66-82`, sentinel defined at `internal/extraction/llm.go:18,129-131`

The design is sound up to the last step: the system prompt instructs the model to signal a non-recipe caption with `instructions = "NO_RECIPE_FOUND"`, and `parseToolInput` correctly maps that to `Confidence: 0`. Then the pipeline ignores it:

```go
extracted, err := p.Extractor.Extract(ctx, post.Caption)
if err != nil { result.Failed++; continue }
recipe, err := p.toRecipe(ctx, post, extracted)   // ← no confidence gate
...
if err := p.Recipes.Create(ctx, recipe); err != nil { ... }
result.Imported++
```

`toRecipe` only uses `Confidence` to pick `needs_review` vs `published` (`:91-94`). There is no branch that declines to store. So a saved post that is a gym selfie is imported as a recipe named whatever the model returned, with `instructions` set to the literal string `NO_RECIPE_FOUND`, counted as `Imported`.

The rules-only path is worse, because it has no sentinel at all: a caption with no `Zutaten:`/`Zubereitung:` headers yields `Confidence 0`, `Ingredients` empty, `Instructions` empty, and `Name` = the first line of the caption — and that is stored too. Importing a mixed saved-posts collection therefore creates one row per post regardless of whether any of them are recipes. Both `FindOrCreateIngredient` rows and the `recipes` row persist, so this also feeds `persistence_reviewer`'s orphan-lookup concern.

**Fix:** gate on confidence in the pipeline, and count the skip distinctly so the tally stays honest:

```go
if extracted.Confidence <= 0 {
    result.Skipped++   // or a new result.NoRecipe counter
    slog.Info("import: no recipe in caption", "source", post.Source)
    continue
}
```

Treating the sentinel as a *typed* outcome rather than a magic instructions string would be sturdier still — have `parseToolInput` return `(nil, nil)` or an `ErrNoRecipe` sentinel, so a model that paraphrases the marker cannot slip a junk row through.

### E2. Confidence is hard-coded to 0.9, so every LLM result auto-publishes unreviewed

**Rating: 7.8 / 10** — held under challenge; arithmetic re-verified end to end. One narrowing: `needs_review` is not unreachable in absolute terms — it is reachable via the zero-confidence sentinel and the non-LLM paths. The accurate claim is that it is unreachable for any LLM result that actually contains a recipe.

**Location:** `internal/extraction/llm.go:100-131`

```go
out := &ExtractedRecipe{..., Confidence: 0.9}
...
if out.Instructions == "NO_RECIPE_FOUND" { out.Confidence = 0 }
```

`Confidence` is binary — 0.9 or 0 — and carries no information about the extraction at all. With `EXTRACTION_CONFIDENCE_THRESHOLD` defaulting to `0.6` (`internal/config/config.go:27`), `0.9 ≥ 0.6` means **every** LLM extraction is stored as `published` (`internal/pipeline/pipeline.go:91-94`). The `needs_review` state, and the whole human-review workflow the frontend builds on (`web/src/pages/list.ts:80`), is unreachable on the LLM path. A hallucinated ingredient list from a marginal caption is indistinguishable from a clean extraction and goes straight to published.

This is the warning sign my brief names explicitly, and here it is load-bearing: the same constant feeds both the hybrid's fallback decision and the publish/review decision, which are different questions.

**Fix:** derive a real signal instead of asserting one. Cheapest useful version — have the model report it, as a field on the tool schema it already must fill:

```go
"confidence": map[string]any{
    "type": "number", "minimum": 0, "maximum": 1,
    "description": "How certain you are that the caption contained a complete, real recipe.",
},
```

and add it to `Required`. Combine it with structural checks you can compute for free and trust more than self-report — ingredient count, whether instructions have more than one sentence, whether any ingredient got an amount — e.g. `min(modelConfidence, structuralScore)`. Then separate the two thresholds: one for "rules were good enough to skip the LLM", one for "good enough to publish without review". They are currently the same number doing two jobs.

## Medium

### E3. Caption length is unbounded — no input token budget

**Rating: 5.0 / 10**

**Location:** `internal/extraction/llm.go:83`

```go
Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(caption))},
```

`caption` comes straight from `extractMedia` (`internal/instagram/saved.go:123-127`) with no length check anywhere in between. `MaxTokens: 2048` correctly caps the *output*; nothing caps the input. Instagram captions run to 2,200 characters normally, but the field is attacker-influenced (see `security_reviewer` on prompt injection) and the JSON path does not validate it. Per-run cost is `maxItems` (50) × unbounded input.

**Fix:** truncate defensively before the call, and make the cap a named constant so it is visible next to `MaxTokens`:

```go
const maxCaptionRunes = 8000 // ~2-3k tokens; real captions cap at 2,200 chars
if len([]rune(caption)) > maxCaptionRunes {
    caption = string([]rune(caption)[:maxCaptionRunes])
}
```

Truncating by runes, not bytes, matters here — these captions are German and emoji-heavy, and byte-slicing produces invalid UTF-8.

### E4. `claude-opus-5` is the wrong default for high-volume batch extraction

**Rating: 4.8 / 10** — lowered by this reviewer in round 2 (chair ruling R7), breaking its own three-way tie at 5.5 in its own lane's disfavour: this is the only one of the three whose failure announces itself, on the first invoice.

**Location:** `internal/extraction/llm.go:12-16`, `internal/config/config.go:30`, `.env.example`

The doc comment states the tradeoff correctly — "operators can override it with a cheaper model for this high-volume, low-stakes batch extraction task" — and then defaults to the most expensive model in the family anyway. Defaults are what run in production. This task is single-turn, tool-constrained, schema-bounded extraction from short text: it is close to the canonical case for a small fast model, and Haiku 4.5 (`claude-haiku-4-5-20251001`) handles it at a fraction of the cost per post.

**Fix:** flip the default to Haiku 4.5 in both `defaultLLMModel` and the kong `default:` tag, and keep the comment — inverted, so it reads "operators can override upward if extraction quality proves insufficient." Then measure which is actually needed with E11's gold set rather than guessing; that is the decision the eval exists to settle.

### E5. Temperature is left at the API default

**Rating: 4.4 / 10**

**Location:** `internal/extraction/llm.go:74-85`

`MessageNewParams` sets `Model`, `MaxTokens`, `System`, `Tools`, `ToolChoice`, `Messages` — no `Temperature`. The API default is 1.0. For structured extraction that is the wrong end of the range: it makes the same caption yield different ingredient splits across runs, which undermines both reproducibility and any eval you build on top.

**Fix:** `Temperature: anthropic.Float(0)`. There is no creative latitude wanted here — the tool schema defines the output shape, and the job is faithful transcription of what the caption says.

### E6. No per-call timeout on the model request

**Rating: 5.8 / 10**

**Location:** `internal/extraction/llm.go:74`

`e.client.Messages.New(ctx, ...)` inherits whatever context the pipeline passes, which traces back to either the signal-derived context in `main` or — for an API-triggered run — `context.WithoutCancel` (`internal/api/handlers_import.go:24`), i.e. no deadline at all. One stalled request stalls the whole import; with `Worker`'s single-flight guard (`internal/pipeline/worker.go:55-60`) it also blocks every subsequent trigger indefinitely.

**Fix:** bound each call inside `Extract`, where the right duration is known:

```go
ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
defer cancel()
```

Worth noting what is already handled: the Anthropic Go SDK retries `429` and `5xx` twice by default with backoff, so the retry gap I would normally flag is covered by the dependency. A timeout is the piece it cannot supply for you.

### E7. `LLMExtractor` cannot be tested or re-pointed

**Rating: 5.5 / 10** — held in round 2. **The fix already exists, unmerged:** branch `feat/provider-agnostic-llm-extractor` (commit `4bbe23e`) carries a 211-line `llm_provider.go` and a 132-line `httptest`-driven test. It does **not** compile against `main` (constructor signature changed; `main.go` untouched), and it does **not** fix E2 — the `0.9`/`0` confidence values survive unchanged on the branch. — raised from 4.6. One variadic parameter also supplies the `WithRequestTimeout` seam E6 needs and the `WithBaseURL` re-pointing the package's provider-agnostic goal implies. Kept below `delivery` D5's 6.0, which it only partly unlocks.

**Location:** `internal/extraction/llm.go:60-68`

```go
func NewLLMExtractor(apiKey, model string) *LLMExtractor {
    return &LLMExtractor{client: anthropic.NewClient(option.WithAPIKey(apiKey)), model: model}
}
```

The client is constructed inside the constructor from a raw string, so there is no seam to inject a test double, a recorded transcript, or an alternate base URL. The visible consequence is in the tests: `internal/extraction/llm_test.go` covers `parseToolInput` in both branches and nothing else — `Extract` itself, including the tool-block scan at `:90-97` and the "no tool_use block" error path at `:97`, is entirely unexercised.

**Fix:** accept options and pass them through — one line, no interface needed:

```go
func NewLLMExtractor(apiKey, model string, opts ...option.RequestOption) *LLMExtractor {
    opts = append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
    return &LLMExtractor{client: anthropic.NewClient(opts...), model: model}
}
```

A test then points `option.WithBaseURL(httptestServer.URL)` at a canned tool-use response and covers `Extract` end to end.

On provider-agnosticism, which my brief asks about specifically: the `Extractor` interface (`internal/extraction/extractor.go:33`) is the right seam and it is clean — `Pipeline` depends only on it, `HybridExtractor` composes it, and swapping in a different provider means one new file implementing one method. Nothing in `domain` or `pipeline` mentions Anthropic. The only leak is this constructor. That is a good position to be in.

### E8. The hybrid's LLM failure is swallowed silently

**Rating: 6.0 / 10** — raised from 5.2, and the top of the three silent-error sites under chair ruling R2. This is the only one where the failure is invisible in *both* directions: the tally reports a successful run while extraction quality degrades.

**Location:** `internal/extraction/hybrid.go:34-37`

```go
llmResult, err := h.LLM.Extract(ctx, caption)
if err != nil {
    return result, nil   // err discarded entirely
}
```

Falling back to the weak rules result is the correct *behaviour* — an LLM hiccup should not fail the import, and the doc comment says so. The problem is that the error vanishes: with no logging anywhere in the package, an expired API key, a billing stop, or a persistent 429 presents as "extraction quality quietly got worse", indistinguishable from captions that simply parse badly. Every import keeps succeeding.

**Fix:** log before returning — `slog.Warn("extraction: llm fallback failed, using rules result", "error", err)` — and, once E2 gives you a real confidence number, mark the degraded result so the pipeline can route it to `needs_review` rather than treating it as an ordinary low-confidence parse.

## Low

### E9. Rule confidence is a three-valued score driving a fine-grained threshold

**Rating: 3.4 / 10**

**Location:** `internal/extraction/rules.go:116-125`

`confidenceFor` can only return `0`, `0.5`, or `1.0`. The threshold it is compared against is a float flag defaulting to `0.6` (`internal/config/config.go:27`). So the entire configurable range collapses to three behaviours: `≤ 0` → LLM, `(0, 0.5]` → LLM, `(0.5, 1.0]` → rules only. Setting the threshold to `0.55` or `0.9` changes nothing; setting it to `1.01` sends everything to the LLM. An operator tuning this flag gets no feedback until they cross an invisible cliff.

Worth pairing with the opposite failure: a caption with both headers present but garbage under them scores a perfect `1.0` and never reaches the LLM, no matter how bad the parse.

**Fix:** either make the score continuous — weight by ingredient count, fraction of ingredient lines that parsed, instruction length — or drop the float flag and make it an explicit enum of the three real modes. The float promises a precision the score does not have.

### E10. The recipe name is whatever the first line happens to be

**Rating: 3.2 / 10**

**Location:** `internal/extraction/rules.go:61`, `firstNonEmptyLine` at `:75-82`

For the caption shape these rules target, the first line is typically a hook, an emoji run, or a hashtag block — not a title. `Name` is never stripped of emoji, hashtags, or trailing punctuation, and it flows straight into `recipes.name`, which is the column the search filter runs against (`internal/db/queries/recipes.sql`). Bad titles degrade search, not just display.

**Fix:** prefer a line that looks like a title — short, no leading `#`, not ending in `:` — and fall back to the current behaviour; strip a leading emoji run and trailing hashtags. `Unbenanntes Rezept` (`:81`) is a good last resort and should probably also drop confidence, since a nameless caption is weak evidence of a recipe.

### E11. No eval harness or gold set — extraction quality is unmeasured

**Rating: 3.8 / 10**

**Location:** `internal/extraction/rules_test.go` (66 lines), `units_test.go` (26), `hybrid_test.go` (79), `llm_test.go` (43)

The unit tests are fine as unit tests: `hybrid_test.go` covers the fallback matrix properly, `units_test.go` pins the alias table. What does not exist is any measurement of *extraction quality* — no corpus of real captions with expected output, no precision/recall on ingredient lines, no regression signal when a regex or the prompt changes. Every decision in this package (E4's model choice, E9's scoring, E10's title heuristic, prompt edits) is currently argued from intuition because there is nothing to measure against.

**Fix:** a `testdata/gold/*.json` set of 20-30 real captions with expected `ExtractedRecipe` values, and a table test scoring ingredient-level precision/recall rather than demanding exact equality. Run the rules extractor against it in CI; run the LLM against it behind a build tag or an `ANTHROPIC_API_KEY`-gated skip, so it stays hermetic by default. Twenty captions is enough to catch a regex regression, which is the failure that will actually happen.

### E12. Rules-only mode produces no categories, ever

**Rating: 2.6 / 10**

**Location:** `internal/extraction/rules.go:60-70`

`RuleBasedExtractor` never populates `Categories`. That is defensible — no rule can infer "Vegetarisch" reliably — and `dto.go:78-80` even notes downstream that empty categories are ordinary. The consequence worth stating: with no API key configured, the hybrid degrades to rules-only (`cmd/recipe-reader/main.go:57-64`), the seeded category list (`internal/db/seed.go:17-20`) stays empty of associations, and the frontend's category filter is dead UI. A keyword pass over the caption against the nine seeded categories would cost nothing and make the degraded mode usable.

## What's right

- **Forced tool use is the correct structured-output mechanism**, and it is set up properly: a single tool, `ToolChoice` pinned to it by name, `Required` listing all four fields, and the schema mirroring `ExtractedRecipe` so the arguments unmarshal directly (`llm.go:23-49,78-81`). No free-text JSON parsing, no markdown-fence stripping, no retry-on-malformed loop. This is the version of this code that does not break.
- **`MaxTokens: 2048`** is set explicitly rather than defaulted — output cost is bounded per call even though input is not (E3).
- **The layering is genuinely provider-agnostic** where it counts: `Extractor` is a one-method interface, `Pipeline` holds only that interface, and `HybridExtractor` composes two of them with `nil` as a meaningful value for "no LLM configured". The rules-only degraded mode is a deliberate design point with the reasoning written down (`main.go:57-58`), not an accident.
- **`hybrid_test.go` covers the decision matrix**, including the case that matters most — LLM error with a low-confidence rules result still returning something usable.
- **The unit alias table** (`units.go:8-20`) is normalised the right way (lowercase, trailing dot stripped) and its canonical values are deliberately aligned with `db.defaultUnits` so `FindOrCreateUnit` hits existing rows instead of creating near-duplicates. The German aliases include the `oe`/`ue` transliterations, which is the detail that gets skipped.
- **`ingredientLineRe`** (`rules.go:17`) only treats a token as a unit when it is followed by whitespace, with a comment explaining that "1 Zwiebel" must not be sliced mid-word — and `parseIngredientLine` falls back to folding an unrecognised unit candidate back into the name (`:103-105`) rather than dropping it. That is the failure mode handled in the right direction.
