package ocr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// testServerState records interactions with the mock Mistral API and lets
// tests inject failures on individual endpoints (0 = respond normally).
type testServerState struct {
	deletedFileIDs  []string
	ocrStatus       int
	signedURLStatus int
	deleteStatus    int
}

func setupTestServer() (*httptest.Server, func()) {
	server, _, cleanup := setupTestServerWithState()
	return server, cleanup
}

func setupTestServerWithState() (*httptest.Server, *testServerState, func()) {
	origOCREndpoint := mistralOCREndpoint
	origFilesEndpoint := mistralFilesEndpoint

	state := &testServerState{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/ocr":
			if state.ocrStatus != 0 {
				http.Error(w, `{"error": "injected failure"}`, state.ocrStatus)
				return
			}
			handleOCRRequest(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/files":
			handleFileUploadRequest(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/files/test-file-id/url":
			if state.signedURLStatus != 0 {
				http.Error(w, `{"error": "injected failure"}`, state.signedURLStatus)
				return
			}
			handleGetSignedURLRequest(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/files/"):
			if state.deleteStatus != 0 {
				http.Error(w, `{"error": "injected failure"}`, state.deleteStatus)
				return
			}
			fileID := strings.TrimPrefix(r.URL.Path, "/v1/files/")
			state.deletedFileIDs = append(state.deletedFileIDs, fileID)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      fileID,
				"object":  "file",
				"deleted": true,
			})
		default:
			http.NotFound(w, r)
		}
	}))

	mistralOCREndpoint = server.URL + "/v1/ocr"
	mistralFilesEndpoint = server.URL + "/v1/files"

	return server, state, func() {
		server.Close()
		mistralOCREndpoint = origOCREndpoint
		mistralFilesEndpoint = origFilesEndpoint
	}
}

// minimalPDF returns bytes that mimetype.Detect identifies as application/pdf,
// so ProcessImage takes the file-upload branch.
func minimalPDF() []byte {
	return []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF")
}

func handleOCRRequest(w http.ResponseWriter, r *http.Request) {
	resp := MistralOCRResponse{
		Pages: []struct {
			Index      int           `json:"index"`
			Markdown   string        `json:"markdown"`
			Images     []interface{} `json:"images"`
			Dimensions struct {
				Dpi    int `json:"dpi"`
				Height int `json:"height"`
				Width  int `json:"width"`
			} `json:"dimensions"`
		}{
			{
				Index:    0,
				Markdown: "Test OCR output",
				Images:   []interface{}{},
				Dimensions: struct {
					Dpi    int `json:"dpi"`
					Height int `json:"height"`
					Width  int `json:"width"`
				}{
					Dpi:    300,
					Height: 1000,
					Width:  800,
				},
			},
		},
		Model: "mistral-ocr-latest",
		UsageInfo: struct {
			PagesProcessed int         `json:"pages_processed"`
			DocSizeBytes   interface{} `json:"doc_size_bytes"`
		}{
			PagesProcessed: 1,
			DocSizeBytes:   1024,
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

	logger := log.WithField("test", "process_document")
	text, err := provider.processDocument(req, logger)

	assert.NoError(t, err)
	assert.Equal(t, "Test OCR output", text)
}

func TestMistralOCRProvider_ProcessImage_PDFUploadAndCleanup(t *testing.T) {
	_, state, cleanup := setupTestServerWithState()
	defer cleanup()

	provider := &MistralOCRProvider{
		apiKey: "test-key",
		model:  "mistral-ocr-latest",
	}

	result, err := provider.ProcessImage(context.Background(), minimalPDF(), 1)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Test OCR output", result.Text)
	assert.Equal(t, "application/pdf", result.Metadata["mime_type"])
	assert.Equal(t, []string{"test-file-id"}, state.deletedFileIDs)
}

func TestMistralOCRProvider_PDFCleanupOnSignedURLFailure(t *testing.T) {
	_, state, cleanup := setupTestServerWithState()
	defer cleanup()
	state.signedURLStatus = http.StatusInternalServerError

	provider := &MistralOCRProvider{
		apiKey: "test-key",
		model:  "mistral-ocr-latest",
	}

	result, err := provider.ProcessImage(context.Background(), minimalPDF(), 1)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "signed URL")
	assert.Nil(t, result)
	// The uploaded file must be cleaned up even though OCR never ran
	assert.Equal(t, []string{"test-file-id"}, state.deletedFileIDs)
}

func TestMistralOCRProvider_PDFCleanupOnOCRFailure(t *testing.T) {
	_, state, cleanup := setupTestServerWithState()
	defer cleanup()
	state.ocrStatus = http.StatusInternalServerError

	provider := &MistralOCRProvider{
		apiKey: "test-key",
		model:  "mistral-ocr-latest",
	}

	result, err := provider.ProcessImage(context.Background(), minimalPDF(), 1)

	assert.Error(t, err)
	assert.Nil(t, result)
	// The uploaded file must be cleaned up even though the OCR call failed
	assert.Equal(t, []string{"test-file-id"}, state.deletedFileIDs)
}

func TestMistralOCRProvider_PDFResultUnaffectedByDeleteFailure(t *testing.T) {
	_, state, cleanup := setupTestServerWithState()
	defer cleanup()
	state.deleteStatus = http.StatusInternalServerError

	provider := &MistralOCRProvider{
		apiKey: "test-key",
		model:  "mistral-ocr-latest",
	}

	result, err := provider.ProcessImage(context.Background(), minimalPDF(), 1)

	// Cleanup is best-effort: a failed delete must never affect the result
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Test OCR output", result.Text)
	assert.Empty(t, state.deletedFileIDs)
}

func TestMistralOCRProvider_DeleteFile(t *testing.T) {
	_, state, cleanup := setupTestServerWithState()
	defer cleanup()

	provider := &MistralOCRProvider{
		apiKey: "test-key",
		model:  "mistral-ocr-latest",
	}

	err := provider.deleteFile("test-file-id")

	assert.NoError(t, err)
	assert.Equal(t, []string{"test-file-id"}, state.deletedFileIDs)
}

func TestMistralOCRProvider_DeleteFile_Error(t *testing.T) {
	_, state, cleanup := setupTestServerWithState()
	defer cleanup()
	state.deleteStatus = http.StatusNotFound

	provider := &MistralOCRProvider{
		apiKey: "test-key",
		model:  "mistral-ocr-latest",
	}

	err := provider.deleteFile("test-file-id")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file deletion failed with status: 404")
	assert.Empty(t, state.deletedFileIDs)
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
			response:   `{"pages":[{"index":0,"markdown":"Test OCR output","images":[],"dimensions":{"dpi":300,"height":1000,"width":800}}],"model":"mistral-ocr-latest","usage_info":{"pages_processed":1,"doc_size_bytes":1024}}`,
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

			logger := log.WithField("test", "error_handling")
			text, err := provider.processDocument(req, logger)

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
