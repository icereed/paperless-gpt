package ocr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/assert"
)

// Helper function to create a mock Docling server
func setupDoclingTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	return server
}

// Helper function to create a DoclingProvider for testing
func newTestDoclingProvider(serverURL string) *DoclingProvider {
	client := retryablehttp.NewClient()
	client.RetryMax = 0 // Disable retries for testing
	client.Logger = nil // Suppress log output during tests

	return &DoclingProvider{
		baseURL:         serverURL,
		imageExportMode: "md",
		httpClient:      client,
	}
}

func TestDoclingProvider_ProcessImage(t *testing.T) {
	sampleImageContent := []byte("dummy image data")

	tests := []struct {
		name           string
		mockHandler    func(w http.ResponseWriter, r *http.Request)
		expectedResult *OCRResult
		expectedErrStr string                // Substring of the expected error message
		checkRequest   func(r *http.Request) // Optional function to validate the request received by the server
	}{
		{
			name: "Success Case",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1alpha/convert/file", r.URL.Path)
				assert.Equal(t, "POST", r.Method)
				assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
				assert.Equal(t, "application/json", r.Header.Get("Accept"))

				resp := DoclingConvertResponse{
					Status: "success",
					Document: DoclingDocumentResponse{
						TextContent: "Successfully processed text.",
						Filename:    "document.pdf",
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			expectedResult: &OCRResult{
				Text: "Successfully processed text.",
				Metadata: map[string]string{
					"provider":    "docling",
					"has_content": "true",
				},
			},
			checkRequest: func(r *http.Request) {
				err := r.ParseMultipartForm(10 << 20) // 10 MB max memory
				assert.NoError(t, err)
				assert.Equal(t, "md", r.FormValue("to_formats"))
				assert.Equal(t, "true", r.FormValue("do_ocr"))
				f, fh, err := r.FormFile("files")
				assert.NoError(t, err)
				assert.NotNil(t, f)
				assert.Equal(t, "document.pdf", fh.Filename)
				fileContent, _ := io.ReadAll(f)
				assert.Equal(t, sampleImageContent, fileContent)
				f.Close()
			},
		},
		{
			name: "Success Case - Fallback to Markdown",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := DoclingConvertResponse{
					Status: "success",
					Document: DoclingDocumentResponse{
						TextContent: "", // Empty text content
						MdContent:   "# Markdown Content",
						Filename:    "document.pdf",
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			expectedResult: &OCRResult{
				Text: "# Markdown Content",
				Metadata: map[string]string{
					"provider":    "docling",
					"has_content": "true",
				},
			},
		},
		{
			name: "Success Case - Empty Content",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := DoclingConvertResponse{
					Status: "success",
					Document: DoclingDocumentResponse{
						TextContent: "",
						MdContent:   "",
						Filename:    "document.pdf",
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			expectedResult: &OCRResult{
				Text: "",
				Metadata: map[string]string{
					"provider":    "docling",
					"has_content": "false",
				},
			},
		},
		{
			name: "Docling Processing Error",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := DoclingConvertResponse{
					Status: "failure",
					Errors: []interface{}{"Some internal docling error"},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			expectedErrStr: "docling processing failed with status 'failure'",
		},
		{
			name: "Invalid JSON Response",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("this is not json"))
			},
			expectedErrStr: "error parsing Docling JSON response",
		},
		{
			name: "Server Connection Error",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				// Handler won't be reached if server is closed
			},
			expectedErrStr: "connection refused", // Expect a connection error substring
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Wrap the handler to allow request checking
			checkedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.checkRequest != nil {
					tt.checkRequest(r)
				}
				tt.mockHandler(w, r)
			})

			server := setupDoclingTestServer(t, checkedHandler)

			serverURL := server.URL
			if tt.name == "Server Connection Error" {
				server.Close() // Intentionally close server to cause connection error
			}

			provider := newTestDoclingProvider(serverURL)

			// Only close the server if it wasn't closed for the connection error test
			if tt.name != "Server Connection Error" {
				defer server.Close()
			}

			result, err := provider.ProcessImage(context.Background(), sampleImageContent, 1)

			if tt.expectedErrStr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrStr)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedResult, result)
			}
		})
	}
}
