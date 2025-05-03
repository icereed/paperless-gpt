package ocr

import (
	"context"
	"fmt"

	"github.com/gardar/ocrchestra/pkg/hocr"
	"github.com/sirupsen/logrus"
)

var log = logrus.New()

// OCRResult holds the output from OCR processing
type OCRResult struct {
	// Plain text output (required)
	Text string

	// hOCR Page data (optional, if provider supports it)
	HOCRPage *hocr.Page

	// Additional provider-specific metadata
	Metadata map[string]string
}

// Provider defines the interface for OCR processing
type Provider interface {
	ProcessImage(ctx context.Context, imageContent []byte, pageNumber int) (*OCRResult, error)
}

// Config holds the OCR provider configuration
type Config struct {
	// Provider type (e.g., "llm", "google_docai", "azure", "mistral_ocr")
	Provider string

	// Mistral OCR settings
	MistralAPIKey string
	MistralModel  string // Optional, defaults to "mistral-ocr-latest"

	// Google Document AI settings
	GoogleProjectID   string
	GoogleLocation    string
	GoogleProcessorID string

	// LLM settings (from existing config)
	VisionLLMProvider string
	VisionLLMModel    string
	VisionLLMPrompt   string

	// Azure Document Intelligence settings
	AzureEndpoint            string
	AzureAPIKey              string
	AzureModelID             string // Optional, defaults to "prebuilt-read"
	AzureTimeout             int    // Optional, defaults to 120 seconds
	AzureOutputContentFormat string // Optional, defaults to ""

	// Docling settings
	DoclingURL             string
	DoclingImageExportMode string

	// OCR output options
	EnableHOCR     bool   // Whether to generate hOCR data if supported by the provider
	HOCROutputPath string // Where to save hOCR output files
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

	case "azure":
		if config.AzureEndpoint == "" || config.AzureAPIKey == "" {
			return nil, fmt.Errorf("missing required Azure Document Intelligence configuration")
		}
		return newAzureProvider(config)

	case "docling":
		if config.DoclingURL == "" {
			return nil, fmt.Errorf("missing required Docling configuration (DOCLING_URL)")
		}
		log.WithField("url", config.DoclingURL).Info("Using Docling provider")
		return newDoclingProvider(config)

	case "mistral_ocr":
		if config.MistralAPIKey == "" {
			return nil, fmt.Errorf("missing required Mistral API key")
		}
		log.WithFields(logrus.Fields{
			"model": config.MistralModel,
		}).Info("Using Mistral OCR provider")
		return newMistralOCRProvider(config)

	default:
		return nil, fmt.Errorf("unsupported OCR provider: %s", config.Provider)
	}
}

// SetLogLevel sets the logging level for the OCR package
func SetLogLevel(level logrus.Level) {
	log.SetLevel(level)
}
