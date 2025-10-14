package ocr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIOSOCRProvider(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
	}{
		{
			name: "valid config",
			config: Config{
				IOSOCRServerURL: "http://localhost:8080",
			},
			expectError: false,
		},
		{
			name: "missing URL",
			config: Config{
				IOSOCRServerURL: "",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := newIOSOCRProvider(tt.config)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, tt.config.IOSOCRServerURL, provider.baseURL)
			}
		})
	}
}

func TestIOSOCRProvider_ProcessImage(t *testing.T) {
	// Mock successful response
	mockResponse := IOSOCRResponse{
		Message:     "File uploaded successfully",
		ImageWidth:  446,
		OCRResult:   "Test OCR Result\nMultiple lines of text",
		Success:     true,
		ImageHeight: 408,
		OCRBoxes: []IOSOCRBox{
			{
				Text: "Test OCR Result",
				W:    200,
				X:    10,
				H:    30,
				Y:    5,
			},
			{
				Text: "Multiple lines of text",
				W:    250,
				X:    10,
				H:    30,
				Y:    40,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/ocr", r.URL.Path)
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	provider := &IOSOCRProvider{
		baseURL:    server.URL,
		httpClient: retryablehttp.NewClient(),
	}

	// Test data
	testImageData := []byte("fake image data")
	
	result, err := provider.ProcessImage(context.Background(), testImageData, 1)
	
	require.NoError(t, err)
	assert.Equal(t, "Test OCR Result\nMultiple lines of text", result.Text)
	assert.NotNil(t, result.Metadata)
	assert.Equal(t, "ios_ocr", result.Metadata["provider"])
	assert.Equal(t, "446", result.Metadata["image_width"])
	assert.Equal(t, "408", result.Metadata["image_height"])
	assert.Equal(t, "2", result.Metadata["num_boxes"])
}

func TestIOSOCRProvider_ProcessImage_ErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectError    bool
		errorContains  string
	}{
		{
			name: "server error",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintln(w, "Internal server error")
			},
			expectError:   true,
			errorContains: "error sending request to iOS-OCR-Server",
		},
		{
			name: "invalid JSON response",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, "invalid json")
			},
			expectError:   true,
			errorContains: "failed to parse iOS-OCR-Server response",
		},
		{
			name: "processing failed",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				response := IOSOCRResponse{
					Success: false,
					Message: "Processing failed",
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(response)
			},
			expectError:   true,
			errorContains: "iOS-OCR-Server processing failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			client := retryablehttp.NewClient()
			client.RetryMax = 0 // Disable retries for error testing

			provider := &IOSOCRProvider{
				baseURL:    server.URL,
				httpClient: client,
			}

			testImageData := []byte("fake image data")
			result, err := provider.ProcessImage(context.Background(), testImageData, 1)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestIOSOCRProvider_Integration(t *testing.T) {
	// Test the full flow with NewProvider
	config := Config{
		Provider:        "ios_ocr",
		IOSOCRServerURL: "http://localhost:8080",
	}

	provider, err := NewProvider(config)
	require.NoError(t, err)
	
	// Verify it's the correct type
	iosProvider, ok := provider.(*IOSOCRProvider)
	require.True(t, ok)
	assert.Equal(t, "http://localhost:8080", iosProvider.baseURL)
}