package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"paperless-gpt/internal/pdfrender"
)

func TestPageSignals(t *testing.T) {
	s := pageSignals(2, pdfrender.PageAnalysis{
		Text:                 "Rechnung RE-2026-0417\nMontana Energieversorgung GmbH",
		ImageCoverage:        0.123,
		TextObjects:          4,
		InvisibleTextObjects: 1,
	})
	assert.Equal(t, 2, s.Page)
	assert.Equal(t, 5, s.Words)
	assert.Equal(t, 0.12, s.ImageCoverage)
	assert.Equal(t, 0.25, s.InvisibleTextShare)
	assert.Zero(t, s.GarbledShare)
	assert.Greater(t, s.LetterShare, 0.8)

	mojibake := pageSignals(1, pdfrender.PageAnalysis{Text: "Hinweise zum Schutz Ihrer Daten f¸r das Firmenkundengesch‰ft. N‰here Informationen erhalten Sie von unserem R¸ckversicherer."})
	assert.Equal(t, 4, mojibake.EncodingErrors, "umlauts in the wrong code page")
	v, clear := clearVerdict(PageSignals{TextChars: 5000, Words: 700, LetterShare: 0.9, EncodingErrors: mojibake.EncodingErrors})
	assert.True(t, clear)
	assert.True(t, v.NeedsOCR, "a text layer with broken umlauts goes to OCR")
	assert.Zero(t, pageSignals(1, pdfrender.PageAnalysis{Text: "Preis: 12,50 € § 3 Abs. 2, Grüße ´quoted´"}).EncodingErrors, "stand-ins outside words are fine")

	garbled := pageSignals(1, pdfrender.PageAnalysis{Text: "Rechnung Ã¤Ã¶ ��  Betrag"})
	assert.Greater(t, garbled.GarbledShare, 0.05, "replacement, private-use and mojibake characters count as garbled")
}

func TestPageVerdicts(t *testing.T) {
	tests := []struct {
		name     string
		signals  PageSignals
		clear    bool
		needsOCR bool
	}{
		{"empty page", PageSignals{TextChars: 3}, true, true},
		{"OCR layer over a scan", PageSignals{TextChars: 1800, Words: 300, LetterShare: 0.9, ImageCoverage: 1, InvisibleTextShare: 1}, true, true},
		{"clean digital text", PageSignals{TextChars: 1500, Words: 250, LetterShare: 0.92, ImageCoverage: 0.05}, true, false},
		{"garbled digital text", PageSignals{TextChars: 900, Words: 120, LetterShare: 0.4, GarbledShare: 0.2}, true, true},
		{"watermark over a large image", PageSignals{TextChars: 40, Words: 6, LetterShare: 0.95, ImageCoverage: 0.93}, false, true},
		{"short digital page", PageSignals{TextChars: 110, Words: 15, LetterShare: 0.9}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, clear := clearVerdict(tt.signals)
			assert.Equal(t, tt.clear, clear)
			if !clear {
				v = localVerdict(tt.signals)
			}
			assert.Equal(t, tt.needsOCR, v.NeedsOCR, v.Reason)
		})
	}
}

func TestJevJudgeSendsOnlyNumbers(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &got))
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"needs_ocr":{"type":"noul","noul":0.83}}}`))
	}))
	defer server.Close()

	judge := &jevJudge{url: server.URL, key: "test-key", model: "jev-latest", threshold: 0.5, client: server.Client()}
	p, err := judge.needsOCR(context.Background(), PageSignals{Page: 3, TextChars: 40, Words: 6, ImageCoverage: 0.93})
	require.NoError(t, err)
	assert.Equal(t, 0.83, p)

	state, ok := got["state"].(map[string]any)
	require.True(t, ok, "state is the page statistics object")
	for key, value := range state {
		_, isString := value.(string)
		assert.False(t, isString, "state.%s must not carry text", key)
	}
	assert.NotContains(t, state, "text_sample", "no text sample unless JEV_SEND_TEXT_SAMPLE=true")
	assert.Equal(t, "jev-latest", got["model"])
	q := got["questions"].(map[string]any)["needs_ocr"].(map[string]any)
	assert.Equal(t, "noul", q["type"])
}

func TestJevJudgeErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/unauthorized":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"bad key"}`))
		default:
			_, _ = w.Write([]byte(`{"answers":{}}`))
		}
	}))
	defer server.Close()

	judge := &jevJudge{url: server.URL + "/unauthorized", key: "k", model: "jev-latest", client: server.Client()}
	_, err := judge.needsOCR(context.Background(), PageSignals{})
	assert.ErrorContains(t, err, "401")

	judge.url = server.URL + "/empty"
	_, err = judge.needsOCR(context.Background(), PageSignals{})
	assert.ErrorContains(t, err, "no needs_ocr probability")
}

// originalPDFClient serves a fixed original file.
type originalPDFClient struct {
	mockPaperlessClient
	original []byte
}

func (c *originalPDFClient) DownloadDocumentAsPDF(context.Context, int, int, bool) ([]string, []byte, int, error) {
	return nil, c.original, 0, nil
}

func TestPlanPageRouting(t *testing.T) {
	prev := ocrPageRouting
	t.Cleanup(func() { ocrPageRouting = prev })
	logger := logrus.WithField("test", t.Name())

	digital, err := os.ReadFile("tests/pdf/sample.pdf")
	require.NoError(t, err)
	scan, err := os.ReadFile("tests/pdf/five-pager.pdf")
	require.NoError(t, err)

	ocrPageRouting = pageRoutingOff
	app := &App{Client: &originalPDFClient{original: digital}}
	assert.Nil(t, app.planPageRouting(context.Background(), 1, 1, 1, logger), "off means OCR every page")

	ocrPageRouting = pageRoutingLocal
	routes := app.planPageRouting(context.Background(), 1, 1, 1, logger)
	require.Len(t, routes, 1)
	assert.True(t, routes[0].UseTextLayer, "a born-digital page keeps its own text")
	assert.NotEmpty(t, strings.TrimSpace(routes[0].Text))

	app = &App{Client: &originalPDFClient{original: scan}}
	routes = app.planPageRouting(context.Background(), 1, 3, 5, logger)
	require.Len(t, routes, 3)
	for i, r := range routes {
		assert.False(t, r.UseTextLayer, "scanned page %d is OCRed again", i+1)
	}

	app = &App{Client: &originalPDFClient{original: []byte("\xff\xd8\xff not a pdf")}}
	assert.Nil(t, app.planPageRouting(context.Background(), 1, 1, 1, logger), "a non-PDF original is OCRed as before")

	app = &App{Client: &originalPDFClient{original: digital}}
	assert.Nil(t, app.planPageRouting(context.Background(), 1, 1, 2, logger), "an original with a different page count than the archive disables routing, even with a page limit")
}
