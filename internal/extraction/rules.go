package extraction

import (
	"context"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ingredientsHeaderRe  = regexp.MustCompile(`(?i)^\s*(zutaten|ingredients)\s*[:\-]?\s*$`)
	instructionsHeaderRe = regexp.MustCompile(`(?i)^\s*(zubereitung|anleitung|schritte|instructions)\s*[:\-]?\s*$`)
	// ingredientLineRe splits an ingredient line into an optional amount, an
	// optional unit token, and the remaining name. The unit group only matches
	// when it is followed by whitespace, so a unit-less line like "1 Zwiebel"
	// keeps its name intact instead of being sliced mid-word.
	ingredientLineRe = regexp.MustCompile(`^\s*[-*•]?\s*(\d+(?:[.,]\d+)?)?\s*(?:([[:alpha:]äöüÄÖÜß.]+)\s+)?(.+)$`)
	// hashtagTailRe matches the trailing hashtag block captions end with. It is
	// anchored at the end so an inline "#1 Lieblingsrezept" survives.
	hashtagTailRe = regexp.MustCompile(`(?:\s*#[^\s#]+)+\s*$`)
)

// untitledRecipe is the last-resort title for a caption with no usable line.
// It is also a confidence signal: see confidenceFor.
const untitledRecipe = "Unbenanntes Rezept"

// RuleBasedExtractor is a fast, dependency-free Extractor that parses captions
// following the common German "Zutaten:/Zubereitung:" layout. It never returns
// an error; a caption it does not understand comes back with Confidence 0.
// Categories come from keywords in the caption's title and hashtags (see
// categoriesFor).
type RuleBasedExtractor struct{}

// NewRuleBasedExtractor returns a ready-to-use RuleBasedExtractor. It is safe
// for concurrent use.
func NewRuleBasedExtractor() *RuleBasedExtractor { return &RuleBasedExtractor{} }

// Extract splits caption into an ingredients section and an instructions
// section using the header regexes, parses each ingredient line into an
// ExtractedIngredient, picks a title (see recipeTitle), and scores Confidence
// continuously from what it found (see confidenceFor). A caption with neither
// section scores 0.
func (e *RuleBasedExtractor) Extract(_ context.Context, caption string) (*ExtractedRecipe, error) {
	lines := strings.Split(caption, "\n")

	var ingredientLines, instructionLines []string
	section := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case ingredientsHeaderRe.MatchString(line):
			section = "ingredients"
			continue
		case instructionsHeaderRe.MatchString(line):
			section = "instructions"
			continue
		}
		if line == "" {
			continue
		}
		switch section {
		case "ingredients":
			ingredientLines = append(ingredientLines, line)
		case "instructions":
			instructionLines = append(instructionLines, line)
		}
	}

	result := &ExtractedRecipe{
		Name:         recipeTitle(lines),
		Instructions: strings.Join(instructionLines, "\n"),
	}
	for _, line := range ingredientLines {
		if ing, ok := parseIngredientLine(line); ok {
			result.Ingredients = append(result.Ingredients, ing)
		}
	}
	// len(ingredientLines) is the denominator of the coverage term: how many
	// lines the ingredients section offered, against how many parsed.
	result.Confidence = confidenceFor(result, len(ingredientLines))
	// Only for something the pipeline will store: a caption that scores 0 is
	// "no recipe here", and categories on it would describe nothing.
	if result.Confidence > 0 {
		result.Categories = categoriesFor(lines)
	}
	return result, nil
}

// recipeTitle picks the caption's most title-like line.
//
// The first non-blank line used to be the title outright, and for the caption
// shape these rules target that line is typically a hook, an emoji run or a
// hashtag block. The name is not only displayed: it is the column the search
// filter runs against, so a bad title degrades search too.
//
// The scan stops at the first section header — everything past "Zutaten:" is
// content, not a name — and prefers a line that looks like a title: short,
// carrying a letter, not a hashtag block, not itself a header ending in ":".
// Failing that it falls back to the first cleaned line, which is the old
// behaviour minus the emoji and hashtag noise.
func recipeTitle(lines []string) string {
	fallback := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if ingredientsHeaderRe.MatchString(line) || instructionsHeaderRe.MatchString(line) {
			break
		}
		candidate := cleanTitle(line)
		if candidate == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if fallback == "" {
			fallback = candidate
		}
		if isTitleLike(line, candidate) {
			return candidate
		}
	}
	if fallback != "" {
		return fallback
	}
	return untitledRecipe
}

// maxTitleRunes is where a line stops reading as a name and starts reading as
// a sentence; minTitleRunes drops stray fragments like "2x" or "ad".
const (
	maxTitleRunes = 60
	minTitleRunes = 3
)

// isTitleLike reports whether candidate — the cleaned form of raw — can stand
// as a recipe name. raw is consulted for the two markers cleaning removes: a
// leading "#" (a hashtag block) and a trailing ":" (a section header).
func isTitleLike(raw, candidate string) bool {
	if strings.HasPrefix(raw, "#") || strings.HasSuffix(raw, ":") {
		return false
	}
	n := utf8.RuneCountInString(candidate)
	if n < minTitleRunes || n > maxTitleRunes {
		return false
	}
	return strings.ContainsFunc(candidate, unicode.IsLetter)
}

// cleanTitle strips a line down to the name inside it: the trailing hashtag
// block first, then the leading and trailing decoration — emoji runs, bullets,
// separators and the ":" or "!" a caption line ends on. Closing brackets and
// quotes survive, so "Brot (vegan)" is not left unbalanced.
func cleanTitle(line string) string {
	s := hashtagTailRe.ReplaceAllString(strings.TrimSpace(line), "")
	s = strings.TrimLeftFunc(s, func(r rune) bool { return isTitleNoise(r, "([{\"'„«") })
	s = strings.TrimRightFunc(s, func(r rune) bool { return isTitleNoise(r, ")]}\"'“»") })
	return strings.TrimSpace(s)
}

// isTitleNoise reports whether r is decoration rather than part of a name.
// Letters, digits and the runes in keep are never noise.
func isTitleNoise(r rune, keep string) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return false
	}
	return !strings.ContainsRune(keep, r)
}

// maxUnitlessIngredientWords bounds a line that carries neither an amount nor
// a known unit. Real ingredient lines of that shape are short — "Salz",
// "Frisch gemahlener schwarzer Pfeffer nach Geschmack" is already the long end
// — while a sentence of prose that drifted under the "Zutaten:" header is not.
// Dropping those is what makes the coverage term in confidenceFor mean
// something: a section of prose now parses badly instead of parsing perfectly.
const maxUnitlessIngredientWords = 6

// parseIngredientLine turns a single "300 g Mehl" style line into an
// ExtractedIngredient. The bool is false when the line yields no usable name
// (blank, a stray hashtag that slipped into the ingredients section, or a
// sentence carrying neither amount nor unit).
func parseIngredientLine(line string) (ExtractedIngredient, bool) {
	// Captions commonly lead each ingredient with an emoji rather than a
	// bullet ("🥥 400 ml Kokosmilch"). The regex below only knows -, * and •,
	// so every such line used to fall through to the name group whole: the
	// amount and unit were lost and the emoji ended up in the ingredient name,
	// which then became a row in the shared ingredients table. "#" is kept
	// because a leading hashtag is how the guard below recognises a tag that
	// drifted into the section.
	line = strings.TrimLeftFunc(line, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsNumber(r) && r != '#'
	})
	m := ingredientLineRe.FindStringSubmatch(line)
	if m == nil {
		return ExtractedIngredient{}, false
	}
	amountStr, unitCandidate, rest := m[1], strings.TrimSpace(m[2]), strings.TrimSpace(m[3])

	var amount float64
	if amountStr != "" {
		amount, _ = strconv.ParseFloat(strings.ReplaceAll(amountStr, ",", "."), 64)
	}

	unit, name := "", rest
	switch {
	case unitCandidate != "" && IsKnownUnit(unitCandidate):
		unit = NormalizeUnit(unitCandidate)
	case unitCandidate != "":
		name = strings.TrimSpace(unitCandidate + " " + rest)
	}
	name = strings.TrimSpace(strings.TrimSuffix(name, "."))
	if name == "" || strings.HasPrefix(name, "#") {
		return ExtractedIngredient{}, false
	}
	if amount == 0 && unit == "" && len(strings.Fields(name)) > maxUnitlessIngredientWords {
		return ExtractedIngredient{}, false
	}
	return ExtractedIngredient{Name: name, Amount: amount, Unit: unit}, true
}

// The shape of a complete rules extraction, used to scale the score below.
const (
	// fullIngredientCount is where a list stops looking like a fragment.
	fullIngredientCount = 4
	// fullInstructionRunes is where instructions stop looking like one
	// stray sentence. It matches the LLM path's structural threshold.
	fullInstructionRunes = 80
	// untitledPenalty multiplies the score of a recipe whose caption offered
	// no usable name. A nameless caption is weak evidence of a recipe, and the
	// row it produces is the one a human most needs to look at.
	untitledPenalty = 0.75
)

// confidenceFor scores a rules extraction continuously in 0..1. It is the
// load-bearing number in this package: the hybrid extractor compares it
// against EXTRACTION_CONFIDENCE_THRESHOLD to decide whether to pay for an LLM
// call, and the pipeline compares it against EXTRACTION_PUBLISH_THRESHOLD to
// decide whether a recipe publishes or waits for review.
//
// It used to return only 0, 0.5 or 1.0, which collapsed both float flags to
// three behaviours: every threshold in (0.5, 1.0] meant the same thing, and an
// operator tuning one from 0.6 to 0.9 saw no change at all until they crossed
// an invisible cliff. The terms below are continuous, so the flags now move
// behaviour where they are set.
//
// The two halves stay worth 0.5 each, which preserves the boundaries callers
// were tuned against: no ingredients and no instructions is still exactly 0
// (the pipeline reads that as "no recipe here"), one section alone still
// cannot exceed 0.5, and a clean full caption still reaches 1.0.
func confidenceFor(r *ExtractedRecipe, ingredientLines int) float64 {
	score := 0.5*ingredientScore(r.Ingredients, ingredientLines) + 0.5*instructionScore(r.Instructions)
	if r.Name == untitledRecipe {
		score *= untitledPenalty
	}
	return clamp01(score)
}

// ingredientScore rates the ingredient list on three signals a real list
// passes and a half-read or prose one usually does not: that enough lines were
// found, that the section's lines mostly parsed (coverage), and that the
// amounts were actually read off the caption rather than left blank.
//
// candidateLines is how many lines the ingredients section offered — the
// denominator of coverage. A section of five lines yielding two ingredients is
// a worse parse than a section of two yielding two, and the old score could
// not tell them apart.
func ingredientScore(ingredients []ExtractedIngredient, candidateLines int) float64 {
	if len(ingredients) == 0 {
		return 0
	}
	count := math.Min(1, float64(len(ingredients))/fullIngredientCount)

	coverage := 1.0
	if candidateLines > 0 {
		coverage = math.Min(1, float64(len(ingredients))/float64(candidateLines))
	}

	withAmounts := 0
	for _, ing := range ingredients {
		if ing.Amount > 0 {
			withAmounts++
		}
	}
	amounts := float64(withAmounts) / float64(len(ingredients))

	return clamp01(0.6*count + 0.2*coverage + 0.2*amounts)
}

// instructionScore rates the instructions by length, which is the only signal
// available without understanding them. A single short sentence is rarely a
// whole method; fullInstructionRunes up is scored as complete.
func instructionScore(instructions string) float64 {
	n := utf8.RuneCountInString(strings.TrimSpace(instructions))
	if n == 0 {
		return 0
	}
	return math.Min(1, float64(n)/fullInstructionRunes)
}
