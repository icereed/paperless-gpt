package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"paperless-gpt/internal/pdfrender"
)

type SplitConfig struct {
	Tag                 string   `json:"tag"`
	DryRun              bool     `json:"dry_run"`
	ConfidenceThreshold float64  `json:"confidence_threshold"`
	Auto                bool     `json:"auto"`
	DeleteOriginals     bool     `json:"delete_originals"`
	WindowSize          int      `json:"window_size"`
	OutputTags          []string `json:"output_tags"`
}

// readSplitConfig reads split settings from the environment, applying defaults
// and rejecting invalid values or collisions with other processing tags.
func readSplitConfig() (*SplitConfig, error) {
	value := func(key, fallback string) string {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	cfg := &SplitConfig{Tag: value("SPLIT_TAG", "paperless-gpt-split")}
	var err error
	for key, dest := range map[string]*bool{"SPLIT_DRY_RUN": &cfg.DryRun, "SPLIT_AUTO": &cfg.Auto, "SPLIT_DELETE_ORIGINALS": &cfg.DeleteOriginals} {
		*dest, err = strconv.ParseBool(value(key, strconv.FormatBool(key == "SPLIT_DRY_RUN")))
		if err != nil {
			return nil, fmt.Errorf("invalid %s: must be a boolean", key)
		}
	}
	cfg.WindowSize, err = strconv.Atoi(value("SPLIT_WINDOW_SIZE", "8"))
	if err != nil || cfg.WindowSize < 3 {
		return nil, fmt.Errorf("SPLIT_WINDOW_SIZE must be at least 3 (two pages overlap)")
	}
	cfg.ConfidenceThreshold, err = strconv.ParseFloat(value("SPLIT_CONFIDENCE_THRESHOLD", "0.9"), 64)
	if err != nil || math.IsNaN(cfg.ConfidenceThreshold) || math.IsInf(cfg.ConfidenceThreshold, 0) || cfg.ConfidenceThreshold < 0 || cfg.ConfidenceThreshold > 1 {
		return nil, fmt.Errorf("SPLIT_CONFIDENCE_THRESHOLD must be between 0 and 1")
	}
	for _, tag := range strings.Split(value("SPLIT_OUTPUT_TAGS", value("AUTO_TAG", "paperless-gpt-auto")), ",") {
		tag = strings.TrimSpace(tag)
		if tag == "" || strings.EqualFold(tag, cfg.Tag) {
			return nil, fmt.Errorf("SPLIT_OUTPUT_TAGS must contain nonempty tags other than SPLIT_TAG")
		}
		if !hasSplitTag(cfg.OutputTags, tag) {
			cfg.OutputTags = append(cfg.OutputTags, tag)
		}
	}
	for _, tag := range []string{manualTag, autoTag, autoOcrTag, failTag, autoTagComplete, pdfOCRCompleteTag} {
		if tag != "" && strings.EqualFold(tag, cfg.Tag) {
			return nil, fmt.Errorf("SPLIT_TAG must differ from other processing tags")
		}
	}
	return cfg, nil
}

// automaticSplitBoundaries returns one-based after-page positions only when
// automatic Apply is enabled and every proposed boundary passes its checks.
// It returns nil for dry runs, ineligible suggestions, or invalid page ranges.
func automaticSplitBoundaries(cfg *SplitConfig, s *SplitSuggestion) []int {
	if !cfg.Auto || cfg.DryRun || s.Status != "review" || len(s.Boundaries) == 0 {
		return nil
	}
	pages := make([]int, 0, len(s.Boundaries))
	for _, b := range s.Boundaries {
		if b.Confidence < cfg.ConfidenceThreshold || b.Conflict || b.BlockedReason != "" {
			return nil
		}
		pages = append(pages, b.AfterPage)
	}
	if _, err := splitPagesString(s.PageCount, pages); err != nil {
		return nil
	}
	return pages
}

// hasSplitTag reports whether tags contains a case-insensitive match for tag.
func hasSplitTag(tags []string, tag string) bool {
	for _, candidate := range tags {
		if strings.EqualFold(candidate, tag) {
			return true
		}
	}
	return false
}

type SplitBoundary struct {
	AfterPage     int     `json:"after_page"`
	Confidence    float64 `json:"confidence"`
	Reason        string  `json:"reason"`
	Conflict      bool    `json:"conflict,omitempty"`
	BlockedReason string  `json:"blocked_reason,omitempty"`
}

type SplitPage struct {
	Page          int      `json:"page"`
	Text          string   `json:"text"`
	Width         float64  `json:"width_points"`
	Height        float64  `json:"height_points"`
	Orientation   string   `json:"orientation"`
	TextLength    int      `json:"text_length"`
	ImageCoverage float64  `json:"image_coverage"`
	Blank         bool     `json:"blank"`
	PrintedPage   int      `json:"printed_page,omitempty"`
	PrintedTotal  int      `json:"printed_total,omitempty"`
	Dates         []string `json:"dates,omitempty"`
	SenderBlock   string   `json:"sender_block"`
}

var splitPagination = regexp.MustCompile(`(?i)\b(?:page|seite)\s+(\d+)\s*(?:of|von)\s*(\d+)\b`)
var splitDates = regexp.MustCompile(`\b(?:\d{1,2}[./]\d{1,2}[./]\d{4}|\d{4}-\d{2}-\d{2})\b`)

// splitPageSignals derives bounded text excerpts, layout, and pagination signals
// for a one-based page number with dimensions measured in points.
func splitPageSignals(page int, width, height float64, a pdfrender.PageAnalysis) SplitPage {
	text := strings.TrimSpace(strings.ReplaceAll(a.Text, "\r\n", "\n"))
	lines := strings.Split(text, "\n")
	s := SplitPage{Page: page, Width: width, Height: height, Orientation: "portrait", TextLength: len([]rune(text)), ImageCoverage: a.ImageCoverage}
	if width > height {
		s.Orientation = "landscape"
	}
	if width == height {
		s.Orientation = "square"
	}
	s.Blank = s.TextLength < 10 && a.ImageCoverage < .05
	s.Text = truncateSplitText(strings.Join(lines[:min(len(lines), 40)], "\n"), 2000)
	s.SenderBlock = truncateSplitText(strings.Join(lines[:min(len(lines), 6)], "\n"), 400)
	s.Dates = splitDates.FindAllString(text, 5)
	matches := splitPagination.FindAllStringSubmatch(text, -1)
	if len(matches) == 1 {
		s.PrintedPage, _ = strconv.Atoi(matches[0][1])
		s.PrintedTotal, _ = strconv.Atoi(matches[0][2])
		if s.PrintedPage < 1 || s.PrintedTotal < s.PrintedPage {
			s.PrintedPage, s.PrintedTotal = 0, 0
		}
	}
	return s
}

// truncateSplitText returns at most limit Unicode code points from s.
// The limit must be nonnegative.
func truncateSplitText(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit])
	}
	return s
}

// extractSplitPages analyzes PDF bytes in document order using one-based page
// numbers, returning PDF processing or context cancellation errors.
func extractSplitPages(ctx context.Context, data []byte) ([]SplitPage, error) {
	pdf, err := pdfrender.Open(ctx, data)
	if err != nil {
		return nil, err
	}
	defer pdf.Close()
	pages := make([]SplitPage, pdf.NumPages())
	for i := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		analysis, err := pdf.AnalyzePage(i)
		if err != nil {
			return nil, err
		}
		w, h, err := pdf.PageSize(i)
		if err != nil {
			return nil, err
		}
		pages[i] = splitPageSignals(i+1, w, h, analysis)
	}
	return pages, nil
}

type splitWindow struct{ First, Last int }
type splitWindowResult struct {
	Window     splitWindow
	Boundaries []SplitBoundary
}

// splitWindows returns inclusive one-based page ranges with a two-page overlap.
// It returns nil when there are fewer than two pages or size is below three.
func splitWindows(pages, size int) []splitWindow {
	var windows []splitWindow
	if pages < 2 || size < 3 {
		return windows
	}
	for first := 1; first < pages; first += size - 2 {
		last := first + min(size, pages-first+1) - 1
		windows = append(windows, splitWindow{first, last})
		if last == pages {
			break
		}
	}
	return windows
}

// renderSplitPrompt executes tmpl with a window's page signals and document page
// count. It rejects a nil template or fewer than two pages and returns encoding
// or template errors, possibly with partial output for template errors.
func renderSplitPrompt(tmpl *template.Template, pages []SplitPage, total int) (string, error) {
	if tmpl == nil || len(pages) < 2 {
		return "", fmt.Errorf("split template or window unavailable")
	}
	data, err := json.Marshal(pages)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = tmpl.Execute(&out, map[string]interface{}{"Pages": string(data), "TotalPages": total, "FirstPage": pages[0].Page, "FirstDecisionPage": pages[0].Page + 1, "LastPage": pages[len(pages)-1].Page})
	return out.String(), err
}

// parseSplitBoundaries validates model JSON for an inclusive one-based window.
// It accepts ordered after_page boundaries or decisions for every page after
// first through last, returning only selected boundaries or a validation error.
func parseSplitBoundaries(response string, first, last int) ([]SplitBoundary, error) {
	if len(response) > 1024*1024 {
		return nil, fmt.Errorf("split response exceeds 1 MiB")
	}
	var raw []struct {
		AfterPage  *int            `json:"after_page"`
		Page       *int            `json:"page"`
		Confidence *float64        `json:"confidence"`
		Reason     *string         `json:"reason"`
		Split      json.RawMessage `json:"split"`
	}
	decoder := json.NewDecoder(strings.NewReader(response))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("invalid split JSON: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("expected a JSON array, not null")
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after split JSON")
	}
	out := make([]SplitBoundary, 0, len(raw))
	decisions := len(raw) > 0 && len(raw[0].Split) > 0
	if decisions && len(raw) != last-first {
		return nil, fmt.Errorf("provide a split decision for every page from %d through %d", first+1, last)
	}
	previous := first - 1
	for _, b := range raw {
		if (len(b.Split) > 0) != decisions {
			return nil, fmt.Errorf("cannot mix split decisions with boundary-only objects")
		}
		decision := string(bytes.TrimSpace(b.Split))
		if decisions && decision != "true" && decision != "false" {
			return nil, fmt.Errorf("split must be true or false")
		}
		var afterPage int
		if decisions {
			if b.Page == nil || b.AfterPage != nil {
				return nil, fmt.Errorf("split decisions need page, not after_page")
			}
			afterPage = *b.Page - 1
		} else {
			if b.AfterPage == nil || b.Page != nil {
				return nil, fmt.Errorf("boundary-only objects need after_page, not page")
			}
			afterPage = *b.AfterPage
		}
		if b.Confidence == nil || b.Reason == nil {
			return nil, fmt.Errorf("each decision needs confidence and reason")
		}
		if afterPage <= previous || afterPage >= last {
			if decisions {
				return nil, fmt.Errorf("pages must be ordered, unique and between %d and %d", first+1, last)
			}
			return nil, fmt.Errorf("boundaries must be ordered, unique and between %d and %d", first, last-1)
		}
		if *b.Confidence < 0 || *b.Confidence > 1 || strings.TrimSpace(*b.Reason) == "" || len([]rune(*b.Reason)) > 500 {
			return nil, fmt.Errorf("invalid confidence or reason")
		}
		if !decisions || decision == "true" {
			out = append(out, SplitBoundary{AfterPage: afterPage, Confidence: *b.Confidence, Reason: *b.Reason})
		}
		previous = afterPage
	}
	return out, nil
}

// mergeSplitWindows merges boundaries in page order, retaining the lowest
// confidence and marking a conflict when any covering window omits a boundary.
func mergeSplitWindows(results []splitWindowResult) []SplitBoundary {
	merged := map[int]SplitBoundary{}
	votes := map[int]int{}
	for _, result := range results {
		for _, b := range result.Boundaries {
			votes[b.AfterPage]++
			if old, ok := merged[b.AfterPage]; !ok || b.Confidence < old.Confidence {
				merged[b.AfterPage] = b
			}
		}
	}
	out := make([]SplitBoundary, 0, len(merged))
	for page, b := range merged {
		seen := 0
		for _, result := range results {
			if page >= result.Window.First && page < result.Window.Last {
				seen++
			}
		}
		b.Conflict = votes[page] != seen
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AfterPage < out[j].AfterPage })
	return out
}

// applySplitRules sets BlockedReason in place for boundaries beside pages with
// insufficient text or inside a printed pagination range contained in the PDF.
func applySplitRules(pages []SplitPage, boundaries []SplitBoundary) {
	for _, p := range pages {
		if p.TextLength < 10 {
			for i := range boundaries {
				if boundaries[i].AfterPage == p.Page-1 || boundaries[i].AfterPage == p.Page {
					boundaries[i].BlockedReason = fmt.Sprintf("Page %d has insufficient OCR text; review the adjacent boundaries", p.Page)
				}
			}
		}
		if p.PrintedPage == 0 {
			continue
		}
		first := p.Page - p.PrintedPage + 1
		// Invalid or incomplete pagination remains advisory, never extrapolate outside the PDF.
		if first < 1 || p.PrintedTotal > len(pages)-first+1 {
			continue
		}
		last := first + p.PrintedTotal - 1
		for i := range boundaries {
			if boundaries[i].AfterPage >= first && boundaries[i].AfterPage < last {
				boundaries[i].BlockedReason = fmt.Sprintf("Page %d says %d of %d; pages %d-%d belong together", p.Page, p.PrintedPage, p.PrintedTotal, first, last)
			}
		}
	}
}

// splitPagesString converts one-based after-page positions into comma-separated
// inclusive page ranges. It rejects empty, unordered, duplicate, or out-of-range
// boundaries and documents with fewer than two pages.
func splitPagesString(total int, boundaries []int) (string, error) {
	if total < 2 || len(boundaries) == 0 {
		return "", fmt.Errorf("a split requires at least one boundary")
	}
	parts := make([]string, 0, len(boundaries)+1)
	first := 1
	for _, last := range append(append([]int{}, boundaries...), total) {
		if last < first || last > total || (last == total && len(parts) < len(boundaries)) {
			return "", fmt.Errorf("boundaries must be ordered, unique and inside the document")
		}
		part := strconv.Itoa(first)
		if last > first {
			part += "-" + strconv.Itoa(last)
		}
		parts = append(parts, part)
		first = last + 1
	}
	return strings.Join(parts, ","), nil
}
