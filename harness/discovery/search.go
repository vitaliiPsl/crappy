package discovery

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type entry struct {
	match Match
	terms map[string]int
}

func newEntry(definition kit.ToolDefinition) entry {
	groups := []struct {
		terms  []string
		weight int
	}{
		{terms: tokenize(definition.Name), weight: 4},
		{terms: tokenize(definition.Description), weight: 2},
		{terms: schemaTerms(definition.Schema), weight: 1},
	}

	weights := make(map[string]int)
	for _, group := range groups {
		for _, term := range group.terms {
			weights[term] = max(weights[term], group.weight)
		}
	}

	return entry{
		match: Match{Name: definition.Name, Description: summarizeDescription(definition.Description)},
		terms: weights,
	}
}

func (e entry) score(terms []string) int {
	score := 0
	for _, term := range terms {
		score += e.terms[term]
	}

	return score
}

func schemaTerms(schema map[string]any) []string {
	var terms []string
	if description, ok := schema["description"].(string); ok {
		terms = append(terms, tokenize(description)...)
	}

	if properties, ok := schema["properties"].(map[string]any); ok {
		for name, property := range properties {
			terms = append(terms, tokenize(name)...)

			if child, ok := property.(map[string]any); ok {
				terms = append(terms, schemaTerms(child)...)
			}
		}
	}

	if items, ok := schema["items"].(map[string]any); ok {
		terms = append(terms, schemaTerms(items)...)
	}

	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if variants, ok := schema[key].([]any); ok {
			for _, variant := range variants {
				if child, ok := variant.(map[string]any); ok {
					terms = append(terms, schemaTerms(child)...)
				}
			}
		}
	}

	return terms
}

func tokenize(text string) []string {
	var (
		separated strings.Builder
		previous  rune
	)

	for _, current := range text {
		if unicode.IsLower(previous) && unicode.IsUpper(current) {
			separated.WriteByte(' ')
		}

		separated.WriteRune(unicode.ToLower(current))
		previous = current
	}

	terms := strings.FieldsFunc(separated.String(), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	})
	for index, term := range terms {
		terms[index] = normalizeTerm(term)
	}

	return terms
}

func normalizeTerm(term string) string {
	switch {
	case len(term) > 4 && strings.HasSuffix(term, "ies"):
		return strings.TrimSuffix(term, "ies") + "y"
	case len(term) > 3 && strings.HasSuffix(term, "s") && !strings.HasSuffix(term, "ss"):
		return strings.TrimSuffix(term, "s")
	default:
		return term
	}
}

func summarizeDescription(description string) string {
	const maxRunes = 200

	line, _, _ := strings.Cut(strings.TrimSpace(description), "\n")

	characters := []rune(line)
	if len(characters) > maxRunes {
		return string(characters[:maxRunes]) + "…"
	}

	return line
}

func rank(entries []entry, terms []string, limit int) []string {
	type candidate struct {
		name  string
		score int
	}

	var matches []candidate
	for _, item := range entries {
		if score := item.score(terms); score > 0 {
			matches = append(matches, candidate{name: item.match.Name, score: score})
		}
	}

	slices.SortStableFunc(matches, func(left, right candidate) int {
		return cmp.Compare(right.score, left.score)
	})
	matches = matches[:min(limit, len(matches))]

	names := make([]string, len(matches))
	for index, match := range matches {
		names[index] = match.name
	}

	return names
}
