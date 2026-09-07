package extraction

import "strings"

// unitAliases maps a lowercased, trailing-dot-stripped unit token to its
// canonical form. The canonical values line up with db.defaultUnits so the
// pipeline's FindOrCreateUnit calls hit existing rows.
var unitAliases = map[string]string{
	"g": "g", "gramm": "g", "gr": "g",
	"kg": "kg", "kilogramm": "kg",
	"ml": "ml", "milliliter": "ml",
	"l": "l", "liter": "l",
	"el": "EL", "esslöffel": "EL", "essloeffel": "EL",
	"tl": "TL", "teelöffel": "TL", "teeloeffel": "TL",
	"stück": "Stück", "stk": "Stück", "stueck": "Stück",
	"prise":   "Prise",
	"bund":    "Bund",
	"dose":    "Dose",
	"packung": "Packung", "pck": "Packung", "pkg": "Packung",
}

func normalizeKey(candidate string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(candidate), "."))
}

// IsKnownUnit reports whether candidate (case-insensitive, optional trailing
// dot) is a recognized unit token.
func IsKnownUnit(candidate string) bool {
	_, ok := unitAliases[normalizeKey(candidate)]
	return ok
}

// NormalizeUnit returns the canonical form of candidate, or "" if it is not a
// known unit.
func NormalizeUnit(candidate string) string {
	return unitAliases[normalizeKey(candidate)]
}
