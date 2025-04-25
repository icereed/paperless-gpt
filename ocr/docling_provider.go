package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/sirupsen/logrus"
)

// DoclingProvider implements OCR using a Docling server
type DoclingProvider struct {
	baseURL         string
	imageExportMode string
	httpClient      *retryablehttp.Client
}

// newDoclingProvider creates a new Docling OCR provider
func newDoclingProvider(config Config) (*DoclingProvider, error) {
	logger := log.WithFields(logrus.Fields{
		"url": config.DoclingURL,
	})
	logger.Info("Creating new Docling provider")

	client := retryablehttp.NewClient()
	client.RetryMax = 3
	client.RetryWaitMin = 1 * time.Second
	client.RetryWaitMax = 10 * time.Second
	client.Logger = logger // Use the logger from the ocr package

	provider := &DoclingProvider{
		baseURL:         config.DoclingURL,
		imageExportMode: config.DoclingImageExportMode,
		httpClient:      client,
	}

	logger.Info("Successfully initialized Docling provider")
	return provider, nil
}

// ProcessImage sends the image content to the Docling server for OCR
func (p *DoclingProvider) ProcessImage(ctx context.Context, imageContent []byte, pageNumber int) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"provider": "docling",
		"url":      p.baseURL,
	})
	logger.Debug("Starting Docling processing")

	// Prepare multipart request body
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Add image file part
	// Using a generic filename as the actual name isn't critical here
	part, err := writer.CreateFormFile("files", "document.pdf")
	if err != nil {
		logger.WithError(err).Error("Failed to create form file")
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = io.Copy(part, bytes.NewReader(imageContent))
	if err != nil {
		logger.WithError(err).Error("Failed to copy image content to form")
		return nil, fmt.Errorf("failed to copy image content to form: %w", err)
	}

	// Add required form fields
	// Note: Docling expects boolean fields as strings "true"/"false"
	if err := writer.WriteField("to_formats", "md"); err != nil {
		return nil, fmt.Errorf("set to_formats: %w", err)
	}
	if err := writer.WriteField("do_ocr", "true"); err != nil {
		return nil, fmt.Errorf("set do_ocr: %w", err)
	}
	if err := writer.WriteField("pipeline", "vlm"); err != nil {
		return nil, fmt.Errorf("set pipeline: %w", err)
	}
	if err := writer.WriteField("image_export_mode", p.imageExportMode); err != nil {
		return nil, fmt.Errorf("set image_export_mode: %w", err)
	}
	// Close multipart writer
	err = writer.Close()
	if err != nil {
		logger.WithError(err).Error("Failed to close multipart writer")
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create HTTP request
	requestURL := p.baseURL + "/v1alpha/convert/file"
	req, err := retryablehttp.NewRequestWithContext(ctx, "POST", requestURL, &requestBody)
	if err != nil {
		logger.WithError(err).Error("Failed to create HTTP request")
		return nil, fmt.Errorf("error creating Docling request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json") // Ensure we get JSON back

	logger.Debug("Sending request to Docling server")
	// Add detailed logging of request parameters
	logger.WithFields(logrus.Fields{
		"to_formats":        "md",
		"do_ocr":            "true",
		"pipeline":          "vlm",
		"image_export_mode": p.imageExportMode,
	}).Debug("Docling request parameters")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		logger.WithError(err).Error("Failed to send request to Docling server")
		return nil, fmt.Errorf("error sending request to Docling: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.WithError(err).Error("Failed to read Docling response body")
		return nil, fmt.Errorf("error reading Docling response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"response":    string(respBodyBytes),
		}).Error("Received non-OK status from Docling")
		return nil, fmt.Errorf("docling API returned status %d: %s", resp.StatusCode, string(respBodyBytes))
	}

	// Parse JSON response
	var doclingResp DoclingConvertResponse
	if err := json.Unmarshal(respBodyBytes, &doclingResp); err != nil {
		logger.WithError(err).WithField("response", string(respBodyBytes)).Error("Failed to parse Docling JSON response")
		return nil, fmt.Errorf("error parsing Docling JSON response: %w", err)
	}

	// Check Docling status and errors
	if doclingResp.Status != "success" {
		logger.WithFields(logrus.Fields{
			"status": doclingResp.Status,
			"errors": doclingResp.Errors,
		}).Error("Docling processing failed")
		// Handle potential error structures if known, otherwise just report status
		return nil, fmt.Errorf("docling processing failed with status '%s', errors: %v", doclingResp.Status, doclingResp.Errors)
	}

	// Extract text content
	ocrText := doclingResp.Document.TextContent
	if ocrText == "" {
		// Fallback to Markdown content if text content is empty (less ideal but better than nothing)
		ocrText = doclingResp.Document.MdContent
		logger.Debug("Text content empty, falling back to Markdown content")
	}

	if ocrText == "" {
		logger.WithFields(logrus.Fields{
			"document":        doclingResp.Document,
			"response_status": doclingResp.Status,
		}).Warn("Received empty text and markdown content from Docling")
		// Log more details about the response to help debug
		logger.WithField("raw_response", string(respBodyBytes)).Debug("Raw Docling response")
	}

	result := &OCRResult{
		Text: ocrText,
		Metadata: map[string]string{
			"provider":    "docling",
			"has_content": fmt.Sprintf("%t", ocrText != ""),
		},
	}

	logger.WithField("content_length", len(result.Text)).Info("Successfully processed image with Docling")
	return result, nil
}

// DoclingConvertResponse mirrors the structure of the /v1alpha/convert/file JSON response
type DoclingConvertResponse struct {
	Document DoclingDocumentResponse `json:"document"`
	Status   string                  `json:"status"`
	Errors   []interface{}           `json:"errors"` // Define more specifically if needed
}

// DoclingDocumentResponse mirrors the 'document' part of the response
type DoclingDocumentResponse struct {
	Filename    string `json:"filename"`
	MdContent   string `json:"md_content"`
	TextContent string `json:"text_content"`
	// Add other fields like json_content, html_content if needed
}
