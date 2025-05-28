package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/sirupsen/logrus"
)

var (
	mistralOCREndpoint   = "https://api.mistral.ai/v1/ocr"
	mistralFilesEndpoint = "https://api.mistral.ai/v1/files"
)

// MistralOCRProvider implements the OCR Provider interface using Mistral's OCR API
type MistralOCRProvider struct {
	apiKey string
	model  string
}

// MistralOCRRequest represents the request body for the Mistral OCR API
type MistralOCRRequest struct {
	Model    string `json:"model"`
	Document struct {
		Type        string `json:"type"`
		DocumentURL string `json:"document_url,omitempty"`
		ImageURL    string `json:"image_url,omitempty"`
	} `json:"document"`
	IncludeImageBase64 bool `json:"include_image_base64,omitempty"`
}

// MistralOCRResponse represents the response from Mistral's OCR API
type MistralOCRResponse struct {
	Pages []struct {
		Index      int           `json:"index"`
		Markdown   string        `json:"markdown"`
		Images     []interface{} `json:"images"`
		Dimensions struct {
			Dpi    int `json:"dpi"`
			Height int `json:"height"`
			Width  int `json:"width"`
		} `json:"dimensions"`
	} `json:"pages"`
	Model     string `json:"model"`
	UsageInfo struct {
		PagesProcessed int         `json:"pages_processed"`
		DocSizeBytes   interface{} `json:"doc_size_bytes"`
	} `json:"usage_info"`
}

// MistralFileUploadResponse represents the response from Mistral's file upload API
type MistralFileUploadResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt int64  `json:"created_at"`
}

// NewMistralOCRProvider creates a new instance of the Mistral OCR provider
func newMistralOCRProvider(config Config) (Provider, error) {
	if config.MistralAPIKey == "" {
		return nil, fmt.Errorf("missing required Mistral API key")
	}
	return &MistralOCRProvider{
		apiKey: config.MistralAPIKey,
		model: func() string {
			if config.MistralModel == "" {
				return "mistral-ocr-latest" // Default model
			}
			return config.MistralModel
		}(),
	}, nil
}

// ProcessImage implements the OCR Provider interface
func (p *MistralOCRProvider) ProcessImage(ctx context.Context, data []byte, pageNumber int) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"page_number": pageNumber,
		"data_size":   len(data),
		"provider":    "mistral_ocr",
		"model":       p.model,
	})
	
	logger.Info("Processing image with Mistral OCR provider")

	// Detect the actual MIME type of the data
	mtype := mimetype.Detect(data)
	logger.WithField("detected_mime_type", mtype.String()).Debug("Detected content type")

	var req MistralOCRRequest
	req.Model = p.model

	// Handle different content types appropriately
	if mtype.String() == "application/pdf" {
		logger.Debug("Processing PDF content via file upload method")
		// For PDF content, we need to upload the file first and use document_url
		fileID, err := p.uploadFile(data)
		if err != nil {
			logger.WithError(err).Error("Failed to upload PDF file")
			return nil, fmt.Errorf("failed to upload PDF file: %w", err)
		}

		// Get signed URL for the uploaded file
		signedURL, err := p.getSignedURL(fileID)
		if err != nil {
			logger.WithError(err).Error("Failed to get signed URL")
			return nil, fmt.Errorf("failed to get signed URL: %w", err)
		}

		req.Document.Type = "document_url"
		req.Document.DocumentURL = signedURL
		logger.WithField("document_url", signedURL).Debug("Using document URL method")
	} else {
		logger.Debug("Processing image content via base64 method")
		// For image content, use base64 encoding
		base64Data := base64.StdEncoding.EncodeToString(data)
		
		// Use the detected MIME type for the data URL
		dataURL := fmt.Sprintf("data:%s;base64,%s", mtype.String(), base64Data)
		
		req.Document.Type = "image_url"
		req.Document.ImageURL = dataURL
		logger.WithFields(logrus.Fields{
			"mime_type":        mtype.String(),
			"base64_length":    len(base64Data),
			"data_url_prefix":  dataURL[:min(50, len(dataURL))],
		}).Debug("Using image URL method")
	}

	text, err := p.processDocument(req, logger)
	if err != nil {
		return nil, err
	}

	return &OCRResult{
		Text: text,
		Metadata: map[string]string{
			"provider":   "mistral_ocr",
			"model":      p.model,
			"mime_type":  mtype.String(),
			"page":       fmt.Sprintf("%d", pageNumber),
		},
	}, nil
}

// uploadFile uploads a file to Mistral's files API
func (p *MistralOCRProvider) uploadFile(data []byte) (string, error) {
	logger := log.WithField("data_size", len(data))
	logger.Debug("Uploading file to Mistral")

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add the file
	part, err := writer.CreateFormFile("file", "document.pdf")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		return "", err
	}

	// Add purpose field
	if err := writer.WriteField("purpose", "ocr"); err != nil {
		return "", err
	}

	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", mistralFilesEndpoint, body)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	logger.WithFields(logrus.Fields{
		"url":          mistralFilesEndpoint,
		"content_type": writer.FormDataContentType(),
		"body_size":    body.Len(),
	}).Debug("Sending file upload request")

	client := &http.Client{Timeout: time.Second * 30}
	resp, err := client.Do(req)
	if err != nil {
		logger.WithError(err).Error("File upload request failed")
		return "", err
	}
	defer resp.Body.Close()

	// Read response body for debugging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.WithError(err).Error("Failed to read upload response body")
		return "", err
	}

	logger.WithFields(logrus.Fields{
		"status_code":   resp.StatusCode,
		"response_body": string(bodyBytes),
		"headers":       resp.Header,
	}).Debug("File upload response")

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logger.WithFields(logrus.Fields{
			"status_code":   resp.StatusCode,
			"response_body": string(bodyBytes),
		}).Error("File upload failed")
		return "", fmt.Errorf("file upload failed with status: %d, response: %s", resp.StatusCode, string(bodyBytes))
	}

	var uploadResp MistralFileUploadResponse
	if err := json.Unmarshal(bodyBytes, &uploadResp); err != nil {
		logger.WithError(err).Error("Failed to parse upload response")
		return "", err
	}

	logger.WithField("file_id", uploadResp.ID).Info("File uploaded successfully")
	return uploadResp.ID, nil
}

// getSignedURL gets a signed URL for an uploaded file
func (p *MistralOCRProvider) getSignedURL(fileID string) (string, error) {
	logger := log.WithField("file_id", fileID)
	logger.Debug("Getting signed URL")

	url := fmt.Sprintf("%s/%s/url?expiry=24", mistralFilesEndpoint, fileID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")

	logger.WithField("url", url).Debug("Sending signed URL request")

	client := &http.Client{Timeout: time.Second * 10}
	resp, err := client.Do(req)
	if err != nil {
		logger.WithError(err).Error("Signed URL request failed")
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.WithError(err).Error("Failed to read signed URL response")
		return "", err
	}

	logger.WithFields(logrus.Fields{
		"status_code":   resp.StatusCode,
		"response_body": string(bodyBytes),
	}).Debug("Signed URL response")

	if resp.StatusCode != http.StatusOK {
		logger.WithFields(logrus.Fields{
			"status_code":   resp.StatusCode,
			"response_body": string(bodyBytes),
		}).Error("Failed to get signed URL")
		return "", fmt.Errorf("failed to get signed URL with status: %d, response: %s", resp.StatusCode, string(bodyBytes))
	}

	var signedURLResp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(bodyBytes, &signedURLResp); err != nil {
		logger.WithError(err).Error("Failed to parse signed URL response")
		return "", err
	}

	logger.WithField("signed_url", signedURLResp.URL).Debug("Got signed URL successfully")
	return signedURLResp.URL, nil
}

// processDocument sends the OCR request to Mistral's API
func (p *MistralOCRProvider) processDocument(req MistralOCRRequest, logger *logrus.Entry) (string, error) {
	logger.Debug("Processing document with Mistral OCR API")

	jsonData, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	// Log the request (but mask sensitive data)
	reqCopy := req
	if reqCopy.Document.ImageURL != "" && len(reqCopy.Document.ImageURL) > 100 {
		reqCopy.Document.ImageURL = reqCopy.Document.ImageURL[:100] + "... [truncated]"
	}
	reqLogData, _ := json.Marshal(reqCopy)
	logger.WithField("request_body", string(reqLogData)).Debug("OCR request details")

	httpReq, err := http.NewRequest("POST", mistralOCREndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	logger.WithFields(logrus.Fields{
		"url":         mistralOCREndpoint,
		"method":      "POST",
		"body_size":   len(jsonData),
		"api_key_len": len(p.apiKey),
	}).Debug("Sending OCR request")

	client := &http.Client{Timeout: time.Second * 60} // Increased timeout for OCR processing
	resp, err := client.Do(httpReq)
	if err != nil {
		logger.WithError(err).Error("OCR request failed")
		return "", err
	}
	defer resp.Body.Close()

	// Read the full response body for debugging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.WithError(err).Error("Failed to read OCR response body")
		return "", err
	}

	logger.WithFields(logrus.Fields{
		"status_code":   resp.StatusCode,
		"status":        resp.Status,
		"headers":       resp.Header,
		"response_body": string(bodyBytes),
		"body_length":   len(bodyBytes),
	}).Debug("OCR response details")

	if resp.StatusCode != http.StatusOK {
		logger.WithFields(logrus.Fields{
			"status_code":   resp.StatusCode,
			"response_body": string(bodyBytes),
			"headers":       resp.Header,
		}).Error("OCR request failed with detailed error info")
		return "", fmt.Errorf("OCR request failed with status: %d, response: %s", resp.StatusCode, string(bodyBytes))
	}

	var ocrResp MistralOCRResponse
	if err := json.Unmarshal(bodyBytes, &ocrResp); err != nil {
		logger.WithError(err).WithField("response_body", string(bodyBytes)).Error("Failed to parse OCR response")
		return "", fmt.Errorf("failed to parse OCR response: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"pages_count":      len(ocrResp.Pages),
		"pages_processed":  ocrResp.UsageInfo.PagesProcessed,
		"model":            ocrResp.Model,
	}).Info("OCR processing completed")

	// Combine text from all pages
	var combinedText string
	for i, page := range ocrResp.Pages {
		logger.WithFields(logrus.Fields{
			"page_index":     i,
			"page_markdown":  len(page.Markdown),
			"page_dpi":       page.Dimensions.Dpi,
			"page_width":     page.Dimensions.Width,
			"page_height":    page.Dimensions.Height,
		}).Debug("Processing page content")
		
		combinedText += page.Markdown + "\n"
	}
	
	// Remove trailing newline
	if len(combinedText) > 0 {
		combinedText = combinedText[:len(combinedText)-1]
	}

	logger.WithField("combined_text_length", len(combinedText)).Info("Successfully extracted text")
	return combinedText, nil
}

// Helper function for min (Go 1.21+ has this built-in)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}