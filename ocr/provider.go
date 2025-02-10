package ocr

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
)

var log = logrus.New()

// OCRResult holds the output from OCR processing
type OCRResult struct {
	// Plain text output (required)
	Text string

	// hOCR output (optional, if provider supports it)
	HOCR string

	// Additional provider-specific metadata
	Metadata map[string]string
}

// Provider defines the interface for OCR processing
type Provider interface {
	ProcessImage(ctx context.Context, imageContent []byte) (*OCRResult, error)
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

	// OCR output options
	EnableHOCR bool // Whether to request hOCR output if supported by the provider
}

// NewProvider creates a new OCR provider based on configuration
func NewProvider(config Config) (Provider, error) {
	log.Info("Initializing OCR provider: ", config.Provider)

	switch config.Provider {
	case "google_docai":
		if config.GoogleProjectID == "" || config.GoogleLocation == "" || config.GoogleProcessorID == "" {
			return nil, fmt.Errorf("missing required Google Document AI configuration")
		}
		log.WithFields(logrus.Fields{
			"location":     config.GoogleLocation,
			"processor_id": config.GoogleProcessorID,
		}).Info("Using Google Document AI provider")
		return newGoogleDocAIProvider(config)

	case "llm":
		if config.VisionLLMProvider == "" || config.VisionLLMModel == "" {
			return nil, fmt.Errorf("missing required LLM configuration")
		}
		log.WithFields(logrus.Fields{
			"provider": config.VisionLLMProvider,
			"model":    config.VisionLLMModel,
		}).Info("Using LLM OCR provider")
		return newLLMProvider(config)

	default:
		return nil, fmt.Errorf("unsupported OCR provider: %s", config.Provider)
	}
}

// SetLogLevel sets the logging level for the OCR package
func SetLogLevel(level logrus.Level) {
	log.SetLevel(level)
}
