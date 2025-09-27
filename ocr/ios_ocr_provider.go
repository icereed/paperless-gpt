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

// IOSOCRProvider implements OCR using iOS-OCR-Server
type IOSOCRProvider struct {
	baseURL    string
	httpClient *retryablehttp.Client
}

// newIOSOCRProvider creates a new iOS-OCR-Server provider
func newIOSOCRProvider(config Config) (*IOSOCRProvider, error) {
	logger := log.WithFields(logrus.Fields{
		"url": config.IOSOCRServerURL,
	})
	logger.Info("Creating new iOS-OCR-Server provider")

	if config.IOSOCRServerURL == "" {
		logger.Error("Missing required iOS-OCR-Server URL")
		return nil, fmt.Errorf("missing required iOS-OCR-Server URL")
	}

	client := retryablehttp.NewClient()
	client.RetryMax = 3
	client.RetryWaitMin = 1 * time.Second
	client.RetryWaitMax = 10 * time.Second
	client.Logger = logger // Use the logger from the ocr package

	provider := &IOSOCRProvider{
		baseURL:    config.IOSOCRServerURL,
		httpClient: client,
	}

	logger.Info("Successfully initialized iOS-OCR-Server provider")
	return provider, nil
}

// ProcessImage sends the image content to the iOS-OCR-Server for OCR
func (p *IOSOCRProvider) ProcessImage(ctx context.Context, imageContent []byte, pageNumber int) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"provider":    "ios_ocr",
		"url":         p.baseURL,
		"page_number": pageNumber,
		"data_size":   len(imageContent),
	})
	logger.Debug("Starting iOS-OCR-Server processing")

	// Prepare multipart request body
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Add image file part
	part, err := writer.CreateFormFile("file", "document.png")
	if err != nil {
		logger.WithError(err).Error("Failed to create form file")
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = io.Copy(part, bytes.NewReader(imageContent))
	if err != nil {
		logger.WithError(err).Error("Failed to copy image content to form")
		return nil, fmt.Errorf("failed to copy image content: %w", err)
	}

	// Close the multipart writer
	err = writer.Close()
	if err != nil {
		logger.WithError(err).Error("Failed to close multipart writer")
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create HTTP request
	endpoint := p.baseURL + "/ocr"
	req, err := retryablehttp.NewRequestWithContext(ctx, "POST", endpoint, &requestBody)
	if err != nil {
		logger.WithError(err).Error("Failed to create HTTP request")
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", writer.FormDataContentType())

	logger.Debug("Sending request to iOS-OCR-Server")
	
	// Send request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		logger.WithError(err).Error("Failed to send request to iOS-OCR-Server")
		return nil, fmt.Errorf("error sending request to iOS-OCR-Server: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.WithError(err).Error("Failed to read iOS-OCR-Server response body")
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"response":    string(respBodyBytes),
		}).Error("iOS-OCR-Server returned non-200 status")
		return nil, fmt.Errorf("iOS-OCR-Server returned status %d: %s", resp.StatusCode, string(respBodyBytes))
	}

	// Parse response
	var ocrResponse IOSOCRResponse
	err = json.Unmarshal(respBodyBytes, &ocrResponse)
	if err != nil {
		logger.WithError(err).WithField("response", string(respBodyBytes)).Error("Failed to parse iOS-OCR-Server response")
		return nil, fmt.Errorf("failed to parse iOS-OCR-Server response: %w", err)
	}

	// Check if processing was successful
	if !ocrResponse.Success {
		logger.Error("iOS-OCR-Server processing failed")
		return nil, fmt.Errorf("iOS-OCR-Server processing failed")
	}

	logger.WithFields(logrus.Fields{
		"text_length":    len(ocrResponse.OCRResult),
		"num_boxes":      len(ocrResponse.OCRBoxes),
		"image_width":    ocrResponse.ImageWidth,
		"image_height":   ocrResponse.ImageHeight,
	}).Info("Successfully processed image with iOS-OCR-Server")

	// Create OCR result
	result := &OCRResult{
		Text:     ocrResponse.OCRResult,
		Metadata: make(map[string]string),
	}

	// Add metadata
	result.Metadata["provider"] = "ios_ocr"
	result.Metadata["image_width"] = fmt.Sprintf("%d", ocrResponse.ImageWidth)
	result.Metadata["image_height"] = fmt.Sprintf("%d", ocrResponse.ImageHeight)
	result.Metadata["num_boxes"] = fmt.Sprintf("%d", len(ocrResponse.OCRBoxes))

	return result, nil
}

// IOSOCRResponse represents the response from iOS-OCR-Server
type IOSOCRResponse struct {
	Message     string      `json:"message"`
	ImageWidth  int         `json:"image_width"`
	OCRResult   string      `json:"ocr_result"`
	OCRBoxes    []IOSOCRBox `json:"ocr_boxes"`
	Success     bool        `json:"success"`
	ImageHeight int         `json:"image_height"`
}

// IOSOCRBox represents a text bounding box from iOS-OCR-Server
type IOSOCRBox struct {
	Text string  `json:"text"`
	W    float64 `json:"w"`
	X    float64 `json:"x"`
	H    float64 `json:"h"`
	Y    float64 `json:"y"`
}