package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"paperless-gpt/internal/pdfrender"
)

// Page routing skips OCR for pages that already carry a good text layer,
// an idea from doc-router (github.com/misbahsy/doc-router): "don't pay to
// OCR a page that already has text on it". It looks at the ORIGINAL file,
// not paperless-ngx' archive version: the archive carries paperless-ngx'
// own Tesseract text on every scanned page, which is exactly the text
// paperless-gpt is there to improve on.
//
// Clear cases are decided locally. Only the grey zone (a text layer that
// might be garbled, little text over a large image) goes to the optional
// Jev judge, and Jev only ever sees numbers about the page, never its text.

const (
	pageRoutingOff   = "off"
	pageRoutingLocal = "local"
	pageRoutingJev   = "jev"
)

var (
	ocrPageRouting   = strings.ToLower(strings.TrimSpace(os.Getenv("OCR_PAGE_ROUTING")))
	jevAPIKey        = firstNonEmpty(os.Getenv("JEV_API_KEY"), os.Getenv("TYPESAFE_API_KEY"))
	jevAPIURL        = firstNonEmpty(os.Getenv("JEV_API_URL"), "https://api.typesafe.ai/v1/systemone")
	jevModel         = firstNonEmpty(os.Getenv("JEV_MODEL"), "jev-latest")
	jevThresholdText = os.Getenv("JEV_NEEDS_OCR_THRESHOLD")
	// jevSendTextSample adds the first characters of the page's text layer
	// to what Jev sees. It helps Jev tell a watermark or page header from
	// real content, but it is document text leaving the instance, so it is
	// opt-in.
	jevSendTextSample = strings.EqualFold(os.Getenv("JEV_SEND_TEXT_SAMPLE"), "true")
)

// jevTextSampleChars bounds the text sample sent with JEV_SEND_TEXT_SAMPLE.
const jevTextSampleChars = 200

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// PageSignals are the numbers a judge decides on. They are derived from the
// page and contain no document text, so they can be sent to an external
// judge without disclosing content.
type PageSignals struct {
	Page               int     `json:"page"`
	TextChars          int     `json:"text_layer_chars"`
	Words              int     `json:"words"`
	LetterShare        float64 `json:"letter_or_digit_share"`
	GarbledShare       float64 `json:"garbled_char_share"`
	ImageCoverage      float64 `json:"image_area_share"`
	InvisibleTextShare float64 `json:"invisible_text_share"`
	// EncodingErrors counts spots where a font's broken encoding shows up
	// inside a word, typically umlauts: "f¸r", "Gesch‰ft", "fÃ¼r".
	EncodingErrors int `json:"encoding_errors"`
	// TextSample is only filled with JEV_SEND_TEXT_SAMPLE=true.
	TextSample string `json:"text_sample,omitempty"`
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

// pageVerdict is a judge's decision for one page.
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
		// Measured, not judged: broken encodings are reliable to count and
		// were the case an external judge got wrong most often.
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

// localVerdict decides the grey zone with fixed thresholds.
func localVerdict(s PageSignals) pageVerdict {
	switch {
	case s.ImageCoverage >= 0.8 && s.TextChars < 400:
		return pageVerdict{true, "little text over a large image"}
	}
	return pageVerdict{false, "usable text layer"}
}

// jevJudge asks Jev (TypeSafe System One) about grey-zone pages.
type jevJudge struct {
	url, key, model string
	threshold       float64
	client          *http.Client
}

func newJevJudge() (*jevJudge, error) {
	if jevAPIKey == "" {
		return nil, fmt.Errorf("OCR_PAGE_ROUTING=jev needs JEV_API_KEY")
	}
	threshold := 0.5
	if jevThresholdText != "" {
		t, err := strconv.ParseFloat(jevThresholdText, 64)
		if err != nil || t <= 0 || t >= 1 {
			return nil, fmt.Errorf("JEV_NEEDS_OCR_THRESHOLD must be a number between 0 and 1, got %q", jevThresholdText)
		}
		threshold = t
	}
	return &jevJudge{
		url:       jevAPIURL,
		key:       jevAPIKey,
		model:     jevModel,
		threshold: threshold,
		client:    &http.Client{Timeout: 15 * time.Second},
	}, nil
}

const jevNeedsOCRInstructions = "The PDF page has no usable text layer, so OCR is needed to read it: " +
	"the page is a scanned image, its text layer is garbled or missing, or the text layer is only a " +
	"watermark, header or page number over an image."

func (j *jevJudge) needsOCR(ctx context.Context, s PageSignals) (float64, error) {
	body, err := json.Marshal(map[string]any{
		"state": s,
		"model": j.model,
		"questions": map[string]any{
			"needs_ocr": map[string]any{"type": "noul", "instructions": jevNeedsOCRInstructions},
		},
	})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+j.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := j.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("jev answered %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Answers struct {
			NeedsOCR struct {
				Noul *float64 `json:"noul"`
			} `json:"needs_ocr"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, fmt.Errorf("unexpected jev answer: %w", err)
	}
	if out.Answers.NeedsOCR.Noul == nil {
		return 0, fmt.Errorf("jev answer has no needs_ocr probability")
	}
	return *out.Answers.NeedsOCR.Noul, nil
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
	if ocrPageRouting == "" || ocrPageRouting == pageRoutingOff {
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

	var judge *jevJudge
	if ocrPageRouting == pageRoutingJev {
		judge, err = newJevJudge()
		if err != nil {
			logger.Warnf("Page routing uses local rules only: %v", err)
		}
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
			if judge != nil {
				asked := signals
				if jevSendTextSample {
					asked.TextSample = truncateRunes(strings.Join(strings.Fields(analysis.Text), " "), jevTextSampleChars)
				}
				p, err := judge.needsOCR(ctx, asked)
				if err != nil {
					logger.Warnf("Page routing: Jev unavailable for page %d, using local rules: %v", i+1, err)
				} else {
					verdict = pageVerdict{p >= judge.threshold, fmt.Sprintf("Jev: needs OCR with p=%.2f", p)}
				}
			}
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

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
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
