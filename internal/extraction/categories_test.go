package extraction

import (
	"context"
	"slices"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/db"
)

// extraction E12: rules-only mode proposed no categories at all, so with no
// API key the category filter was dead UI.
func TestRuleCategories_FromTitleAndHashtags(t *testing.T) {
	for name, tc := range map[string]struct {
		caption string
		want    []string
	}{
		"inflected title word and hashtags": {
			caption: "🍌 Veganes Bananenbrot 🍌\n\nZutaten:\n- 3 Bananen\n- 250 g Mehl\n\nZubereitung:\nAlles verrühren und backen.\n\n#vegan #backen",
			want:    []string{"Vegan", "Backen"},
		},
		"compound hashtags": {
			caption: "Overnight Oats\n\nZutaten:\n- 50 g Haferflocken\n- 150 ml Milch\n\nZubereitung:\nÜber Nacht quellen lassen.\n#frühstücksidee #vegetarischerezepte",
			want:    []string{"Frühstück", "Vegetarisch"},
		},
		"english": {
			caption: "Fluffy Pancakes\n\nIngredients:\n- 200 g flour\n- 2 eggs\n\nInstructions:\nWhisk and fry.\n#breakfast",
			want:    []string{"Frühstück"},
		},
		"hyphenated title": {
			caption: "Grüner Power-Smoothie\n\nZutaten:\n- 1 Banane\n- 200 ml Wasser\n\nZubereitung:\nMixen.",
			want:    []string{"Getränk"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, _ := NewRuleBasedExtractor().Extract(context.Background(), tc.caption)
			if !slices.Equal(got.Categories, tc.want) {
				t.Errorf("Categories = %v, want %v", got.Categories, tc.want)
			}
		})
	}
}

// A wrong category is worse than none: it files a recipe where nobody looks for
// it, and may publish it without review. Each of these is a keyword in a place
// that does not describe the dish.
func TestRuleCategories_IgnoreWhatDoesNotDescribeTheDish(t *testing.T) {
	for name, caption := range map[string]string{
		// How a gratin ends is not a sign it belongs under Backen.
		"verb in the method": "Gemüseauflauf\n\nZutaten:\n- 600 g Kartoffeln\n- 200 ml Sahne\n\nZubereitung:\nSchichten und 40 Minuten bei 190 Grad backen.",
		// One vegan ingredient does not make the dish vegan.
		"ingredient": "Käsespätzle\n\nZutaten:\n- 400 g Spätzle\n- 20 g vegane Butter\n\nZubereitung:\nSchmelzen und servieren.",
		// A smoothie bowl is eaten; only the whole word means a drink.
		"compound of a whole-word keyword": "Beeren-Bowl\n\nZutaten:\n- 200 g Beeren\n- 1 Banane\n\nZubereitung:\nPürieren und toppen.\n#smoothiebowl",
		// And a Cocktailsoße is a sauce.
		"sauce": "Cocktailsoße\n\nZutaten:\n- 3 EL Mayonnaise\n- 1 EL Ketchup\n\nZubereitung:\nVerrühren.",
		// "Veggies" are vegetables, not a diet.
		"english veggies": "Chicken Stir Fry\n\nIngredients:\n- 300 g chicken\n- 200 g veggies\n\nInstructions:\nFry everything.\n#veggies",
	} {
		t.Run(name, func(t *testing.T) {
			got, _ := NewRuleBasedExtractor().Extract(context.Background(), caption)
			if got.Confidence <= 0 {
				t.Fatalf("Confidence = 0; the case needs a caption the rules accept")
			}
			if len(got.Categories) != 0 {
				t.Errorf("Categories = %v, want none", got.Categories)
			}
		})
	}
}

// A caption with no recipe in it is discarded by the pipeline, and categories
// on it would describe nothing.
func TestRuleCategories_NoneWithoutARecipe(t *testing.T) {
	got, _ := NewRuleBasedExtractor().Extract(context.Background(), "Bestes Frühstück heute ☀️ #vegan #breakfast")
	if got.Confidence != 0 || len(got.Categories) != 0 {
		t.Errorf("(Confidence, Categories) = (%v, %v), want (0, none)", got.Confidence, got.Categories)
	}
}

// Every category the rules can propose must be one the database seeds. The
// import would drop any other name — so a list that drifted from the seed would
// fail quietly, one category at a time, which is what this catches instead.
func TestRuleCategories_AreAllSeeded(t *testing.T) {
	seeded := db.DefaultCategories()
	for _, c := range ruleCategories {
		if !slices.Contains(seeded, c.name) {
			t.Errorf("rules propose %q, which internal/db does not seed (%v)", c.name, seeded)
		}
	}
}

func TestMatchesKeyword(t *testing.T) {
	for _, tc := range []struct {
		word, keyword string
		want          bool
	}{
		{"veganfood", "vegan*", true},
		{"nichtvegan", "vegan*", false},
		{"smoothie", "smoothie", true},
		{"smoothies", "smoothie", true},
		{"smoothiebowl", "smoothie", false},
		{"cocktails", "cocktail", true},
		{"cocktailsoße", "cocktail", false},
	} {
		if got := matchesKeyword(tc.word, tc.keyword); got != tc.want {
			t.Errorf("matchesKeyword(%q, %q) = %v, want %v", tc.word, tc.keyword, got, tc.want)
		}
	}
}
