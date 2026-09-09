// internal/instagram/saved_test.go
package instagram

import "testing"

func TestExtractMedia(t *testing.T) {
	media := map[string]any{
		"code": "Cxyz123",
		"caption": map[string]any{
			"text": "Zutaten: 200g Mehl\nZubereitung: Backen.",
		},
		"image_versions2": map[string]any{
			"candidates": []any{
				map[string]any{"url": "https://example.com/photo.jpg"},
			},
		},
	}

	post, ok := extractMedia(media)
	if !ok {
		t.Fatal("extractMedia() ok = false, want true")
	}
	if post.Source != "https://www.instagram.com/p/Cxyz123/" {
		t.Errorf("Source = %q", post.Source)
	}
	if post.Caption == "" {
		t.Error("expected non-empty caption")
	}
	if post.ImageURL != "https://example.com/photo.jpg" {
		t.Errorf("ImageURL = %q", post.ImageURL)
	}
}

func TestExtractMedia_MissingCode(t *testing.T) {
	if _, ok := extractMedia(map[string]any{}); ok {
		t.Error("expected ok = false when code is missing")
	}
}
