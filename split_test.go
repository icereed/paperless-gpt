package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"text/template"

	"paperless-gpt/internal/pdfrender"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseSplitBoundaries checks both response formats and rejects malformed, unordered, duplicate, or out-of-window boundaries.
func TestParseSplitBoundaries(t *testing.T) {
	for _, input := range []string{
		`not json`, `null`, `{}`, `[{}]`,
		`[{"after_page":0,"confidence":0.9,"reason":"x"}]`,
		`[{"after_page":8,"confidence":0.9,"reason":"x"}]`,
		`[{"after_page":2.5,"confidence":0.9,"reason":"x"}]`,
		`[{"after_page":2,"confidence":1.1,"reason":"x"}]`,
		`[{"after_page":2,"confidence":null,"reason":"x"}]`,
		`[{"after_page":2,"confidence":0.9,"reason":""}]`,
		`[{"after_page":2,"confidence":0.9,"reason":"x","extra":true}]`,
		`[{"after_page":2,"confidence":0.9,"reason":"x"},{"after_page":2,"confidence":0.9,"reason":"y"}]`,
		`[{"after_page":4,"confidence":0.9,"reason":"x"},{"after_page":2,"confidence":0.9,"reason":"y"}]`,
		`[] []`,
	} {
		t.Run(input, func(t *testing.T) {
			_, err := parseSplitBoundaries(input, 1, 8)
			require.Error(t, err)
		})
	}
	boundaries, err := parseSplitBoundaries(`[{"after_page":7,"confidence":0.95,"reason":"New sender"}]`, 7, 14)
	require.NoError(t, err)
	assert.Equal(t, 7, boundaries[0].AfterPage)
	_, err = parseSplitBoundaries(`[{"after_page":6,"confidence":0.95,"reason":"x"}]`, 7, 14)
	require.Error(t, err)
	boundaries, err = parseSplitBoundaries(`[]`, 1, 8)
	require.NoError(t, err)
	assert.Empty(t, boundaries)
	boundaries, err = parseSplitBoundaries(`[{"page":8,"split":true,"confidence":1,"reason":"New document"}]`, 7, 8)
	require.NoError(t, err)
	require.Len(t, boundaries, 1)
	assert.Equal(t, 7, boundaries[0].AfterPage)
	for _, input := range []string{
		`[{"page":7,"split":false,"confidence":1,"reason":"x"}]`,
		`[{"page":9,"split":false,"confidence":1,"reason":"x"}]`,
		`[{"page":null,"split":true,"confidence":1,"reason":"x"}]`,
		`[{"after_page":7,"split":true,"confidence":1,"reason":"x"}]`,
		`[{"page":8,"after_page":7,"split":true,"confidence":1,"reason":"x"}]`,
		`[{"page":8,"confidence":1,"reason":"x"}]`,
	} {
		_, err := parseSplitBoundaries(input, 7, 8)
		require.Error(t, err, input)
	}
}

// TestSplitWindowsAndMerge checks window overlap, disagreement detection, and minimum confidence for shared boundaries.
func TestSplitWindowsAndMerge(t *testing.T) {
	assert.Equal(t, []splitWindow{{1, 8}, {7, 14}, {13, 20}, {19, 21}}, splitWindows(21, 8))
	assert.Equal(t, []splitWindow{{1, 8}}, splitWindows(8, 8))
	assert.Empty(t, splitWindows(1, 8))
	// Pages 7-12 remain together, including across the first window's edge.
	merged := mergeSplitWindows([]splitWindowResult{
		{splitWindow{1, 8}, []SplitBoundary{{AfterPage: 6, Confidence: .99, Reason: "New letter"}}},
		{splitWindow{7, 14}, []SplitBoundary{{AfterPage: 12, Confidence: .98, Reason: "New invoice"}}},
	})
	require.Len(t, merged, 2)
	assert.Equal(t, 6, merged[0].AfterPage)
	assert.Equal(t, 12, merged[1].AfterPage)
	assert.False(t, merged[0].Conflict)
	merged = mergeSplitWindows([]splitWindowResult{
		{splitWindow{1, 8}, []SplitBoundary{{AfterPage: 7, Confidence: .99, Reason: "x"}}},
		{splitWindow{7, 14}, nil},
	})
	require.Len(t, merged, 1)
	assert.True(t, merged[0].Conflict, "omitting a boundary in overlapping context is disagreement")
	merged = mergeSplitWindows([]splitWindowResult{
		{splitWindow{1, 8}, []SplitBoundary{{AfterPage: 7, Confidence: .99, Reason: "x"}}},
		{splitWindow{7, 14}, []SplitBoundary{{AfterPage: 7, Confidence: .91, Reason: "y"}}},
	})
	assert.InDelta(t, .91, merged[0].Confidence, .0001)
	assert.False(t, merged[0].Conflict)
}

// TestParseSplitDecisions checks that complete boolean page decisions produce only selected boundaries and reject mixed formats.
func TestParseSplitDecisions(t *testing.T) {
	boundaries, err := parseSplitBoundaries(`[
		{"page":8,"split":false,"confidence":1,"reason":"Same invoice continues"},
		{"page":9,"split":true,"confidence":0.95,"reason":"New invoice starts on page 9"}
	]`, 7, 9)
	require.NoError(t, err)
	require.Len(t, boundaries, 1)
	assert.Equal(t, 8, boundaries[0].AfterPage)
	assert.Equal(t, .95, boundaries[0].Confidence)
	boundaries, err = parseSplitBoundaries(`[{"page":8,"split":false,"confidence":1,"reason":"Continuation"}]`, 7, 8)
	require.NoError(t, err)
	assert.Empty(t, boundaries)
	for _, decision := range []string{"null", `"false"`, "0", "{}"} {
		_, err := parseSplitBoundaries(`[{"page":8,"split":`+decision+`,"confidence":1,"reason":"x"}]`, 7, 8)
		require.ErrorContains(t, err, "split must be true or false")
	}
	for _, input := range []string{
		`[{"page":8,"split":null,"confidence":1,"reason":"x"}]`,
		`[{"page":8,"split":"false","confidence":1,"reason":"x"}]`,
		`[{"page":8,"split":false,"confidence":1,"reason":"x"}]`,
		`[{"page":8,"split":false,"confidence":1,"reason":"x"},{"after_page":8,"confidence":1,"reason":"x"}]`,
		`[{"after_page":7,"confidence":1,"reason":"x"},{"page":9,"split":true,"confidence":1,"reason":"x"}]`,
		`[{"page":8,"split":false,"confidence":1,"reason":"x"},{"page":8,"split":true,"confidence":1,"reason":"x"}]`,
	} {
		_, err := parseSplitBoundaries(input, 7, 9)
		require.Error(t, err, input)
	}
}

// TestSplitPagesString checks page-range formatting and rejects missing, unordered, duplicate, or out-of-range boundaries.
func TestSplitPagesString(t *testing.T) {
	pages, err := splitPagesString(9, []int{3, 4})
	require.NoError(t, err)
	assert.Equal(t, "1-3,4,5-9", pages)
	for _, boundaries := range [][]int{nil, {0}, {9}, {4, 3}, {3, 3}} {
		_, err := splitPagesString(9, boundaries)
		require.Error(t, err)
	}
}

// TestSplitSignalsAndRules checks page signals and pagination guards, including footers beyond the text excerpt.
func TestSplitSignalsAndRules(t *testing.T) {
	p := splitPageSignals(7, 595, 842, pdfrender.PageAnalysis{Text: "Firma Beispiel\nBerlin\n01.10.2026\nSeite 1 von 3", ImageCoverage: 1})
	assert.Equal(t, "portrait", p.Orientation)
	assert.Equal(t, 1, p.PrintedPage)
	assert.Equal(t, 3, p.PrintedTotal)
	assert.Contains(t, p.Dates, "01.10.2026")
	assert.Contains(t, p.SenderBlock, "Firma Beispiel")
	assert.False(t, p.Blank)
	assert.False(t, splitPageSignals(1, 100, 100, pdfrender.PageAnalysis{ImageCoverage: 1}).Blank)
	assert.True(t, splitPageSignals(1, 100, 100, pdfrender.PageAnalysis{}).Blank)
	pages := make([]SplitPage, 12)
	pages[6] = p
	boundaries := []SplitBoundary{{AfterPage: 6}, {AfterPage: 7}, {AfterPage: 8}, {AfterPage: 9}}
	applySplitRules(pages, boundaries)
	assert.Empty(t, boundaries[0].BlockedReason)
	assert.NotEmpty(t, boundaries[1].BlockedReason)
	assert.NotEmpty(t, boundaries[2].BlockedReason)
	assert.Empty(t, boundaries[3].BlockedReason)
	// Pagination is extracted from the whole page, including footers beyond line 40.
	p = splitPageSignals(1, 842, 595, pdfrender.PageAnalysis{Text: strings.Repeat("body\n", 60) + "Page 2 of 3"})
	assert.Equal(t, 2, p.PrintedPage)
	assert.Equal(t, "landscape", p.Orientation)
	assert.LessOrEqual(t, len(strings.Split(p.Text, "\n")), 40)
}

// TestSplitPrompt checks that the default prompt includes page content, decision bounds, and German document guidance.
func TestSplitPrompt(t *testing.T) {
	content, err := os.ReadFile("default_prompts/split_prompt.tmpl")
	require.NoError(t, err)
	tmpl, err := template.New("split").Parse(string(content))
	require.NoError(t, err)
	prompt, err := renderSplitPrompt(tmpl, []SplitPage{{Page: 7, Text: "Brief"}, {Page: 8, Text: "Rechnung"}}, 20)
	require.NoError(t, err)
	assert.Contains(t, prompt, `"page":7`)
	assert.Contains(t, prompt, `"split": true if this page starts a separate document`)
	assert.Contains(t, prompt, "from 8 through 8")
	assert.Contains(t, prompt, "Brief")
	assert.Contains(t, prompt, "German")
}

// TestSplitSparseOCRBlocksAutomaticApply checks that image pages with fewer than ten text characters block adjacent boundaries and automatic application.
func TestSplitSparseOCRBlocksAutomaticApply(t *testing.T) {
	for _, length := range []int{0, 9, 10} {
		pages := []SplitPage{{Page: 1, TextLength: 100}, {Page: 2, TextLength: length, ImageCoverage: 1}, {Page: 3, TextLength: 100}, {Page: 4, TextLength: 100}}
		boundaries := []SplitBoundary{{AfterPage: 1, Confidence: 1}, {AfterPage: 2, Confidence: 1}, {AfterPage: 3, Confidence: 1}}
		applySplitRules(pages, boundaries)
		cfg := SplitConfig{Auto: true, ConfidenceThreshold: .9}
		suggestion := SplitSuggestion{Status: "review", PageCount: 4, Boundaries: boundaries}
		if length < 10 {
			assert.Contains(t, boundaries[0].BlockedReason, "insufficient OCR text")
			assert.Contains(t, boundaries[1].BlockedReason, "insufficient OCR text")
			assert.Nil(t, automaticSplitBoundaries(&cfg, &suggestion))
		} else {
			assert.Equal(t, []int{1, 2, 3}, automaticSplitBoundaries(&cfg, &suggestion))
		}
		assert.Empty(t, boundaries[2].BlockedReason)
	}
}

// TestSplitConfigValidation checks defaults, automatic mode, and rejection of invalid environment settings.
func TestSplitConfigValidation(t *testing.T) {
	for _, key := range []string{"SPLIT_TAG", "SPLIT_DRY_RUN", "SPLIT_CONFIDENCE_THRESHOLD", "SPLIT_AUTO", "SPLIT_DELETE_ORIGINALS", "SPLIT_WINDOW_SIZE", "SPLIT_OUTPUT_TAGS"} {
		t.Setenv(key, "")
	}
	cfg, err := readSplitConfig()
	require.NoError(t, err)
	assert.True(t, cfg.DryRun)
	assert.False(t, cfg.DeleteOriginals)
	assert.Equal(t, 8, cfg.WindowSize)
	t.Setenv("SPLIT_AUTO", "true")
	cfg, err = readSplitConfig()
	require.NoError(t, err)
	assert.True(t, cfg.Auto)
	for key, value := range map[string]string{"SPLIT_WINDOW_SIZE": "2", "SPLIT_CONFIDENCE_THRESHOLD": "NaN", "SPLIT_DRY_RUN": "maybe", "SPLIT_OUTPUT_TAGS": "paperless-gpt-split"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			_, err := readSplitConfig()
			require.Error(t, err)
		})
	}
}

// TestAutomaticSplitBoundaries checks that automatic application requires every boundary and the suggestion configuration to qualify.
func TestAutomaticSplitBoundaries(t *testing.T) {
	for _, kind := range []string{"eligible", "threshold", "low-confidence", "blocked", "conflict", "empty", "dry-run", "disabled", "skipped", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			cfg := SplitConfig{Auto: true, ConfidenceThreshold: .9}
			s := SplitSuggestion{Status: "review", PageCount: 6, Boundaries: []SplitBoundary{{AfterPage: 2, Confidence: .99}, {AfterPage: 4, Confidence: .9}}}
			switch kind {
			case "threshold":
				cfg.ConfidenceThreshold = .99
				s.Boundaries[1].Confidence = .99
			case "low-confidence":
				s.Boundaries[1].Confidence = .89
			case "blocked":
				s.Boundaries[1].BlockedReason = "pagination"
			case "conflict":
				s.Boundaries[1].Conflict = true
			case "empty":
				s.Boundaries = nil
			case "dry-run":
				cfg.DryRun = true
			case "disabled":
				cfg.Auto = false
			case "skipped":
				s.Status = "skipped"
			case "invalid":
				s.Boundaries[1].AfterPage = 6
			}
			got := automaticSplitBoundaries(&cfg, &s)
			if kind == "eligible" || kind == "threshold" {
				assert.Equal(t, []int{2, 4}, got)
			} else {
				assert.Nil(t, got, "never submit a partial set of boundaries")
			}
		})
	}
}

// TestSplitCanceledExtraction checks that extraction returns an error for a canceled context and invalid PDF data.
func TestSplitCanceledExtraction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := extractSplitPages(ctx, []byte("invalid"))
	require.Error(t, err)
}
