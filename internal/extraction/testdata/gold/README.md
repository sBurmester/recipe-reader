# Extraction gold set

Twenty-four captions with the `ExtractedRecipe` a human says is correct for
each. `gold_test.go` scores the extractors against them.

Every other decision in this package — which model to default to, how the
confidence score is shaped, how a title is picked, any edit to the system
prompt — was previously argued from intuition, because nothing measured the
result. This is the thing to measure against.

## Provenance — read this before trusting a number

**These captions are written, not collected.** They reproduce the shapes the
rules target and the ones they are known to miss, but no real saved post is in
here: the Instagram endpoints have never been run against a live account
(integration finding I11 / task T-43, still open). The scores below therefore
say how the extractors behave on captions someone expected; they do not yet say
how they behave on the feed.

When T-43 lands and commits a real response fixture, add real captions here —
one JSON file each, `origin` set to something other than `synthetic` — and
re-measure. Expect the floors to move; that is the point of measuring.

## Case format

One JSON file per case:

```json
{
  "id": "kuerbis-risotto",
  "origin": "synthetic",
  "note": "why this caption is in the corpus",
  "caption": "the caption as it would arrive",
  "expect": {
    "no_recipe": false,
    "name": "Cremiger Kürbis-Risotto",
    "ingredients": [{ "name": "Risottoreis", "amount": 300, "unit": "g" }],
    "instructions_contain": ["andünsten"],
    "rules": { "min_confidence": 0.95, "max_confidence": 1.0 }
  }
}
```

`expect` is the truth, not what the parser currently produces. A case that
scores below 1.00 is a recorded gap, not a broken test — `chili-mengenbereich`
(amount ranges) and `hefezopf-lang` (`Würfel` is not in the unit table) are
both in the corpus precisely because they are misses.

`expect.rules` is optional and bounds the rules extractor's confidence for that
caption. Use it where the case exists to pin a behaviour (a half recipe must
stay under the fallback threshold), not to freeze a number.

## Running it

```bash
# hermetic, runs in CI, no key needed
go test ./internal/extraction -run GoldSetRules -v

# the same corpus through the configured provider. Opt-in: it costs money.
RECIPE_READER_EVAL_LLM=1 ANTHROPIC_API_KEY=sk-... \
  go test ./internal/extraction -run GoldSetLLM -v
```

Both print a per-case table of precision, recall, F1, confidence and
amount/unit exactness. Read the table when an aggregate floor moves: the
aggregate says something changed, the table says which caption.

## What is gated and what is only reported

Gated, in `gold_test.go`:

- a `no_recipe` caption must score exactly 0 — the pipeline stores everything
  above it;
- a recipe caption must score above 0;
- per-case `min_confidence` / `max_confidence`, where set;
- `instructions_contain` substrings;
- mean ingredient precision, mean recall and title accuracy across the corpus.

Reported only: per-case F1, amount and unit exactness. They are the numbers to
watch when the unit alias table or the ingredient-line regex changes, and
gating them would turn an improvement into a test edit.

The aggregate floors sit a little under what the corpus currently scores. They
are a regression signal, not a target — raising them to chase a number produces
a parser tuned to twenty-four captions.
