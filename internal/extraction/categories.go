package extraction

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// ruleCategories is what the rules may propose: each seeded category (see
// internal/db/seed.go, which a test holds this list to) with the keywords that
// name it. Its order is the order categories are proposed in.
//
// A keyword ending in "*" matches any word it begins, which is what hashtags
// need: they run words together, as in #veganfood or #backenmitliebe. Any
// other keyword matches only itself and its German inflections ("vegane",
// "Smoothies"), for words whose compounds mean something else — a
// #smoothiebowl is eaten, not drunk, and a Cocktailsoße is a sauce.
//
// The list is short on purpose. A missing category is the state rules-only
// mode was already in; a wrong one files a recipe under the category filter
// where nobody looks for it, and may publish without review.
var ruleCategories = []struct {
	name     string
	keywords []string
}{
	{"Frühstück", []string{"frühstück*", "fruehstueck*", "breakfast*", "brunch*"}},
	{"Hauptgericht", []string{"hauptgericht*", "hauptspeise*", "mittagessen*", "abendessen*"}},
	{"Dessert", []string{"dessert*", "nachtisch*", "nachspeise*"}},
	{"Vorspeise", []string{"vorspeise*", "antipast*", "appetizer*"}},
	{"Snack", []string{"snack*", "fingerfood*"}},
	// Not "veggie": in English captions #veggies means vegetables, and a dish
	// with vegetables in it is not thereby vegetarian.
	{"Vegetarisch", []string{"vegetari*"}},
	{"Vegan", []string{"vegan*"}},
	{"Backen", []string{"backen*", "baking*", "kuchen*", "cake"}},
	{"Getränk", []string{"getränk*", "drink*", "smoothie", "cocktail", "limonade*"}},
}

// inflections are the endings a whole-word keyword may carry.
var inflections = []string{"", "e", "s", "n", "en", "er", "es", "em"}

// hashtagRe matches one hashtag and captures its text.
var hashtagRe = regexp.MustCompile(`#([\p{L}\p{N}_]+)`)

// categoriesFor proposes categories for a caption from what its author said
// about the dish, and from nothing else.
//
// That means the caption's head — the title and any lines before the first
// section header — and its hashtags wherever they are. The ingredient and
// instruction sections are not read: "40 Minuten backen" is how a gratin ends,
// not a sign it belongs under Backen, and "vegane Butter" in an ingredient list
// says nothing about the rest of it.
//
// Rules-only mode used to propose no categories at all, so with no API key the
// category filter in the UI was dead. These are still only proposals: the
// import checks them against the categories the database holds, so a list that
// drifts from the seed drops a name rather than inventing one.
func categoriesFor(lines []string) []string {
	words := categoryWords(lines)
	var out []string
	for _, c := range ruleCategories {
		if slices.ContainsFunc(c.keywords, func(keyword string) bool {
			return slices.ContainsFunc(words, func(word string) bool { return matchesKeyword(word, keyword) })
		}) {
			out = append(out, c.name)
		}
	}
	return out
}

// categoryWords lowercases the words categoriesFor reads: every word of the
// caption's head, and the text of every hashtag after it.
func categoryWords(lines []string) []string {
	var words []string
	inHead := true
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if ingredientsHeaderRe.MatchString(line) || instructionsHeaderRe.MatchString(line) {
			inHead = false
			continue
		}
		if inHead {
			words = append(words, strings.FieldsFunc(strings.ToLower(line), func(r rune) bool {
				return !unicode.IsLetter(r) && !unicode.IsDigit(r)
			})...)
			continue
		}
		for _, m := range hashtagRe.FindAllStringSubmatch(line, -1) {
			words = append(words, strings.ToLower(m[1]))
		}
	}
	return words
}

// matchesKeyword reports whether word is named by keyword; see ruleCategories
// for the two forms a keyword takes.
func matchesKeyword(word, keyword string) bool {
	if stem, ok := strings.CutSuffix(keyword, "*"); ok {
		return strings.HasPrefix(word, stem)
	}
	rest, ok := strings.CutPrefix(word, keyword)
	return ok && slices.Contains(inflections, rest)
}
