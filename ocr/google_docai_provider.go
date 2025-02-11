package ocr

import (
	"context"
	"fmt"
	"strings"

	documentai "cloud.google.com/go/documentai/apiv1"
	"cloud.google.com/go/documentai/apiv1/documentaipb"
	"github.com/gabriel-vasile/mimetype"
	"github.com/sirupsen/logrus"
	"google.golang.org/api/option"
)

// GoogleDocAIProvider implements OCR using Google Document AI
type GoogleDocAIProvider struct {
	projectID   string
	location    string
	processorID string
	client      *documentai.DocumentProcessorClient
}

func newGoogleDocAIProvider(config Config) (*GoogleDocAIProvider, error) {
	logger := log.WithFields(logrus.Fields{
		"location":     config.GoogleLocation,
		"processor_id": config.GoogleProcessorID,
	})
	logger.Info("Creating new Google Document AI provider")

	ctx := context.Background()
	endpoint := fmt.Sprintf("%s-documentai.googleapis.com:443", config.GoogleLocation)

	client, err := documentai.NewDocumentProcessorClient(ctx, option.WithEndpoint(endpoint))
	if err != nil {
		logger.WithError(err).Error("Failed to create Document AI client")
		return nil, fmt.Errorf("error creating Document AI client: %w", err)
	}

	provider := &GoogleDocAIProvider{
		projectID:   config.GoogleProjectID,
		location:    config.GoogleLocation,
		processorID: config.GoogleProcessorID,
		client:      client,
	}

	logger.Info("Successfully initialized Google Document AI provider")
	return provider, nil
}

func (p *GoogleDocAIProvider) ProcessImage(ctx context.Context, imageContent []byte) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"project_id":   p.projectID,
		"location":     p.location,
		"processor_id": p.processorID,
	})
	logger.Debug("Starting Document AI processing")

	// Detect MIME type
	mtype := mimetype.Detect(imageContent)
	logger.WithField("mime_type", mtype.String()).Debug("Detected file type")

	if !isImageMIMEType(mtype.String()) {
		logger.WithField("mime_type", mtype.String()).Error("Unsupported file type")
		return nil, fmt.Errorf("unsupported file type: %s", mtype.String())
	}

	name := fmt.Sprintf("projects/%s/locations/%s/processors/%s", p.projectID, p.location, p.processorID)

	req := &documentaipb.ProcessRequest{
		Name: name,
		Source: &documentaipb.ProcessRequest_RawDocument{
			RawDocument: &documentaipb.RawDocument{
				Content:  imageContent,
				MimeType: mtype.String(),
			},
		},
	}

	logger.Debug("Sending request to Document AI")
	resp, err := p.client.ProcessDocument(ctx, req)
	if err != nil {
		logger.WithError(err).Error("Failed to process document")
		return nil, fmt.Errorf("error processing document: %w", err)
	}

	if resp == nil || resp.Document == nil {
		logger.Error("Received nil response or document from Document AI")
		return nil, fmt.Errorf("received nil response or document from Document AI")
	}

	if resp.Document.Error != nil {
		logger.WithField("error", resp.Document.Error.Message).Error("Document processing error")
		return nil, fmt.Errorf("document processing error: %s", resp.Document.Error.Message)
	}

	metadata := map[string]string{
		"provider":     "google_docai",
		"mime_type":    mtype.String(),
		"page_count":   fmt.Sprintf("%d", len(resp.Document.GetPages())),
		"processor_id": p.processorID,
	}

	// Safely add language code if available
	if pages := resp.Document.GetPages(); len(pages) > 0 {
		if langs := pages[0].GetDetectedLanguages(); len(langs) > 0 {
			metadata["lang_code"] = langs[0].GetLanguageCode()
		}
	}

	result := &OCRResult{
		Text:     resp.Document.Text,
		Metadata: metadata,
	}

	// Add hOCR output if available
	if len(resp.Document.GetPages()) > 0 {
		var hocr string
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.WithField("error", r).Error("Panic during hOCR generation")
				}
			}()
			hocr = generateHOCR(resp.Document)
		}()
		if hocr != "" {
			result.HOCR = hocr
		}
	}

	logger.WithField("content_length", len(result.Text)).Info("Successfully processed document")
	return result, nil
}

// isImageMIMEType checks if the given MIME type is a supported image type
func isImageMIMEType(mimeType string) bool {
	supportedTypes := map[string]bool{
		"image/jpeg":      true,
		"image/jpg":       true,
		"image/png":       true,
		"image/tiff":      true,
		"image/bmp":       true,
		"application/pdf": true,
	}
	return supportedTypes[mimeType]
}

// generateHOCR converts Document AI response to hOCR format
func generateHOCR(doc *documentaipb.Document) string {
	if len(doc.GetPages()) == 0 {
		return ""
	}

	var hocr strings.Builder
	hocr.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="en" lang="en">
<head>
    <title>OCR Output</title>
    <meta http-equiv="Content-Type" content="text/html;charset=utf-8" />
    <meta name='ocr-system' content='google-docai' />
</head>
<body>`)

	for pageNum, page := range doc.GetPages() {
		pageWidth := page.GetDimension().GetWidth()
		pageHeight := page.GetDimension().GetHeight()

		hocr.WriteString(fmt.Sprintf(`
    <div class='ocr_page' id='page_%d' title='image;bbox 0 0 %d %d'>`,
			pageNum+1, int(pageWidth), int(pageHeight)))

		// Process paragraphs
		for _, para := range page.GetParagraphs() {
			paraBox := para.GetLayout().GetBoundingPoly().GetNormalizedVertices()
			if len(paraBox) < 4 {
				continue
			}

			// Convert normalized coordinates to absolute
			x1 := int(paraBox[0].GetX() * pageWidth)
			y1 := int(paraBox[0].GetY() * pageHeight)
			x2 := int(paraBox[2].GetX() * pageWidth)
			y2 := int(paraBox[2].GetY() * pageHeight)

			hocr.WriteString(fmt.Sprintf(`
        <p class='ocr_par' id='par_%d_%d' title='bbox %d %d %d %d'>`,
				pageNum+1, len(page.GetParagraphs()), x1, y1, x2, y2))

			// Process words within paragraph
			for _, token := range para.GetLayout().GetTextAnchor().GetTextSegments() {
				text := doc.Text[token.GetStartIndex():token.GetEndIndex()]
				if text == "" {
					continue
				}

				hocr.WriteString(fmt.Sprintf(`
            <span class='ocrx_word'>%s</span>`, text))
			}

			hocr.WriteString("\n        </p>")
		}
		hocr.WriteString("\n    </div>")
	}

	hocr.WriteString("\n</body>\n</html>")
	return hocr.String()
}

// Close releases resources used by the provider
func (p *GoogleDocAIProvider) Close() error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}
