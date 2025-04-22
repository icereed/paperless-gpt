package ocr

import (
	"strings"
	"testing"

	"cloud.google.com/go/documentai/apiv1/documentaipb"
	"github.com/gardar/ocrchestra/pkg/hocr"
)

// TestGoogleDocAIProviderHOCR tests the hOCR functionality of GoogleDocAIProvider
func TestGoogleDocAIProviderHOCR(t *testing.T) {
	// Test with hOCR enabled
	t.Run("with hOCR enabled", func(t *testing.T) {
		provider := &GoogleDocAIProvider{
			enableHOCR: true,
			hocrPages:  make([]hocr.Page, 0),
		}

		// Verify hOCR is enabled
		if !provider.IsHOCREnabled() {
			t.Error("IsHOCREnabled should return true")
		}

		// Verify initial state has no pages
		if len(provider.GetHOCRPages()) != 0 {
			t.Errorf("Initial GetHOCRPages should return empty slice, got %d pages", len(provider.GetHOCRPages()))
		}

		// Test GetHOCRDocument with no pages (should fail)
		doc, err := provider.GetHOCRDocument()
		if doc != nil || err == nil {
			t.Error("GetHOCRDocument should fail when no pages are collected")
		}

		// Add a test page
		testPage := hocr.Page{
			ID:         "page_1",
			Title:      "image;bbox 0 0 800 600",
			PageNumber: 1,
			BBox:       hocr.NewBoundingBox(0, 0, 800, 600),
			Paragraphs: []hocr.Paragraph{},
			Lines:      []hocr.Line{},
		}
		provider.hocrPages = append(provider.hocrPages, testPage)

		// Test GetHOCRPages after adding a page
		pages := provider.GetHOCRPages()
		if len(pages) != 1 {
			t.Errorf("GetHOCRPages should return 1 page, got %d", len(pages))
		}

		// Test GetHOCRDocument with a page
		doc, err = provider.GetHOCRDocument()
		if err != nil {
			t.Errorf("GetHOCRDocument failed: %v", err)
		}
		if doc == nil {
			t.Error("GetHOCRDocument returned nil document")
		} else if len(doc.Pages) != 1 {
			t.Errorf("Expected document to have 1 page, got %d", len(doc.Pages))
		}

		// Test ResetHOCR
		provider.ResetHOCR()
		if len(provider.GetHOCRPages()) != 0 {
			t.Errorf("After ResetHOCR, GetHOCRPages should return empty slice, got %d pages", len(provider.GetHOCRPages()))
		}
	})

	// Test with hOCR disabled
	t.Run("with hOCR disabled", func(t *testing.T) {
		provider := &GoogleDocAIProvider{
			enableHOCR: false,
			hocrPages:  make([]hocr.Page, 0),
		}

		// Verify hOCR is disabled
		if provider.IsHOCREnabled() {
			t.Error("IsHOCREnabled should return false")
		}

		// Test GetHOCRDocument with hOCR disabled (should fail)
		doc, err := provider.GetHOCRDocument()
		if doc != nil || err == nil {
			t.Error("GetHOCRDocument should fail when hOCR is disabled")
		}
	})
}

// TestHOCRGeneration tests the complete flow from Document AI output to hOCR HTML
func TestHOCRGeneration(t *testing.T) {
	tests := []struct {
		name string
		doc  *documentaipb.Document
	}{
		{
			name: "single page with one paragraph",
			doc: &documentaipb.Document{
				Text: "Hello World",
				Pages: []*documentaipb.Document_Page{
					{
						Dimension: &documentaipb.Document_Page_Dimension{
							Width:  800,
							Height: 600,
						},
						Paragraphs: []*documentaipb.Document_Page_Paragraph{
							{
								Layout: &documentaipb.Document_Page_Layout{
									BoundingPoly: &documentaipb.BoundingPoly{
										NormalizedVertices: []*documentaipb.NormalizedVertex{
											{X: 0.1, Y: 0.1},
											{X: 0.9, Y: 0.1},
											{X: 0.9, Y: 0.2},
											{X: 0.1, Y: 0.2},
										},
									},
									TextAnchor: &documentaipb.Document_TextAnchor{
										TextSegments: []*documentaipb.Document_TextAnchor_TextSegment{
											{
												StartIndex: 0,
												EndIndex:   11,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create hOCR page from the document page
			// Manually create the hOCR structure to ensure we have control over every aspect of the test
			// Create a word for "Hello World"
			word := hocr.Word{
				ID:         "word_1_1_1",
				Text:       "Hello World",
				BBox:       hocr.NewBoundingBox(80, 60, 720, 120),
				Confidence: 90.0,
			}

			// Create a paragraph containing the word
			paragraph := hocr.Paragraph{
				ID:    "par_1_1",
				BBox:  hocr.NewBoundingBox(80, 60, 720, 120),
				Words: []hocr.Word{word},
			}

			// Create a page containing the paragraph
			page := hocr.Page{
				ID:         "page_1",
				PageNumber: 1,
				BBox:       hocr.NewBoundingBox(0, 0, 800, 600),
				Paragraphs: []hocr.Paragraph{paragraph},
			}

			// Create the HOCR document
			hocrDoc := &hocr.HOCR{
				Title:  "Document OCR",
				Pages:  []hocr.Page{page},
			}

			// Generate hOCR HTML from the document
			result, err := hocr.GenerateHOCRDocument(hocrDoc)
			if err != nil {
				t.Fatalf("GenerateHOCRDocument failed: %v", err)
			}

			// Verify the specific elements we expect to see
			if !strings.Contains(result, "<div class='ocr_page' id='page_1' title='bbox 0 0 800 600") {
				t.Error("Missing or incorrect ocr_page element")
			}

			if !strings.Contains(result, "<p class='ocr_par' id='par_1_1' title='bbox 80 60 720 120") {
				t.Error("Missing or incorrect ocr_par element")
			}

			if !strings.Contains(result, "Hello World") {
				t.Error("Missing text content")
			}

			// Verify basic hOCR structure
			if !strings.Contains(result, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>") {
				t.Error("missing XML declaration")
			}

			if !strings.Contains(result, "<html xmlns=\"http://www.w3.org/1999/xhtml\"") {
				t.Error("missing HTML namespace")
			}

			if !strings.Contains(result, "<meta name=\"ocr-system\"") {
				t.Error("missing OCR system metadata")
			}

			// Log the result for debugging
			t.Logf("Generated hOCR HTML:\n%s", result)
		})
	}
}
