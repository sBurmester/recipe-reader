package extraction

import (
	"context"
	"math"
	"strings"
	"testing"
)

// extractRules runs the rule extractor, failing the test on the error it
// documents itself as never returning.
func extractRules(t *testing.T, caption string) *ExtractedRecipe {
	t.Helper()
	got, err := NewRuleBasedExtractor().Extract(context.Background(), caption)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	return got
}

func TestRecipeTitle(t *testing.T) {
	tests := []struct {
		name    string
		caption string
		want    string
	}{
		{
			name:    "emoji run is stripped from both ends",
			caption: "🔥🔥 Cremiger Kürbis-Risotto 🎃\n\nZutaten:\n- 300 g Reis",
			want:    "Cremiger Kürbis-Risotto",
		},
		{
			name:    "a leading hashtag block is skipped for the line after it",
			caption: "#rezept #foodporn #vegan\nOfengemüse mit Feta\n\nZutaten:\n- 1 Zucchini",
			want:    "Ofengemüse mit Feta",
		},
		{
			name:    "a long hook is skipped for the title under it",
			caption: "Ihr Lieben, dieses Rezept habe ich schon so oft für euch gemacht und ihr fragt immer wieder danach!\nSchoko-Bananenbrot\n\nZutaten:\n- 2 Bananen",
			want:    "Schoko-Bananenbrot",
		},
		{
			name:    "a trailing colon marks a header, not a title",
			caption: "Mein Lieblingsrezept:\nLinsensuppe\n\nZutaten:\n- 200 g Linsen",
			want:    "Linsensuppe",
		},
		{
			name:    "trailing hashtags are dropped from the title itself",
			caption: "Apfelkuchen #backen #herbst\n\nZutaten:\n- 3 Äpfel",
			want:    "Apfelkuchen",
		},
		{
			name:    "brackets survive the trailing trim",
			caption: "▶️ Bananenbrot (vegan)!\n\nZutaten:\n- 2 Bananen",
			want:    "Bananenbrot (vegan)",
		},
		{
			name:    "no usable line falls back to the placeholder",
			caption: "😍😍😍\n#food\n\nZutaten:\n- 100 g Mehl",
			want:    untitledRecipe,
		},
		{
			name:    "a caption that is only a long hook still uses it",
			caption: strings.Repeat("sehr langer text ", 6) + "\n\nZutaten:\n- 100 g Mehl",
			want:    strings.TrimSpace(strings.Repeat("sehr langer text ", 6)),
		},
		{
			name:    "the title is never taken from below a section header",
			caption: "Zutaten:\n- 100 g Mehl\n\nZubereitung:\nVerrühren.",
			want:    untitledRecipe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractRules(t, tt.caption).Name; got != tt.want {
				t.Errorf("Name = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestConfidenceIsContinuous is the point of the change: the score has to move
// between the three values it used to be pinned to, or the float thresholds it
// feeds are three settings wearing a continuous disguise.
func TestConfidenceIsContinuous(t *testing.T) {
	clean := extractRules(t, sampleCaption)
	if clean.Confidence != 1.0 {
		t.Fatalf("clean caption Confidence = %v, want 1.0", clean.Confidence)
	}

	// Same recipe, but half the ingredient section is prose the parser cannot
	// use. The old score called this identical to the clean caption.
	partial := extractRules(t, `Cremiger Kürbis-Risotto

Zutaten:
- 300 g Risottoreis
ich nehme immer den von meinem Lieblingshändler um die Ecke
und dazu noch was ihr sonst so im Kühlschrank findet ehrlich
- 400 g Kürbis

Zubereitung:
Zwiebel fein hacken und in Olivenöl andünsten.
Kürbis würfeln und dazugeben.
Reis zugeben und mit Brühe ablöschen, unter Rühren garen.`)

	if partial.Confidence >= clean.Confidence {
		t.Errorf("a half-parsed ingredient section scored %v, want less than the clean %v",
			partial.Confidence, clean.Confidence)
	}
	if partial.Confidence <= 0.5 {
		t.Errorf("a caption with both sections scored %v, want above 0.5", partial.Confidence)
	}

	// A short method scores below a full one, which is the term an operator
	// raising the publish threshold above 0.9 is reaching for.
	short := extractRules(t, "Rührei\n\nZutaten:\n- 3 Eier\n- 1 Prise Salz\n\nZubereitung:\nVerrühren.")
	long := extractRules(t, "Rührei\n\nZutaten:\n- 3 Eier\n- 1 Prise Salz\n\nZubereitung:\n"+
		"Eier in eine Schüssel schlagen und mit einer Prise Salz kräftig verrühren, bis die Masse gleichmäßig gelb ist.")
	if short.Confidence >= long.Confidence {
		t.Errorf("short instructions scored %v, want less than the long %v", short.Confidence, long.Confidence)
	}
}

// TestConfidenceBoundariesHold pins the three boundaries every caller was
// tuned against, so making the score continuous did not move them.
func TestConfidenceBoundariesHold(t *testing.T) {
	tests := []struct {
		name          string
		caption       string
		wantExactly   float64
		wantAtMost    float64
		wantAboveHalf bool
	}{
		{
			name:        "neither section is still exactly zero",
			caption:     "Schönes Foto vom Urlaub! #travel",
			wantExactly: 0,
		},
		{
			name:       "ingredients alone cannot exceed one half",
			caption:    "Brot\n\nZutaten:\n- 500 g Mehl\n- 300 ml Wasser\n- 1 Prise Salz\n- 7 g Hefe",
			wantAtMost: 0.5,
		},
		{
			name:       "instructions alone cannot exceed one half",
			caption:    "Brot\n\nZubereitung:\nAlles verkneten, gehen lassen und bei 230 Grad 40 Minuten backen, bis die Kruste dunkel ist.",
			wantAtMost: 0.5,
		},
		{
			name:          "both sections clear one half",
			caption:       sampleCaption,
			wantAboveHalf: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRules(t, tt.caption).Confidence
			switch {
			case tt.wantAboveHalf && got <= 0.5:
				t.Errorf("Confidence = %v, want > 0.5", got)
			case tt.wantAtMost > 0 && got > tt.wantAtMost:
				t.Errorf("Confidence = %v, want <= %v", got, tt.wantAtMost)
			case !tt.wantAboveHalf && tt.wantAtMost == 0 && got != tt.wantExactly:
				t.Errorf("Confidence = %v, want %v", got, tt.wantExactly)
			}
			if got < 0 || got > 1 {
				t.Errorf("Confidence = %v, want within [0,1]", got)
			}
		})
	}
}

// TestUntitledRecipeLowersConfidence covers E10's second half: a caption that
// offered no name is weak evidence of a recipe, and the row it produces is the
// one a human most needs to see.
func TestUntitledRecipeLowersConfidence(t *testing.T) {
	body := "\n\nZutaten:\n- 500 g Mehl\n- 300 ml Wasser\n- 1 Prise Salz\n- 7 g Hefe\n\n" +
		"Zubereitung:\nAlles verkneten, gehen lassen und bei 230 Grad 40 Minuten backen, bis die Kruste dunkel ist."

	named := extractRules(t, "Bauernbrot"+body)
	unnamed := extractRules(t, "😍"+body)

	if unnamed.Name != untitledRecipe {
		t.Fatalf("Name = %q, want %q", unnamed.Name, untitledRecipe)
	}
	if unnamed.Confidence >= named.Confidence {
		t.Errorf("untitled scored %v, want less than the named %v", unnamed.Confidence, named.Confidence)
	}
	if want := named.Confidence * untitledPenalty; math.Abs(unnamed.Confidence-want) > 1e-9 {
		t.Errorf("untitled Confidence = %v, want %v", unnamed.Confidence, want)
	}
}

// TestProseUnderIngredientsHeaderIsNotAnIngredient covers the parse guard the
// coverage term depends on: a sentence with neither amount nor unit is not an
// ingredient, so a section of prose parses badly rather than perfectly.
func TestProseUnderIngredientsHeaderIsNotAnIngredient(t *testing.T) {
	got := extractRules(t, `Nudeln

Zutaten:
- 500 g Nudeln
das Rezept habe ich von meiner Oma und es gelingt wirklich immer
- Salz

Zubereitung:
Nudeln kochen.`)

	for _, ing := range got.Ingredients {
		if strings.HasPrefix(ing.Name, "das Rezept") {
			t.Errorf("a prose line was parsed as an ingredient: %+v", got.Ingredients)
		}
	}
	// "Salz" has no amount and no unit, but it is short, so it survives.
	if len(got.Ingredients) != 2 {
		t.Errorf("len(Ingredients) = %d, want 2: %+v", len(got.Ingredients), got.Ingredients)
	}
}
