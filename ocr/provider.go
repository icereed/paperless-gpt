package ocr

import (
	"context"
	"fmt"
)

// Provider defines the interface for OCR processing
type Provider interface {
	ProcessImage(ctx context.Context, imageContent []byte) (string, error)
}

// Config holds the OCR provider configuration
type Config struct {
	// Provider type (e.g., "llm", "google_docai")
	Provider string

	// Google Document AI settings
	GoogleProjectID   string
	GoogleLocation    string
	GoogleProcessorID string

	// LLM settings (from existing config)
	VisionLLMProvider string
	VisionLLMModel    string
}

// NewProvider creates a new OCR provider based on configuration
func NewProvider(config Config) (Provider, error) {
	switch config.Provider {
	case "google_docai":
		if config.GoogleProjectID == "" || config.GoogleLocation == "" || config.GoogleProcessorID == "" {
			return nil, fmt.Errorf("missing required Google Document AI configuration")
		}
		return newGoogleDocAIProvider(config)
	case "llm":
		if config.VisionLLMProvider == "" || config.VisionLLMModel == "" {
			return nil, fmt.Errorf("missing required LLM configuration")
		}
		return newLLMProvider(config)
	default:
		return nil, fmt.Errorf("unsupported OCR provider: %s", config.Provider)
	}
}
