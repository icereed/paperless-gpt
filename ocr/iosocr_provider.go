package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/sirupsen/logrus"
)

const (
	defaultIosOcrTimeout = 60
)

// IosOcrProvider implements OCR using the iOS OCR Server app
type IosOcrProvider struct {
	serverURL  string
	httpClient *retryablehttp.Client
}

// IosOcrUploadResponse mirrors the JSON response from the iOS OCR Server
type IosOcrUploadResponse struct {
	Success     bool        `json:"success"`
	Message     string      `json:"message"`
	OcrResult   string      `json:"ocr_result"`
	ImageWidth  int         `json:"image_width"`
	ImageHeight int         `json:"image_height"`
	OcrBoxes    interface{} `json:"ocr_boxes"`
}

func newIosOcrProvider(config Config) (*IosOcrProvider, error) {
	logger := log.WithFields(logrus.Fields{
		"server_url": config.IosOcrServerURL,
	})
	logger.Info("Creating new iOS OCR Server provider")

	if config.IosOcrServerURL == "" {
		return nil, fmt.Errorf("missing required iOS OCR Server URL")
	}

	timeout := defaultIosOcrTimeout
	if config.IosOcrServerTimeout > 0 {
		timeout = config.IosOcrServerTimeout
	}

	client := retryablehttp.NewClient()
	client.RetryMax = 3
	client.RetryWaitMin = 1 * time.Second
	client.RetryWaitMax = 10 * time.Second
	client.HTTPClient.Timeout = time.Duration(timeout) * time.Second
	client.Logger = logger

	// Normalize server URL: strip trailing slash for consistent URL building
	serverURL := strings.TrimRight(config.IosOcrServerURL, "/")

	provider := &IosOcrProvider{
		serverURL:  serverURL,
		httpClient: client,
	}

	logger.Info("Successfully initialized iOS OCR Server provider")
	return provider, nil
}

func (p *IosOcrProvider) ProcessImage(ctx context.Context, imageContent []byte, pageNumber int) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"provider": "ios_ocr",
		"url":      p.serverURL,
		"page":     pageNumber,
	})
	logger.Debug("Starting iOS OCR Server processing")

	uploadURL := p.serverURL + "/upload"

	// Build multipart form request
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	part, err := writer.CreateFormFile("file", "document.png")
	if err != nil {
		logger.WithError(err).Error("Failed to create form file")
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	_, err = io.Copy(part, bytes.NewReader(imageContent))
	if err != nil {
		logger.WithError(err).Error("Failed to copy image content to form")
		return nil, fmt.Errorf("failed to copy image content to form: %w", err)
	}

	err = writer.Close()
	if err != nil {
		logger.WithError(err).Error("Failed to close multipart writer")
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create HTTP request
	req, err := retryablehttp.NewRequestWithContext(ctx, "POST", uploadURL, &requestBody)
	if err != nil {
		logger.WithError(err).Error("Failed to create HTTP request")
		return nil, fmt.Errorf("error creating iOS OCR request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	logger.WithField("url", uploadURL).Debug("Sending request to iOS OCR Server")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		logger.WithError(err).Error("Failed to send request to iOS OCR Server")
		return nil, fmt.Errorf("error sending request to iOS OCR Server: %w", err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.WithError(err).Error("Failed to read response body")
		return nil, fmt.Errorf("error reading iOS OCR response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"response":    string(respBodyBytes),
		}).Error("Received non-OK status from iOS OCR Server")
		return nil, fmt.Errorf("iOS OCR Server returned status %d: %s", resp.StatusCode, string(respBodyBytes))
	}

	var ocrResp IosOcrUploadResponse
	if err := json.Unmarshal(respBodyBytes, &ocrResp); err != nil {
		logger.WithError(err).WithField("response", string(respBodyBytes)).Error("Failed to parse iOS OCR JSON response")
		return nil, fmt.Errorf("error parsing iOS OCR JSON response: %w", err)
	}

	if !ocrResp.Success {
		logger.WithFields(logrus.Fields{
			"message": ocrResp.Message,
		}).Error("iOS OCR Server returned failure")
		return nil, fmt.Errorf("iOS OCR Server processing failed: %s", ocrResp.Message)
	}

	result := &OCRResult{
		Text: ocrResp.OcrResult,
		Metadata: map[string]string{
			"provider":     "ios_ocr",
			"has_content":  fmt.Sprintf("%t", ocrResp.OcrResult != ""),
			"image_width":  fmt.Sprintf("%d", ocrResp.ImageWidth),
			"image_height": fmt.Sprintf("%d", ocrResp.ImageHeight),
		},
	}

	logger.WithFields(logrus.Fields{
		"content_length": len(result.Text),
		"image_width":    ocrResp.ImageWidth,
		"image_height":   ocrResp.ImageHeight,
	}).Info("Successfully processed image with iOS OCR Server")

	return result, nil
}
