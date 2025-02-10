package ocr

import (
	"context"
	"fmt"

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

func (p *GoogleDocAIProvider) ProcessImage(ctx context.Context, imageContent []byte) (string, error) {
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
		return "", fmt.Errorf("unsupported file type: %s", mtype.String())
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
		return "", fmt.Errorf("error processing document: %w", err)
	}

	if resp == nil || resp.Document == nil {
		logger.Error("Received nil response or document from Document AI")
		return "", fmt.Errorf("received nil response or document from Document AI")
	}

	if resp.Document.Error != nil {
		logger.WithField("error", resp.Document.Error.Message).Error("Document processing error")
		return "", fmt.Errorf("document processing error: %s", resp.Document.Error.Message)
	}

	logger.WithField("content_length", len(resp.Document.Text)).Info("Successfully processed document")
	return resp.Document.Text, nil
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

// Close releases resources used by the provider
func (p *GoogleDocAIProvider) Close() error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}
