package extraction

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

var (
	ingredientsHeaderRe  = regexp.MustCompile(`(?i)^\s*(zutaten|ingredients)\s*[:\-]?\s*$`)
	instructionsHeaderRe = regexp.MustCompile(`(?i)^\s*(zubereitung|anleitung|schritte|instructions)\s*[:\-]?\s*$`)
	// ingredientLineRe splits an ingredient line into an optional amount, an
	// optional unit token, and the remaining name. The unit group only matches
	// when it is followed by whitespace, so a unit-less line like "1 Zwiebel"
	// keeps its name intact instead of being sliced mid-word.
	ingredientLineRe = regexp.MustCompile(`^\s*[-*•]?\s*(\d+(?:[.,]\d+)?)?\s*(?:([[:alpha:]äöüÄÖÜß.]+)\s+)?(.+)$`)
)

// RuleBasedExtractor is a fast, dependency-free Extractor that parses captions
// following the common German "Zutaten:/Zubereitung:" layout. It never returns
// an error; a caption it does not understand comes back with Confidence 0.
type RuleBasedExtractor struct{}

// NewRuleBasedExtractor returns a ready-to-use RuleBasedExtractor. It is safe
// for concurrent use.
func NewRuleBasedExtractor() *RuleBasedExtractor { return &RuleBasedExtractor{} }

// Extract splits caption into an ingredients section and an instructions section
// using the header regexes, parses each ingredient line into an
// ExtractedIngredient, and scores Confidence: +0.5 when the ingredient list is
// non-empty, +0.5 when the instructions are non-empty, capped at 1.0. A caption
// with neither section scores 0.
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
		Name:         firstNonEmptyLine(lines),
		Instructions: strings.Join(instructionLines, "\n"),
	}
	for _, line := range ingredientLines {
		if ing, ok := parseIngredientLine(line); ok {
			result.Ingredients = append(result.Ingredients, ing)
		}
	}
	result.Confidence = confidenceFor(result)
	return result, nil
}

// firstNonEmptyLine returns the first non-blank line, trimmed, or a placeholder
// title when every line is blank.
func firstNonEmptyLine(lines []string) string {
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return "Unbenanntes Rezept"
}

// parseIngredientLine turns a single "300 g Mehl" style line into an
// ExtractedIngredient. The bool is false when the line yields no usable name
// (blank, or a stray hashtag that slipped into the ingredients section).
func parseIngredientLine(line string) (ExtractedIngredient, bool) {
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
	return ExtractedIngredient{Name: name, Amount: amount, Unit: unit}, true
}

// confidenceFor implements the load-bearing score used by the hybrid extractor
// and the pipeline: 0.5 for a non-empty ingredient list plus 0.5 for non-empty
// instructions, so a full recipe scores 1.0 and a non-recipe caption scores 0.
func confidenceFor(r *ExtractedRecipe) float64 {
	score := 0.0
	if len(r.Ingredients) > 0 {
		score += 0.5
	}
	if strings.TrimSpace(r.Instructions) != "" {
		score += 0.5
	}
	return score
}
