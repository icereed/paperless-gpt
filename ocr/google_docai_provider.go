package ocr

import (
	"context"
	"fmt"

	documentai "cloud.google.com/go/documentai/apiv1"
	"cloud.google.com/go/documentai/apiv1/documentaipb"
	"github.com/gabriel-vasile/mimetype"
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
	ctx := context.Background()
	endpoint := fmt.Sprintf("%s-documentai.googleapis.com:443", config.GoogleLocation)

	client, err := documentai.NewDocumentProcessorClient(ctx, option.WithEndpoint(endpoint))
	if err != nil {
		return nil, fmt.Errorf("error creating Document AI client: %w", err)
	}

	return &GoogleDocAIProvider{
		projectID:   config.GoogleProjectID,
		location:    config.GoogleLocation,
		processorID: config.GoogleProcessorID,
		client:      client,
	}, nil
}

func (p *GoogleDocAIProvider) ProcessImage(ctx context.Context, imageContent []byte) (string, error) {
	// Detect MIME type
	mtype := mimetype.Detect(imageContent)
	if !isImageMIMEType(mtype.String()) {
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

	resp, err := p.client.ProcessDocument(ctx, req)
	if err != nil {
		return "", fmt.Errorf("error processing document: %w", err)
	}

	if resp == nil || resp.Document == nil {
		return "", fmt.Errorf("received nil response or document from Document AI")
	}

	if resp.Document.Error != nil {
		return "", fmt.Errorf("document processing error: %s", resp.Document.Error.Message)
	}

	return resp.Document.Text, nil
}

// isImageMIMEType checks if the given MIME type is a supported image type
func isImageMIMEType(mimeType string) bool {
	switch mimeType {
	case "image/jpeg", "image/jpg", "image/png", "image/tiff", "image/bmp", "application/pdf":
		return true
	default:
		return false
	}
}

// Close releases resources used by the provider
func (p *GoogleDocAIProvider) Close() error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}
