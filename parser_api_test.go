package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"paperless-gpt/ocr"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubOCR returns the same text on every call. We don't reuse mockOCRProvider
// here to keep the parser_api tests self-contained.
type stubOCR struct{ text string }

func (s *stubOCR) ProcessImage(_ context.Context, _ []byte, _ int) (*ocr.OCRResult, error) {
	return &ocr.OCRResult{Text: s.text}, nil
}

func newParserTestApp(t *testing.T) (*gin.Engine, *App) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	app := &App{ocrProvider: &stubOCR{text: "hello world"}}
	app.registerParserAPI(router)
	return router, app
}

// makeTinyPNG returns a 2×2 white PNG. Real-world images would go through the
// PDF rendering or directly to the provider; for the test the bytes only have
// to be a valid image so http.DetectContentType picks the right MIME type.
func makeTinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for x := 0; x < 2; x++ {
		for y := 0; y < 2; y++ {
			img.Set(x, y, color.White)
		}
	}
	buf := &bytes.Buffer{}
	require.NoError(t, png.Encode(buf, img))
	return buf.Bytes()
}

func multipartBody(t *testing.T, fields map[string]string, fileField, fileName string, fileBytes []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	for k, v := range fields {
		require.NoError(t, mw.WriteField(k, v))
	}
	if fileBytes != nil {
		fw, err := mw.CreateFormFile(fileField, fileName)
		require.NoError(t, err)
		_, err = io.Copy(fw, bytes.NewReader(fileBytes))
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())
	return body, mw.FormDataContentType()
}

func TestParserCapabilities(t *testing.T) {
	router, _ := newParserTestApp(t)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/capabilities", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var caps ParserCapabilities
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &caps))
	assert.Equal(t, "paperless-gpt", caps.Name)
	assert.Contains(t, caps.SupportedMimeTypes, "application/pdf")
	assert.Contains(t, caps.SupportedMimeTypes, "image/png")
	assert.Equal(t, 50, caps.DefaultScore)
}

func TestParserHealthz(t *testing.T) {
	router, _ := newParserTestApp(t)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestParserParseImage(t *testing.T) {
	router, _ := newParserTestApp(t)

	pngBytes := makeTinyPNG(t)
	body, ct := multipartBody(t, map[string]string{"mime_type": "image/png"}, "file", "tiny.png", pngBytes)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/parse", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp ParseResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "hello world", resp.Text)
	require.NotNil(t, resp.PageCount)
	assert.Equal(t, 1, *resp.PageCount)
}

func TestParserParseUnsupportedMime(t *testing.T) {
	router, _ := newParserTestApp(t)

	body, ct := multipartBody(t, map[string]string{"mime_type": "application/octet-stream"}, "file", "x.bin", []byte("garbage"))

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/parse", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnsupportedMediaType, w.Code)
}

func TestParserParseMissingFile(t *testing.T) {
	router, _ := newParserTestApp(t)

	body, ct := multipartBody(t, map[string]string{"mime_type": "image/png"}, "", "", nil)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/parse", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestParserBearerTokenEnforced(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_API_TOKEN", "secret-xyz")
	router, _ := newParserTestApp(t)

	// No header → 401
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	// Correct header → 200
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
	req.Header.Set("Authorization", "Bearer secret-xyz")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestParserParseInvalidContextJSON(t *testing.T) {
	router, _ := newParserTestApp(t)
	pngBytes := makeTinyPNG(t)
	body, ct := multipartBody(t, map[string]string{
		"mime_type": "image/png",
		"context":   "not-json",
	}, "file", "tiny.png", pngBytes)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/parse", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.True(t, strings.Contains(w.Body.String(), "context"))
}
