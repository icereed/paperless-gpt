package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// ProcessDocumentOCR processes a document through OCR and returns the combined text
func (app *App) ProcessDocumentOCR(ctx context.Context, documentID int) (string, error) {
	docLogger := documentLogger(documentID)
	docLogger.Info("Starting OCR processing")

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

		result, err := app.ocrProvider.ProcessImage(ctx, imageContent)
		if err != nil {
			return "", fmt.Errorf("error performing OCR for document %d, page %d: %w", documentID, i+1, err)
		}
		if result == nil {
			pageLogger.Error("Got nil result from OCR provider")
			return "", fmt.Errorf("error performing OCR for document %d, page %d: nil result", documentID, i+1)
		}

		pageLogger.WithField("has_hocr", result.HOCR != "").
			WithField("metadata", result.Metadata).
			Debug("OCR completed for page")

		ocrTexts = append(ocrTexts, result.Text)
	}

	docLogger.Info("OCR processing completed successfully")
	return strings.Join(ocrTexts, "\n\n"), nil
}
