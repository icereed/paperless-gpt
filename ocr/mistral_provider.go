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
	log.Info("Processing image with Mistral OCR provider")

	// Convert image data to base64
	base64Data := base64.StdEncoding.EncodeToString(data)
	imageURL := fmt.Sprintf("data:image/jpeg;base64,%s", base64Data)

	req := MistralOCRRequest{
		Model: p.model,
	}
	req.Document.Type = "image_url"
	req.Document.ImageURL = imageURL

	text, err := p.processDocument(req)
	if err != nil {
		return nil, err
	}

	return &OCRResult{
		Text: text,
		Metadata: map[string]string{
			"provider": "mistral_ocr",
			"model":    p.model,
		},
	}, nil
}

// uploadFile uploads a file to Mistral's files API
func (p *MistralOCRProvider) uploadFile(data []byte) (string, error) {
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

	client := &http.Client{Timeout: time.Second * 30}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("file upload failed with status: %d", resp.StatusCode)
	}

	var uploadResp MistralFileUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return "", err
	}

	return uploadResp.ID, nil
}

// getSignedURL gets a signed URL for an uploaded file
func (p *MistralOCRProvider) getSignedURL(fileID string) (string, error) {
	url := fmt.Sprintf("%s/%s/url?expiry=24", mistralFilesEndpoint, fileID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: time.Second * 10}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get signed URL with status: %d", resp.StatusCode)
	}

	var signedURLResp struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&signedURLResp); err != nil {
		return "", err
	}

	return signedURLResp.URL, nil
}

// processDocument sends the OCR request to Mistral's API
func (p *MistralOCRProvider) processDocument(req MistralOCRRequest) (string, error) {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequest("POST", mistralOCREndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	client := &http.Client{Timeout: time.Second * 30}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OCR request failed with status: %d", resp.StatusCode)
	}

	var ocrResp MistralOCRResponse
	if err := json.NewDecoder(resp.Body).Decode(&ocrResp); err != nil {
		return "", err
	}

	// Combine text from all pages
	var combinedText string
	for _, page := range ocrResp.Pages {
		combinedText += page.Markdown + "\n"
	}
	// Remove trailing newline
	if len(combinedText) > 0 {
		combinedText = combinedText[:len(combinedText)-1]
	}

	return combinedText, nil
}
