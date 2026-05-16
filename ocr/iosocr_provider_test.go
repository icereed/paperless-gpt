package ocr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gardar/ocrchestra/pkg/hocr"
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

func newTestIosOcrProviderWithHOCR(serverURL string) *IosOcrProvider {
	return &IosOcrProvider{
		serverURL:  serverURL,
		httpClient: &http.Client{},
		enableHOCR: true,
		hocrPages:  make([]hocr.Page, 0),
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

func TestIosOcrProvider_ProcessImage_HOCR(t *testing.T) {
	sampleImageContent := []byte("dummy image data")

	tests := []struct {
		name           string
		enableHOCR     bool
		ocrBoxes       interface{}
		expectHOCRPage bool
		expectedLines  int
	}{
		{
			name:       "HOCR Enabled With Boxes",
			enableHOCR: true,
			ocrBoxes: []IosOcrBox{
				{Text: "Hello", X: 100, Y: 100, W: 200, H: 40},
				{Text: "World", X: 320, Y: 100, W: 180, H: 40},
				{Text: "Second", X: 100, Y: 200, W: 220, H: 40},
			},
			expectHOCRPage: true,
			expectedLines:  2,
		},
		{
			name:       "HOCR Enabled Empty Boxes",
			enableHOCR: true,
			ocrBoxes:   []IosOcrBox{},
			expectHOCRPage: false,
			expectedLines:  0,
		},
		{
			name:       "HOCR Enabled Nil Boxes",
			enableHOCR: true,
			ocrBoxes:   nil,
			expectHOCRPage: false,
			expectedLines:  0,
		},
		{
			name:       "HOCR Disabled With Boxes",
			enableHOCR: false,
			ocrBoxes: []IosOcrBox{
				{Text: "Hello", X: 100, Y: 100, W: 200, H: 40},
			},
			expectHOCRPage: false,
			expectedLines:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := IosOcrUploadResponse{
					Success:     true,
					Message:     "OK",
					OcrResult:   "Hello World\nSecond",
					ImageWidth:  1247,
					ImageHeight: 648,
					OcrBoxes:    tt.ocrBoxes,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			var provider *IosOcrProvider
			if tt.enableHOCR {
				provider = newTestIosOcrProviderWithHOCR(server.URL)
			} else {
				provider = newTestIosOcrProvider(server.URL)
			}

			result, err := provider.ProcessImage(context.Background(), sampleImageContent, 1)
			assert.NoError(t, err)
			assert.NotNil(t, result)

			if tt.expectHOCRPage {
				assert.NotNil(t, result.HOCRPage)
				assert.Equal(t, 1, result.HOCRPage.PageNumber)
				assert.Len(t, result.HOCRPage.Lines, tt.expectedLines)
				if len(result.HOCRPage.Lines) > 0 {
					assert.Len(t, result.HOCRPage.Lines[0].Words, 2)
					assert.Equal(t, "Hello", result.HOCRPage.Lines[0].Words[0].Text)
					assert.Equal(t, "World", result.HOCRPage.Lines[0].Words[1].Text)
				}
			} else {
				assert.Nil(t, result.HOCRPage)
			}
		})
	}
}

func TestBuildHOCRPage(t *testing.T) {
	boxes := []IosOcrBox{
		{Text: "Hello", X: 100, Y: 100, W: 200, H: 40},
		{Text: "World", X: 320, Y: 100, W: 180, H: 40},
		{Text: "Bar", X: 50, Y: 200, W: 100, H: 40},
	}

	page := buildHOCRPage(boxes, "Hello World\nBar", 1, 1247, 648)

	assert.Equal(t, "page_1", page.ID)
	assert.Equal(t, 1, page.PageNumber)
	assert.Equal(t, hocr.BoundingBox{X1: 0, Y1: 0, X2: 1247, Y2: 648}, page.BBox)
	assert.Len(t, page.Lines, 2)

	// First line: "Hello World"
	assert.Len(t, page.Lines[0].Words, 2)
	assert.Equal(t, "Hello", page.Lines[0].Words[0].Text)
	assert.Equal(t, "World", page.Lines[0].Words[1].Text)
	assert.Equal(t, hocr.BoundingBox{X1: 100, Y1: 100, X2: 500, Y2: 140}, page.Lines[0].BBox)

	// Second line: "Bar"
	assert.Len(t, page.Lines[1].Words, 1)
	assert.Equal(t, "Bar", page.Lines[1].Words[0].Text)
}

func TestParseOcrBoxes(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{
			"text": "Hello",
			"x":    float64(100),
			"y":    float64(100),
			"w":    float64(200),
			"h":    float64(40),
		},
	}

	boxes, err := parseOcrBoxes(raw)
	assert.NoError(t, err)
	assert.Len(t, boxes, 1)
	assert.Equal(t, "Hello", boxes[0].Text)
	assert.Equal(t, 100.0, boxes[0].X)

	// Test with nil
	_, err = parseOcrBoxes(nil)
	assert.Error(t, err)

	// Test with invalid data
	_, err = parseOcrBoxes("not an array")
	assert.Error(t, err)
}
