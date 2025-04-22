package ocr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/assert"
)

func TestNewAzureProvider(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			config: Config{
				AzureEndpoint: "https://test.cognitiveservices.azure.com/",
				AzureAPIKey:   "test-key",
			},
			wantErr: false,
		},
		{
			name: "valid config with custom model and timeout",
			config: Config{
				AzureEndpoint: "https://test.cognitiveservices.azure.com/",
				AzureAPIKey:   "test-key",
				AzureModelID:  "custom-model",
				AzureTimeout:  60,
			},
			wantErr: false,
		},
		{
			name: "missing endpoint",
			config: Config{
				AzureAPIKey: "test-key",
			},
			wantErr:     true,
			errContains: "missing required Azure Document Intelligence configuration",
		},
		{
			name: "missing api key",
			config: Config{
				AzureEndpoint: "https://test.cognitiveservices.azure.com/",
			},
			wantErr:     true,
			errContains: "missing required Azure Document Intelligence configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := newAzureProvider(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, provider)

			// Verify default values
			if tt.config.AzureModelID == "" {
				assert.Equal(t, defaultModelID, provider.modelID)
			} else {
				assert.Equal(t, tt.config.AzureModelID, provider.modelID)
			}

			if tt.config.AzureTimeout == 0 {
				assert.Equal(t, time.Duration(defaultTimeout)*time.Second, provider.timeout)
			} else {
				assert.Equal(t, time.Duration(tt.config.AzureTimeout)*time.Second, provider.timeout)
			}
		})
	}
}

func TestAzureProvider_ProcessImage(t *testing.T) {
	// Sample success response
	now := time.Now()
	successResult := AzureDocumentResult{
		Status:              "succeeded",
		CreatedDateTime:     now,
		LastUpdatedDateTime: now,
		AnalyzeResult: AzureAnalyzeResult{
			APIVersion:      apiVersion,
			ModelID:         defaultModelID,
			StringIndexType: "utf-16",
			Content:         "Test document content",
			Pages: []AzurePage{
				{
					PageNumber: 1,
					Angle:      0.0,
					Width:      800,
					Height:     600,
					Unit:       "pixel",
					Lines: []AzureLine{
						{
							Content: "Test line",
							Polygon: []int{0, 0, 100, 0, 100, 20, 0, 20},
							Spans:   []AzureSpan{{Offset: 0, Length: 9}},
						},
					},
					Spans: []AzureSpan{{Offset: 0, Length: 9}},
				},
			},
			Paragraphs: []AzureParagraph{
				{
					Content: "Test document content",
					Spans:   []AzureSpan{{Offset: 0, Length: 19}},
					BoundingRegions: []AzureBoundingBox{
						{
							PageNumber: 1,
							Polygon:    []int{0, 0, 100, 0, 100, 20, 0, 20},
						},
					},
				},
			},
			ContentFormat: "text",
		},
	}

	tests := []struct {
		name         string
		setupServer  func() *httptest.Server
		imageContent []byte
		wantErr      bool
		errContains  string
		expectedText string
	}{
		{
			name: "successful processing",
			setupServer: func() *httptest.Server {
				mux := http.NewServeMux()
				server := httptest.NewServer(mux)

				mux.HandleFunc("/documentintelligence/documentModels/prebuilt-read:analyze", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Operation-Location", fmt.Sprintf("%s/operations/123", server.URL))
					w.WriteHeader(http.StatusAccepted)
				})

				mux.HandleFunc("/operations/123", func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(successResult)
				})

				return server
			},
			// Create minimal JPEG content with magic numbers
			imageContent: append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("JFIF test content")...),
			expectedText: "Test document content",
		},
		{
			name: "invalid mime type",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Log("Server should not be called with invalid mime type")
					w.WriteHeader(http.StatusBadRequest)
				}))
			},
			imageContent: []byte("invalid content"),
			wantErr:      true,
			errContains:  "unsupported file type",
		},
		{
			name: "submission error",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprintln(w, "Invalid request")
				}))
			},
			imageContent: []byte{0xFF, 0xD8, 0xFF, 0xE0}, // JPEG magic numbers
			wantErr:      true,
			errContains:  "unexpected status code 400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer()
			defer server.Close()

			client := retryablehttp.NewClient()
			client.HTTPClient = server.Client()
			client.Logger = log

			provider := &AzureProvider{
				endpoint:   server.URL,
				apiKey:     "test-key",
				modelID:    defaultModelID,
				timeout:    5 * time.Second,
				httpClient: client,
			}

			result, err := provider.ProcessImage(context.Background(), tt.imageContent, 1)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, tt.expectedText, result.Text)
			assert.Equal(t, "azure_docai", result.Metadata["provider"])
			assert.Equal(t, apiVersion, result.Metadata["api_version"])
			assert.Equal(t, "1", result.Metadata["page_count"])
		})
	}
}
