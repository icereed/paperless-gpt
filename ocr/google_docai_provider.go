package ocr

import (
	"context"
	"fmt"
	"sync"

	documentai "cloud.google.com/go/documentai/apiv1"
	"cloud.google.com/go/documentai/apiv1/documentaipb"
	"github.com/gabriel-vasile/mimetype"
	"github.com/gardar/ocrchestra/pkg/gdocai"
	"github.com/gardar/ocrchestra/pkg/hocr"
	"github.com/sirupsen/logrus"
	"google.golang.org/api/option"
)

// GoogleDocAIProvider implements OCR using Google Document AI
type GoogleDocAIProvider struct {
	projectID   string
	location    string
	processorID string
	client      *documentai.DocumentProcessorClient
	enableHOCR  bool        // Whether HOCR generation is enabled
	mu          sync.Mutex  // Add mutex for thread safety
	hocrPages   []hocr.Page // Storage for HOCR pages
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
		enableHOCR:  config.EnableHOCR,
		hocrPages:   make([]hocr.Page, 0),
	}

	logger.WithField("enable_hocr", config.EnableHOCR).Info("Successfully initialized Google Document AI provider")
	return provider, nil
}

func (p *GoogleDocAIProvider) ProcessImage(ctx context.Context, imageContent []byte, pageNumber int) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"project_id":   p.projectID,
		"location":     p.location,
		"processor_id": p.processorID,
		"page_number":  pageNumber,
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
		"page_number":  fmt.Sprintf("%d", pageNumber),
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

	// Create hOCR page structure (only if hOCR is enabled)
	if p.enableHOCR && len(resp.Document.GetPages()) > 0 {
		// Use the provided page number (1-based)
		hocrPage, err := gdocai.CreateHOCRPage(resp.Document.Pages[0], resp.Document.Text, pageNumber)
		if err != nil {
			logger.WithError(err).Error("Failed to create HOCR page")
		} else {
			// Store the HOCR page
			p.mu.Lock()
			p.hocrPages = append(p.hocrPages, hocrPage)
			p.mu.Unlock()
			result.HOCRPage = &hocrPage

			logger.WithField("page_number", pageNumber).Info("Created and stored HOCR page struct")
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

// IsHOCREnabled returns whether hOCR generation is enabled
func (p *GoogleDocAIProvider) IsHOCREnabled() bool {
	return p.enableHOCR
}

// GetHOCRPages returns the collected hOCR pages struct
func (p *GoogleDocAIProvider) GetHOCRPages() []hocr.Page {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]hocr.Page, len(p.hocrPages))
	copy(result, p.hocrPages)
	return result
}

// GetHOCRDocument creates an hOCR document from the collected pages
func (p *GoogleDocAIProvider) GetHOCRDocument() (*hocr.HOCR, error) {
	if !p.enableHOCR {
		return nil, fmt.Errorf("hOCR generation is not enabled")
	}

	p.mu.Lock()
	pageCount := len(p.hocrPages)
	pages := make([]hocr.Page, pageCount)
	copy(pages, p.hocrPages)
	p.mu.Unlock()

	if pageCount == 0 {
		return nil, fmt.Errorf("no hOCR pages collected")
	}

	// Create hOCR document struct from the collected pages
	hocrDoc, err := gdocai.CreateHOCRDocument(nil, pages...)
	if err != nil {
		return nil, fmt.Errorf("failed to create hOCR document struct: %w", err)
	}

	return hocrDoc, nil
}

// ResetHOCR clears the collected HOCR pages
func (p *GoogleDocAIProvider) ResetHOCR() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hocrPages = make([]hocr.Page, 0)
}

// Close releases resources used by the provider
func (p *GoogleDocAIProvider) Close() error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}
