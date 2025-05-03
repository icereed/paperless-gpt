package ocr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setupTestServer() (*httptest.Server, func()) {
	origOCREndpoint := mistralOCREndpoint
	origFilesEndpoint := mistralFilesEndpoint

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ocr" {
			handleOCRRequest(w, r)
		} else if r.URL.Path == "/v1/files" {
			handleFileUploadRequest(w, r)
		} else if r.URL.Path == "/v1/files/test-file-id/url" {
			handleGetSignedURLRequest(w, r)
		}
	}))

	mistralOCREndpoint = server.URL + "/v1/ocr"
	mistralFilesEndpoint = server.URL + "/v1/files"

	return server, func() {
		server.Close()
		mistralOCREndpoint = origOCREndpoint
		mistralFilesEndpoint = origFilesEndpoint
	}
}

func handleOCRRequest(w http.ResponseWriter, r *http.Request) {
	resp := MistralOCRResponse{
		Text: "Test OCR output",
		Metadata: struct {
			Pages int "json:\"pages\""
		}{
			Pages: 1,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

func handleFileUploadRequest(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	if r.FormValue("purpose") != "ocr" {
		http.Error(w, "missing or invalid purpose", http.StatusBadRequest)
		return
	}

	resp := MistralFileUploadResponse{
		ID:       "test-file-id",
		Object:   "file",
		Filename: "document.pdf",
		Purpose:  "ocr",
	}
	json.NewEncoder(w).Encode(resp)
}

func handleGetSignedURLRequest(w http.ResponseWriter, r *http.Request) {
	resp := struct {
		URL string `json:"url"`
	}{
		URL: "https://signed-url-for-file",
	}
	json.NewEncoder(w).Encode(resp)
}

func TestNewMistralOCRProvider(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			config: Config{
				MistralAPIKey: "test-key",
			},
			wantErr: false,
		},
		{
			name: "valid config with custom model",
			config: Config{
				MistralAPIKey: "test-key",
				MistralModel:  "custom-model",
			},
			wantErr: false,
		},
		{
			name:        "missing API key",
			config:      Config{},
			wantErr:     true,
			errContains: "missing required Mistral API key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := newMistralOCRProvider(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				mistralProvider := provider.(*MistralOCRProvider)
				assert.Equal(t, tt.config.MistralAPIKey, mistralProvider.apiKey)
				if tt.config.MistralModel != "" {
					assert.Equal(t, tt.config.MistralModel, mistralProvider.model)
				} else {
					assert.Equal(t, "mistral-ocr-latest", mistralProvider.model)
				}
			}
		})
	}
}

func TestMistralOCRProvider_ProcessImage(t *testing.T) {
	_, cleanup := setupTestServer()
	defer cleanup()

	// Create provider with mocked API endpoint
	provider := &MistralOCRProvider{
		apiKey: "test-key",
		model:  "mistral-ocr-latest",
	}

	// Test image processing
	testImage := []byte("test image data")
	result, err := provider.ProcessImage(context.Background(), testImage, 1)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Test OCR output", result.Text)
	assert.Equal(t, "mistral_ocr", result.Metadata["provider"])
	assert.Equal(t, "mistral-ocr-latest", result.Metadata["model"])
}

func TestMistralOCRProvider_UploadFile(t *testing.T) {
	_, cleanup := setupTestServer()
	defer cleanup()

	// Create provider with mocked API endpoint
	provider := &MistralOCRProvider{
		apiKey: "test-key",
		model:  "mistral-ocr-latest",
	}

	// Test file upload
	testPDF := []byte("test pdf data")
	fileID, err := provider.uploadFile(testPDF)

	assert.NoError(t, err)
	assert.Equal(t, "test-file-id", fileID)
}

func TestMistralOCRProvider_GetSignedURL(t *testing.T) {
	_, cleanup := setupTestServer()
	defer cleanup()

	// Create provider with mocked API endpoint
	provider := &MistralOCRProvider{
		apiKey: "test-key",
		model:  "mistral-ocr-latest",
	}

	// Test getting signed URL
	url, err := provider.getSignedURL("test-file-id")

	assert.NoError(t, err)
	assert.Equal(t, "https://signed-url-for-file", url)
}

func TestMistralOCRProvider_ProcessDocument(t *testing.T) {
	_, cleanup := setupTestServer()
	defer cleanup()

	// Create provider with mocked API endpoint
	provider := &MistralOCRProvider{
		apiKey: "test-key",
		model:  "mistral-ocr-latest",
	}

	req := MistralOCRRequest{
		Model: provider.model,
	}
	req.Document.Type = "document_url"
	req.Document.DocumentURL = "https://test-document-url"

	text, err := provider.processDocument(req)

	assert.NoError(t, err)
	assert.Equal(t, "Test OCR output", text)
}

func TestMistralOCRProvider_ErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		response    string
		wantErr     bool
		errContains string
	}{
		{
			name:        "unauthorized",
			statusCode:  401,
			response:    `{"error": "Invalid API key"}`,
			wantErr:     true,
			errContains: "OCR request failed with status: 401",
		},
		{
			name:        "bad request",
			statusCode:  400,
			response:    `{"error": "Invalid request"}`,
			wantErr:     true,
			errContains: "OCR request failed with status: 400",
		},
		{
			name:       "successful response",
			statusCode: 200,
			response:   `{"text": "Test output", "metadata": {"pages": 1}}`,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				fmt.Fprintln(w, tt.response)
			}))
			defer server.Close()

			provider := &MistralOCRProvider{
				apiKey: "test-key",
				model:  "mistral-ocr-latest",
			}
			mistralOCREndpoint = server.URL + "/v1/ocr"

			req := MistralOCRRequest{
				Model: provider.model,
			}
			req.Document.Type = "document_url"
			req.Document.DocumentURL = "https://test-document-url"

			text, err := provider.processDocument(req)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Empty(t, text)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, text)
			}
		})
	}
}
