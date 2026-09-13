package main

import (
	"sort"
	"strings"
	"unicode"
)

// filterCorrespondentsForPrompt reduces the correspondent names embedded into
// the correspondent prompt to at most limit entries, preferring names that
// occur in the document. Installations with hundreds of correspondents
// otherwise produce prompts that are slow to evaluate on local LLMs and can
// overflow small context windows. With limit <= 0 the input is returned
// unchanged.
func filterCorrespondentsForPrompt(names []string, content string, title string, limit int) []string {
	if limit <= 0 || len(names) <= limit {
		return names
	}

	haystack := strings.ToLower(title + "\n" + content)

	type scored struct {
		name  string
		score float64
	}
	ranked := make([]scored, 0, len(names))
	for _, name := range names {
		ranked = append(ranked, scored{name: name, score: correspondentMatchScore(name, haystack)})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return strings.ToLower(ranked[i].name) < strings.ToLower(ranked[j].name)
	})

	filtered := make([]string, 0, limit)
	for _, entry := range ranked[:limit] {
		filtered = append(filtered, entry.name)
	}
	return filtered
}

// correspondentMatchScore returns the fraction of significant words (3+ runes)
// of the correspondent name found in the haystack. Names without a significant
// word fall back to a whole-name substring match.
func correspondentMatchScore(name string, haystack string) float64 {
	words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	significant := 0
	matched := 0
	for _, word := range words {
		if len([]rune(word)) < 3 {
			continue
		}
		significant++
		if strings.Contains(haystack, word) {
			matched++
		}
	}
	if significant == 0 {
		if strings.Contains(haystack, strings.ToLower(name)) {
			return 1
		}
		return 0
	}
	return float64(matched) / float64(significant)
}
