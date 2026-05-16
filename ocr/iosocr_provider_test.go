package ocr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setupIosOcrTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func newTestIosOcrProvider(serverURL string) *IosOcrProvider {
	return &IosOcrProvider{
		serverURL:  serverURL,
		httpClient: &http.Client{},
	}
}

func TestIosOcrProvider_ProcessImage(t *testing.T) {
	sampleImageContent := []byte("dummy image data")

	tests := []struct {
		name           string
		mockHandler    func(w http.ResponseWriter, r *http.Request)
		expectedResult *OCRResult
		expectedErrStr string
		checkRequest   func(r *http.Request)
	}{
		{
			name: "Success Case",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/upload", r.URL.Path)
				assert.Equal(t, "POST", r.Method)
				assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
				assert.Equal(t, "application/json", r.Header.Get("Accept"))

				resp := IosOcrUploadResponse{
					Success:     true,
					Message:     "File uploaded successfully",
					OcrResult:   "Hello\nWorld",
					ImageWidth:  1247,
					ImageHeight: 648,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			expectedResult: &OCRResult{
				Text: "Hello\nWorld",
				Metadata: map[string]string{
					"provider":     "ios_ocr",
					"has_content":  "true",
					"image_width":  "1247",
					"image_height": "648",
				},
			},
			checkRequest: func(r *http.Request) {
				err := r.ParseMultipartForm(10 << 20)
				assert.NoError(t, err)
				f, fh, err := r.FormFile("file")
				assert.NoError(t, err)
				assert.NotNil(t, f)
				assert.Equal(t, "document.png", fh.Filename)
				fileContent, _ := io.ReadAll(f)
				assert.Equal(t, sampleImageContent, fileContent)
				f.Close()
			},
		},
		{
			name: "Success Case - Empty OCR Result",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := IosOcrUploadResponse{
					Success:   true,
					Message:   "File uploaded successfully",
					OcrResult: "",
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			expectedResult: &OCRResult{
				Text: "",
				Metadata: map[string]string{
					"provider":     "ios_ocr",
					"has_content":  "false",
					"image_width":  "0",
					"image_height": "0",
				},
			},
		},
		{
			name: "Server Returns Failure",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := IosOcrUploadResponse{
					Success: false,
					Message: "Error processing image",
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			expectedErrStr: "iOS OCR Server processing failed: Error processing image",
		},
		{
			name: "Non-OK HTTP Status",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Internal Server Error"))
			},
			expectedErrStr: "iOS OCR Server returned status 500",
		},
		{
			name: "Invalid JSON Response",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("this is not json"))
			},
			expectedErrStr: "error parsing iOS OCR JSON response",
		},
		{
			name: "Server Connection Error",
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
			},
			expectedErrStr: "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.checkRequest != nil {
					tt.checkRequest(r)
				}
				tt.mockHandler(w, r)
			})

			server := setupIosOcrTestServer(t, checkedHandler)

			serverURL := server.URL
			if tt.name == "Server Connection Error" {
				server.Close()
			}

			provider := newTestIosOcrProvider(serverURL)

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
