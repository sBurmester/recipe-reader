package extraction

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapCaption(t *testing.T) {
	short := "Kürbis 🎃 Risotto"
	if got := capCaption(short); got != short {
		t.Errorf("capCaption(short) = %q, want it unchanged", got)
	}

	exact := strings.Repeat("ü", maxCaptionRunes)
	if got := capCaption(exact); got != exact {
		t.Error("a caption of exactly maxCaptionRunes runes was cut")
	}

	// Four-byte runes: a byte-based cut at any multiple of the cap would land
	// on a character boundary only by accident.
	got := capCaption(strings.Repeat("🎃", maxCaptionRunes+500))
	if n := utf8.RuneCountInString(got); n != maxCaptionRunes {
		t.Errorf("rune count = %d, want %d", n, maxCaptionRunes)
	}
	if !utf8.ValidString(got) {
		t.Error("truncated caption is not valid UTF-8")
	}
}

// capturingClient records the caption a transport would have been sent.
type capturingClient struct{ caption string }

func (c *capturingClient) recordRecipe(_ context.Context, caption string) ([]byte, error) {
	c.caption = caption
	return toolInput(0.9), nil
}

// The cap has to sit on the path every transport takes, not beside it.
func TestLLMExtractor_ExtractCapsTheCaption(t *testing.T) {
	client := &capturingClient{}
	e := &LLMExtractor{client: client}

	if _, err := e.Extract(context.Background(), strings.Repeat("ö", 2*maxCaptionRunes)); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if n := utf8.RuneCountInString(client.caption); n != maxCaptionRunes {
		t.Errorf("transport received %d runes, want %d", n, maxCaptionRunes)
	}
}
