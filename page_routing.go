package main

import (
	"context"
	"os"
	"strings"
	"unicode"

	"paperless-gpt/internal/pdfrender"
)

// Page routing skips OCR for pages that already carry a good text layer,
// an idea from doc-router (github.com/misbahsy/doc-router): "don't pay to
// OCR a page that already has text on it". It looks at the ORIGINAL file,
// not paperless-ngx' archive version: the archive carries paperless-ngx'
// own Tesseract text on every scanned page, which is exactly the text
// paperless-gpt is there to improve on.

// ocrSkipDigitalPages turns page routing on (OCR_SKIP_DIGITAL_PAGES=true).
var ocrSkipDigitalPages = strings.EqualFold(strings.TrimSpace(os.Getenv("OCR_SKIP_DIGITAL_PAGES")), "true")

// PageSignals are the measurements page routing decides on.
type PageSignals struct {
	Page               int
	TextChars          int
	Words              int
	LetterShare        float64
	GarbledShare       float64
	ImageCoverage      float64
	InvisibleTextShare float64
	// EncodingErrors counts spots where a font's broken encoding shows up
	// inside a word, typically umlauts: "f¸r", "Gesch‰ft", "fÃ¼r".
	EncodingErrors int
}

func pageSignals(page int, a pdfrender.PageAnalysis) PageSignals {
	s := PageSignals{Page: page, ImageCoverage: round2(a.ImageCoverage)}
	if a.TextObjects > 0 {
		s.InvisibleTextShare = round2(float64(a.InvisibleTextObjects) / float64(a.TextObjects))
	}
	var visible, letters, garbled int
	runes := []rune(a.Text)
	for i, r := range runes {
		if unicode.IsSpace(r) {
			continue
		}
		visible++
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			letters++
		}
		// Replacement characters, private-use glyphs and control characters
		// are what a broken font encoding produces; "Ã"/"Â" followed by a
		// Latin-1 character is UTF-8 read as Latin-1.
		if r == unicode.ReplacementChar || unicode.In(r, unicode.Co) || unicode.IsControl(r) ||
			((r == 'Ã' || r == 'Â') && i+1 < len(runes) && runes[i+1] >= 0x80 && runes[i+1] <= 0xFF) {
			garbled++
		}
	}
	s.TextChars = visible
	s.Words = len(strings.Fields(a.Text))
	s.EncodingErrors = countEncodingErrors(runes)
	if visible > 0 {
		s.LetterShare = round2(float64(letters) / float64(visible))
		s.GarbledShare = round2(float64(garbled) / float64(visible))
	}
	return s
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

// pageVerdict is the decision for one page.
type pageVerdict struct {
	NeedsOCR bool
	Reason   string
}

// clearVerdict decides the cases that need no judgement. ok is false for the
// grey zone.
func clearVerdict(s PageSignals) (v pageVerdict, ok bool) {
	switch {
	case s.TextChars < 30:
		return pageVerdict{true, "no text layer"}, true
	case s.EncodingErrors >= 3:
		return pageVerdict{true, "text layer has broken characters inside words (e.g. umlauts)"}, true
	case s.GarbledShare > 0.05 || s.LetterShare < 0.5:
		return pageVerdict{true, "text layer looks garbled"}, true
	case s.InvisibleTextShare >= 0.5:
		// A scanner's or another tool's OCR layer over the page image.
		// Replacing that OCR is what paperless-gpt is for.
		return pageVerdict{true, "OCR text layer over a scanned page"}, true
	case s.ImageCoverage < 0.2 && s.GarbledShare == 0 && s.LetterShare >= 0.6 && s.Words >= 20:
		return pageVerdict{false, "digital text layer"}, true
	}
	return pageVerdict{}, false
}

// localVerdict decides the remaining pages, e.g. a little text over a large image.
func localVerdict(s PageSignals) pageVerdict {
	switch {
	case s.ImageCoverage >= 0.8 && s.TextChars < 400:
		return pageVerdict{true, "little text over a large image"}
	}
	return pageVerdict{false, "usable text layer"}
}

// pageRoute is what to do with one page: OCR it, or use its text layer.
type pageRoute struct {
	UseTextLayer bool
	Text         string
	Reason       string
}

// planPageRouting decides, per page, whether OCR is needed. It returns nil
// (OCR every page) when routing is off, the original is not a PDF, or
// anything goes wrong: routing only ever saves work, it never blocks OCR.
// pages is the number of pages to process, totalPages the archive version's
// page count; the original must have exactly as many pages, or page i of the
// original might not be page i of the archive.
func (app *App) planPageRouting(ctx context.Context, documentID, pages, totalPages int, logger interface {
	Infof(string, ...any)
	Warnf(string, ...any)
}) []pageRoute {
	if !ocrSkipDigitalPages {
		return nil
	}
	_, original, _, err := app.Client.DownloadDocumentAsPDF(ctx, documentID, 0, false)
	if err != nil || !isPDFData(original) {
		if err != nil {
			logger.Warnf("Page routing skipped: could not read the original file: %v", err)
		}
		return nil
	}
	doc, err := pdfrender.Open(ctx, original)
	if err != nil {
		logger.Warnf("Page routing skipped: could not open the original PDF: %v", err)
		return nil
	}
	defer doc.Close()
	if doc.NumPages() != totalPages || pages > totalPages {
		logger.Warnf("Page routing skipped: the original has %d pages, the archive version %d", doc.NumPages(), totalPages)
		return nil
	}

	routes := make([]pageRoute, pages)
	skipped := 0
	for i := 0; i < pages; i++ {
		analysis, err := doc.AnalyzePage(i)
		if err != nil {
			logger.Warnf("Page routing: OCR for page %d, it could not be analyzed: %v", i+1, err)
			continue
		}
		signals := pageSignals(i+1, analysis)
		verdict, clear := clearVerdict(signals)
		if !clear {
			verdict = localVerdict(signals)
		}
		if !verdict.NeedsOCR {
			routes[i] = pageRoute{UseTextLayer: true, Text: strings.TrimSpace(analysis.Text), Reason: verdict.Reason}
			skipped++
		} else {
			routes[i] = pageRoute{Reason: verdict.Reason}
		}
	}
	logger.Infof("Page routing: %d of %d pages read from their text layer, %d sent to OCR", skipped, pages, pages-skipped)
	return routes
}

// mojibakeStandIns are the characters a wrong code page puts where umlauts
// and ß belong: Mac Roman and Windows-1252 text read as the other one gives
// "f¸r" and "Gesch‰ft". In real text they practically never sit between two
// letters.
const mojibakeStandIns = "¸‰ˆ˜´¨¤¦§"

// countEncodingErrors counts mojibake inside words: one of mojibakeStandIns
// between two letters, or UTF-8 read as Latin-1 ("Ã¼").
func countEncodingErrors(runes []rune) int {
	n := 0
	for i := 1; i+1 < len(runes); i++ {
		r := runes[i]
		switch {
		case strings.ContainsRune(mojibakeStandIns, r):
			if unicode.IsLetter(runes[i-1]) && unicode.IsLetter(runes[i+1]) {
				n++
			}
		case r == 'Ã' || r == 'Â':
			if runes[i+1] >= 0x80 && runes[i+1] <= 0xFF {
				n++
			}
		}
	}
	return n
}
