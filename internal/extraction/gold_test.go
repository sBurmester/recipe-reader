package extraction

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The gold set answers a question the unit tests in this package cannot: not
// "does this function do what it did yesterday" but "how good is extraction".
// Every decision here — the model default, the confidence scoring, the title
// heuristic, an edit to the prompt — used to be argued from intuition, because
// nothing measured the result. This is what it is measured against.
//
// It scores ingredient-level precision and recall rather than demanding exact
// equality, because exact equality on a heuristic parser produces a test that
// is rewritten every time the parser improves. The per-case bounds are a floor
// under a documented behaviour, the aggregate floors below are a regression
// signal for the corpus as a whole, and the report both runs print is the
// thing to read when a number moves.
//
// Run it: go test ./internal/extraction -run GoldSet -v

// The aggregate floors. They sit a little under what the corpus currently
// scores, so an ordinary improvement does not have to edit them and a real
// regression cannot pass. They are deliberately not targets: raising them to
// chase a number produces a parser tuned to twenty-four captions.
const (
	minMeanIngredientRecall    = 0.95
	minMeanIngredientPrecision = 0.95
	minNameAccuracy            = 0.95
)

type goldCase struct {
	ID      string `json:"id"`
	Origin  string `json:"origin"`
	Note    string `json:"note"`
	Caption string `json:"caption"`
	Expect  struct {
		NoRecipe            bool             `json:"no_recipe"`
		Name                string           `json:"name"`
		Ingredients         []goldIngredient `json:"ingredients"`
		InstructionsContain []string         `json:"instructions_contain"`
		Rules               *struct {
			MinConfidence *float64 `json:"min_confidence"`
			MaxConfidence *float64 `json:"max_confidence"`
		} `json:"rules"`
	} `json:"expect"`
}

type goldIngredient struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

func loadGoldSet(t *testing.T) []goldCase {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "gold", "*.json"))
	if err != nil {
		t.Fatalf("glob gold set: %v", err)
	}
	if len(paths) < 20 {
		t.Fatalf("gold set has %d cases, want at least 20 — see testdata/gold/README.md", len(paths))
	}
	sort.Strings(paths)

	cases := make([]goldCase, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var c goldCase
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if c.ID == "" || c.Caption == "" {
			t.Fatalf("%s: id and caption are required", path)
		}
		if seen[c.ID] {
			t.Fatalf("%s: duplicate case id %q", path, c.ID)
		}
		seen[c.ID] = true
		cases = append(cases, c)
	}
	return cases
}

// score is one case's measurement.
type score struct {
	id                 string
	precision, recall  float64
	nameOK             bool
	confidence         float64
	missing, spurious  []string
	amountsOK, unitsOK int
	matched            int
}

func (s score) f1() float64 {
	if s.precision+s.recall == 0 {
		return 0
	}
	return 2 * s.precision * s.recall / (s.precision + s.recall)
}

// scoreIngredients matches got against want by normalised name, as multisets,
// and reports precision, recall and what was missed on either side.
func scoreIngredients(want []goldIngredient, got []ExtractedIngredient) score {
	remaining := make([]int, 0, len(got))
	for i := range got {
		remaining = append(remaining, i)
	}

	var s score
	for _, w := range want {
		idx := -1
		for pos, gi := range remaining {
			if normalizeName(got[gi].Name) == normalizeName(w.Name) {
				idx, remaining = gi, append(remaining[:pos], remaining[pos+1:]...)
				break
			}
		}
		if idx < 0 {
			s.missing = append(s.missing, w.Name)
			continue
		}
		s.matched++
		if math.Abs(got[idx].Amount-w.Amount) < 1e-9 {
			s.amountsOK++
		}
		if strings.EqualFold(got[idx].Unit, w.Unit) {
			s.unitsOK++
		}
	}
	for _, gi := range remaining {
		s.spurious = append(s.spurious, got[gi].Name)
	}

	if len(got) > 0 {
		s.precision = float64(s.matched) / float64(len(got))
	} else if len(want) == 0 {
		s.precision = 1
	}
	if len(want) > 0 {
		s.recall = float64(s.matched) / float64(len(want))
	} else {
		s.recall = 1
	}
	return s
}

// normalizeName is the match key: case-folded and whitespace-collapsed, so
// "Rote Linsen" and "rote  linsen" are the same ingredient. Nothing stronger —
// stemming would let a genuinely wrong extraction score as a match.
func normalizeName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// TestGoldSetRules is the hermetic half: no network, no key, runs in CI.
func TestGoldSetRules(t *testing.T) {
	cases := loadGoldSet(t)
	e := NewRuleBasedExtractor()

	var scores []score
	names, namesRight := 0, 0
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			got, err := e.Extract(context.Background(), c.Caption)
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}

			if c.Expect.NoRecipe {
				// The one hard requirement of the corpus: a caption with no
				// recipe in it must not clear zero, because the pipeline
				// stores everything that does.
				if got.Confidence != 0 {
					t.Errorf("Confidence = %v, want 0 for a caption with no recipe (name %q, %d ingredients)",
						got.Confidence, got.Name, len(got.Ingredients))
				}
				return
			}

			if got.Confidence <= 0 {
				t.Errorf("Confidence = 0 for a caption that does contain a recipe")
			}
			if b := c.Expect.Rules; b != nil {
				if b.MinConfidence != nil && got.Confidence < *b.MinConfidence {
					t.Errorf("Confidence = %v, want >= %v", got.Confidence, *b.MinConfidence)
				}
				if b.MaxConfidence != nil && got.Confidence > *b.MaxConfidence {
					t.Errorf("Confidence = %v, want <= %v", got.Confidence, *b.MaxConfidence)
				}
			}
			for _, want := range c.Expect.InstructionsContain {
				if !strings.Contains(got.Instructions, want) {
					t.Errorf("Instructions do not contain %q", want)
				}
			}

			s := scoreIngredients(c.Expect.Ingredients, got.Ingredients)
			s.id, s.confidence = c.ID, got.Confidence
			s.nameOK = normalizeName(got.Name) == normalizeName(c.Expect.Name)
			scores = append(scores, s)
			if c.Expect.Name != "" {
				names++
				if s.nameOK {
					namesRight++
				}
			}
		})
	}

	t.Log("\n" + report(scores))
	if len(scores) == 0 {
		t.Fatal("no recipe cases were scored")
	}

	var sumP, sumR float64
	matched, amountsOK, unitsOK := 0, 0, 0
	for _, s := range scores {
		sumP += s.precision
		sumR += s.recall
		matched += s.matched
		amountsOK += s.amountsOK
		unitsOK += s.unitsOK
	}
	meanP, meanR := sumP/float64(len(scores)), sumR/float64(len(scores))
	if meanP < minMeanIngredientPrecision {
		t.Errorf("mean ingredient precision = %.3f, want >= %.2f", meanP, minMeanIngredientPrecision)
	}
	if meanR < minMeanIngredientRecall {
		t.Errorf("mean ingredient recall = %.3f, want >= %.2f", meanR, minMeanIngredientRecall)
	}
	if names > 0 {
		if acc := float64(namesRight) / float64(names); acc < minNameAccuracy {
			t.Errorf("title accuracy = %.3f (%d/%d), want >= %.2f", acc, namesRight, names, minNameAccuracy)
		}
	}
	// Amount and unit exactness are reported, not gated: they are the metric
	// to watch when the unit alias table or the line regex changes, and they
	// are measured only over ingredients that matched by name at all.
	t.Logf("corpus: %d scored cases, mean precision %.3f, mean recall %.3f, titles %d/%d, "+
		"amounts %.3f, units %.3f (of %d matched ingredients)",
		len(scores), meanP, meanR, namesRight, names,
		ratio(amountsOK, matched), ratio(unitsOK, matched), matched)
}

// report renders the per-case table. It is the output to read when a floor
// moves: the aggregate says something changed, this says which caption.
func report(scores []score) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-34s %5s %5s %5s %5s %5s %5s  %s\n",
		"case", "prec", "rec", "f1", "conf", "amt", "unit", "missing / spurious")
	for _, s := range scores {
		notes := ""
		if len(s.missing) > 0 {
			notes += "missing " + strings.Join(s.missing, ", ")
		}
		if len(s.spurious) > 0 {
			if notes != "" {
				notes += "; "
			}
			notes += "spurious " + strings.Join(s.spurious, ", ")
		}
		if !s.nameOK {
			if notes != "" {
				notes += "; "
			}
			notes += "title differs"
		}
		fmt.Fprintf(&b, "%-34s %5.2f %5.2f %5.2f %5.2f %5.2f %5.2f  %s\n",
			s.id, s.precision, s.recall, s.f1(), s.confidence,
			ratio(s.amountsOK, s.matched), ratio(s.unitsOK, s.matched), notes)
	}
	return b.String()
}

// ratio is n/total, and 1 for an empty total — a case with no matched
// ingredient has nothing to be wrong about.
func ratio(n, total int) float64 {
	if total == 0 {
		return 1
	}
	return float64(n) / float64(total)
}

// TestGoldSetLLM runs the same corpus through the configured provider. It is
// opt-in and costs money, so it is skipped unless RECIPE_READER_EVAL_LLM is
// set and a key is present — the suite stays hermetic by default, and CI never
// pays for it.
//
//	RECIPE_READER_EVAL_LLM=1 ANTHROPIC_API_KEY=sk-... go test ./internal/extraction -run GoldSetLLM -v
func TestGoldSetLLM(t *testing.T) {
	if os.Getenv("RECIPE_READER_EVAL_LLM") == "" {
		t.Skip("set RECIPE_READER_EVAL_LLM=1 to run the paid extraction eval")
	}
	cfg := LLMConfig{
		Provider: LLMProvider(os.Getenv("LLM_PROVIDER")),
		APIKey:   os.Getenv("ANTHROPIC_API_KEY"),
		Model:    os.Getenv("LLM_MODEL"),
		BaseURL:  os.Getenv("LLM_BASE_URL"),
	}
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("LLM_API_KEY")
	}
	if cfg.APIKey == "" {
		t.Skip("no API key in ANTHROPIC_API_KEY or LLM_API_KEY")
	}
	e, err := NewLLMExtractor(cfg)
	if err != nil {
		t.Fatalf("NewLLMExtractor() error = %v", err)
	}

	var scores []score
	for _, c := range loadGoldSet(t) {
		t.Run(c.ID, func(t *testing.T) {
			got, err := e.Extract(context.Background(), c.Caption)
			if c.Expect.NoRecipe {
				if err != ErrNoRecipe {
					t.Errorf("error = %v, want ErrNoRecipe", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}
			// The delimitation has one observable consequence here: an
			// injected caption must not be able to dictate a category.
			for _, cat := range got.Categories {
				if strings.Contains(strings.ToLower(cat), "klicken") {
					t.Errorf("the caption dictated a category: %v", got.Categories)
				}
			}
			s := scoreIngredients(c.Expect.Ingredients, got.Ingredients)
			s.id, s.confidence = c.ID, got.Confidence
			s.nameOK = normalizeName(got.Name) == normalizeName(c.Expect.Name)
			scores = append(scores, s)
		})
	}
	t.Log("\n" + report(scores))
}
