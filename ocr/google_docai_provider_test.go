package ocr

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"cloud.google.com/go/documentai/apiv1/documentaipb"
)

func TestGenerateHOCR(t *testing.T) {
	tests := []struct {
		name     string
		doc      *documentaipb.Document
		expected string
	}{
		{
			name:     "empty document",
			doc:      &documentaipb.Document{},
			expected: "",
		},
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
			expected: "(?s).*<div class='ocr_page' id='page_1' title='image;bbox 0 0 800 600'>.*" +
				"<p class='ocr_par' id='par_1_1' title='bbox 80 60 720 120'>.*" +
				"<span class='ocrx_word'>Hello World</span>.*</p>.*</div>.*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateHOCR(tt.doc)

			if tt.expected == "" {
				if result != "" {
					t.Errorf("expected empty string, got %v", result)
				}
				return
			}

			matched, err := regexp.MatchString(tt.expected, result)
			if err != nil {
				t.Fatalf("error matching regex: %v", err)
			}
			if !matched {
				t.Errorf("expected to match regex %v\ngot: %v", tt.expected, result)
			}

			// Verify basic hOCR structure
			if !strings.Contains(result, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>") {
				t.Error("missing XML declaration")
			}
			if !strings.Contains(result, "<html xmlns=\"http://www.w3.org/1999/xhtml\"") {
				t.Error("missing HTML namespace")
			}
			if !strings.Contains(result, "<meta name='ocr-system' content='google-docai'") {
				t.Error("missing OCR system metadata")
			}
		})
	}
}

func testContext() context.Context {
	return context.Background()
}
