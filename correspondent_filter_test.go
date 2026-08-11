package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterCorrespondentsForPrompt_Disabled(t *testing.T) {
	names := []string{"Amazon", "REWE", "Telekom"}
	assert.Equal(t, names, filterCorrespondentsForPrompt(names, "some content", "title", 0))
	assert.Equal(t, names, filterCorrespondentsForPrompt(names, "some content", "title", -1))
}

func TestFilterCorrespondentsForPrompt_BelowLimitUnchanged(t *testing.T) {
	names := []string{"Amazon", "REWE"}
	assert.Equal(t, names, filterCorrespondentsForPrompt(names, "content", "title", 5))
}

func TestFilterCorrespondentsForPrompt_PrefersContentMatches(t *testing.T) {
	names := []string{"Allianz", "Amazon", "REWE Markt", "Stadtwerke Hof", "Telekom"}
	content := "Ihre Bestellung bei Amazon wurde versandt. Rechnungsbetrag 12,34 EUR."
	got := filterCorrespondentsForPrompt(names, content, "Amazon Rechnung", 2)
	assert.Len(t, got, 2)
	assert.Equal(t, "Amazon", got[0])
}

func TestFilterCorrespondentsForPrompt_MatchesInTitle(t *testing.T) {
	names := []string{"Allianz", "Stadtwerke Hof", "Telekom"}
	got := filterCorrespondentsForPrompt(names, "unrelated content", "Telekom Mobilfunkrechnung", 1)
	assert.Equal(t, []string{"Telekom"}, got)
}

func TestFilterCorrespondentsForPrompt_CaseInsensitiveAndUmlauts(t *testing.T) {
	names := []string{"Müller Drogerie", "Bäckerei Söllner", "Telekom"}
	content := "Einkauf bei MÜLLER Drogerie, danke für Ihren Besuch"
	got := filterCorrespondentsForPrompt(names, content, "", 1)
	assert.Equal(t, []string{"Müller Drogerie"}, got)
}

func TestFilterCorrespondentsForPrompt_MultiWordFractionWins(t *testing.T) {
	// "Stadtwerke Hof" has 2/2 words in content, "Sparkasse Hochfranken" only 1/2.
	names := []string{"Sparkasse Hochfranken", "Stadtwerke Hof", "Telekom"}
	content := "Die Stadtwerke Hof berechnen für die Sparkasse nichts."
	got := filterCorrespondentsForPrompt(names, content, "", 2)
	assert.Equal(t, "Stadtwerke Hof", got[0])
	assert.Equal(t, "Sparkasse Hochfranken", got[1])
}

func TestFilterCorrespondentsForPrompt_NoMatchesStableAlphabetical(t *testing.T) {
	names := []string{"Zeta", "Alpha", "Mitte"}
	got := filterCorrespondentsForPrompt(names, "completely unrelated text", "", 2)
	assert.Equal(t, []string{"Alpha", "Mitte"}, got)
}

func TestCorrespondentMatchScore_ShortNameFallback(t *testing.T) {
	// "dm" has no significant (3+ rune) words -> whole-name substring fallback.
	assert.Equal(t, 1.0, correspondentMatchScore("dm", "einkauf bei dm heute"))
	assert.Equal(t, 0.0, correspondentMatchScore("dm", "einkauf woanders"))
}
