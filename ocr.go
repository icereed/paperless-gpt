package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// ProcessDocumentOCR processes a document through OCR and returns the combined text
func (app *App) ProcessDocumentOCR(ctx context.Context, documentID int) (string, error) {
	imagePaths, err := app.Client.DownloadDocumentAsImages(ctx, documentID)
	defer func() {
		for _, imagePath := range imagePaths {
			os.Remove(imagePath)
		}
	}()
	if err != nil {
		return "", fmt.Errorf("error downloading document images: %w", err)
	}

	var ocrTexts []string
	for _, imagePath := range imagePaths {
		imageContent, err := os.ReadFile(imagePath)
		if err != nil {
			return "", fmt.Errorf("error reading image file: %w", err)
		}

		ocrText, err := app.doOCRViaLLM(ctx, imageContent)
		if err != nil {
			return "", fmt.Errorf("error performing OCR: %w", err)
		}
		log.Debugf("OCR text: %s", ocrText)

		ocrTexts = append(ocrTexts, ocrText)
	}

	return strings.Join(ocrTexts, "\n\n"), nil
}
