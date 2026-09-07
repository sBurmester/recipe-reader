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
