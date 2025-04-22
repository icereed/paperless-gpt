package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gardar/ocrchestra/pkg/hocr"
)

// ProcessedDocument represents a document after OCR processing
type ProcessedDocument struct {
	ID         int
	Text       string
	HOCRStruct *hocr.HOCR
	HOCRHTML   string
}

// HOCRCapable defines an interface for OCR providers that can generate hOCR
type HOCRCapable interface {
	// IsHOCREnabled returns whether hOCR generation is enabled
	IsHOCREnabled() bool

	// GetHOCRPages returns all hOCR pages collected during processing
	GetHOCRPages() []hocr.Page

	// GetHOCRDocument returns the complete hOCR document structure
	GetHOCRDocument() (*hocr.HOCR, error)

	// ResetHOCR clears any stored hOCR data
	ResetHOCR()
}

// ProcessDocumentOCR processes a document through OCR and returns the combined text and hOCR
func (app *App) ProcessDocumentOCR(ctx context.Context, documentID int) (string, error) {
	docLogger := documentLogger(documentID)
	docLogger.Info("Starting OCR processing")

	// Check if we have an hOCR-capable provider
	var hocrCapable HOCRCapable
	var hasHOCR bool

	hocrCapable, hasHOCR = app.ocrProvider.(HOCRCapable)

	// Reset hOCR if the provider supports it and it's enabled
	if hasHOCR && hocrCapable.IsHOCREnabled() {
		hocrCapable.ResetHOCR()
	} else {
		hasHOCR = false // Not hOCR capable or not enabled
	}

	imagePaths, err := app.Client.DownloadDocumentAsImages(ctx, documentID, limitOcrPages)
	defer func() {
		for _, imagePath := range imagePaths {
			if err := os.Remove(imagePath); err != nil {
				docLogger.WithError(err).WithField("image_path", imagePath).Warn("Failed to remove temporary image file")
			}
		}
	}()
	if err != nil {
		return "", fmt.Errorf("error downloading document images for document %d: %w", documentID, err)
	}

	docLogger.WithField("page_count", len(imagePaths)).Debug("Downloaded document images")

	var ocrTexts []string

	for i, imagePath := range imagePaths {
		pageLogger := docLogger.WithField("page", i+1)
		pageLogger.Debug("Processing page")

		imageContent, err := os.ReadFile(imagePath)
		if err != nil {
			return "", fmt.Errorf("error reading image file for document %d, page %d: %w", documentID, i+1, err)
		}

		// Pass the page number (1-based index) to ProcessImage
		result, err := app.ocrProvider.ProcessImage(ctx, imageContent, i+1)
		if err != nil {
			return "", fmt.Errorf("error performing OCR for document %d, page %d: %w", documentID, i+1, err)
		}
		if result == nil {
			pageLogger.Error("Got nil result from OCR provider")
			return "", fmt.Errorf("error performing OCR for document %d, page %d: nil result", documentID, i+1)
		}

		pageLogger.WithField("has_hocr_page", result.HOCRPage != nil).
			WithField("metadata", result.Metadata).
			Debug("OCR completed for page")

		ocrTexts = append(ocrTexts, result.Text)
	}

	// Generate complete hOCR if we have hOCR capability and it's enabled
	if hasHOCR && hocrCapable.IsHOCREnabled() {
		hocrDoc, err := hocrCapable.GetHOCRDocument()
		if err == nil && hocrDoc != nil {
			// Generate the HTML from the complete document
			hocrHTML, err := hocr.GenerateHOCRDocument(hocrDoc)
			if err == nil {
				docLogger.WithField("page_count", len(hocrCapable.GetHOCRPages())).
					Info("Successfully generated hOCR document")

				// Save the HOCR to a file
				if err := app.saveHOCRToFile(documentID, hocrHTML); err != nil {
					docLogger.WithError(err).Error("Failed to save HOCR file")
				} else {
					docLogger.Info("Successfully saved HOCR file")
				}
			} else {
				docLogger.WithError(err).Error("Failed to generate hOCR")
			}
		} else if err != nil {
			docLogger.WithError(err).Error("Failed to create hOCR document")
		}
	}

	fullText := strings.Join(ocrTexts, "\n\n")
	docLogger.Info("OCR processing completed successfully")
	return fullText, nil
}

// saveHOCRToFile saves the hOCR HTML to a file
// TODO: Implement a proper solution to store this alongside the document in Paperless
func (app *App) saveHOCRToFile(documentID int, hocrHTML string) error {
	// Ensure the directory exists
	if err := os.MkdirAll(app.hocrOutputPath, 0755); err != nil {
		return fmt.Errorf("failed to create HOCR output directory: %w", err)
	}

	// Create the file path
	filePath := filepath.Join(app.hocrOutputPath, fmt.Sprintf("doc_%d.hocr", documentID))

	// Write the HOCR to the file
	if err := os.WriteFile(filePath, []byte(hocrHTML), 0644); err != nil {
		return fmt.Errorf("failed to write HOCR file: %w", err)
	}

	return nil
}
