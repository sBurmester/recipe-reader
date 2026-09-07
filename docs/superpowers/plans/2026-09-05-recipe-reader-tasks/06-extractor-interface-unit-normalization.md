> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 2: Recipe Extraction Engine.
>
> **Status:** [x] done

# Task 6: Extractor Interface & Unit Normalization

**Files:**
- Create: `internal/extraction/extractor.go`, `internal/extraction/units.go`
- Test: `internal/extraction/units_test.go`

**Interfaces:**
- Produces:

```go
type ExtractedIngredient struct {
	Name   string
	Amount float64
	Unit   string
}

type ExtractedRecipe struct {
	Name         string
	Ingredients  []ExtractedIngredient
	Instructions string
	Categories   []string
	Confidence   float64 // 0..1, how sure the extractor is that this is a real, complete recipe
}

type Extractor interface {
	Extract(ctx context.Context, caption string) (*ExtractedRecipe, error)
}

func IsKnownUnit(candidate string) bool
func NormalizeUnit(candidate string) string
```

Task 7 (rules), Task 8 (LLM), Task 9 (hybrid), and Task 12 (pipeline) all implement/consume `Extractor` and `ExtractedRecipe` exactly as defined here — do not add fields without updating all four.

- [x] **Step 1: Write the failing test**

```go
// internal/extraction/units_test.go
package extraction

import "testing"

func TestIsKnownUnit(t *testing.T) {
	cases := map[string]bool{
		"g": true, "G": true, "EL": true, "el": true, "Stk.": true,
		"Mehl": false, "": false,
	}
	for input, want := range cases {
		if got := IsKnownUnit(input); got != want {
			t.Errorf("IsKnownUnit(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestNormalizeUnit(t *testing.T) {
	cases := map[string]string{
		"g": "g", "gramm": "g", "EL": "EL", "esslöffel": "EL", "Stk.": "Stück",
	}
	for input, want := range cases {
		if got := NormalizeUnit(input); got != want {
			t.Errorf("NormalizeUnit(%q) = %q, want %q", input, got, want)
		}
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extraction/... -v`
Expected: FAIL — package doesn't exist yet.

- [x] **Step 3: Implement `extractor.go`**

```go
// internal/extraction/extractor.go
package extraction

import "context"

type ExtractedIngredient struct {
	Name   string
	Amount float64
	Unit   string
}

type ExtractedRecipe struct {
	Name         string
	Ingredients  []ExtractedIngredient
	Instructions string
	Categories   []string
	Confidence   float64
}

type Extractor interface {
	Extract(ctx context.Context, caption string) (*ExtractedRecipe, error)
}
```

- [x] **Step 4: Implement `units.go`**

```go
// internal/extraction/units.go
package extraction

import "strings"

var unitAliases = map[string]string{
	"g": "g", "gramm": "g", "gr": "g",
	"kg": "kg", "kilogramm": "kg",
	"ml": "ml", "milliliter": "ml",
	"l": "l", "liter": "l",
	"el": "EL", "esslöffel": "EL", "essloeffel": "EL",
	"tl": "TL", "teelöffel": "TL", "teeloeffel": "TL",
	"stück": "Stück", "stk": "Stück", "stk.": "Stück", "stueck": "Stück",
	"prise": "Prise",
	"bund": "Bund",
	"dose": "Dose",
	"packung": "Packung", "pck": "Packung", "pck.": "Packung", "pkg": "Packung",
}

func normalizeKey(candidate string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(candidate), "."))
}

func IsKnownUnit(candidate string) bool {
	_, ok := unitAliases[normalizeKey(candidate)]
	return ok
}

func NormalizeUnit(candidate string) string {
	return unitAliases[normalizeKey(candidate)]
}
```

- [x] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/extraction/... -v`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add internal/extraction/extractor.go internal/extraction/units.go internal/extraction/units_test.go
git commit -m "$(cat <<'EOF'
feat: add Extractor interface and German unit normalization table

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 5](05-lookup-repository-categories-units-ingredients.md) · [Task 7 →](07-rule-based-extractor.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
