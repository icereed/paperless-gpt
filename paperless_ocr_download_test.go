package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// onePixelPNG is a minimal 1x1 PNG used to simulate a non-PDF original served
// when PAPERLESS_ARCHIVE_FILE_GENERATION=never.
var onePixelPNG = func() []byte {
	b, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		panic(err)
	}
	return b
}()

func TestIsPDFData(t *testing.T) {
	assert.True(t, isPDFData([]byte("%PDF-1.4\n rest")))
	assert.False(t, isPDFData(onePixelPNG))
	assert.False(t, isPDFData([]byte("plain text, not a pdf")))
	assert.False(t, isPDFData(nil))
	assert.False(t, isPDFData([]byte("%PD")))
}

// TestDownloadDocumentAsImages_NonPDFImage verifies that an image download
// (e.g. original served with PAPERLESS_ARCHIVE_FILE_GENERATION=never) passes
// through as a single page instead of failing in fitz.
func TestDownloadDocumentAsImages_NonPDFImage(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	documentID := 991
	downloadPath := fmt.Sprintf("/api/documents/%d/download/", documentID)
	env.setMockResponse(downloadPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(onePixelPNG)
	})

	env.client.CacheFolder = "tests/tmp"
	docDir := fmt.Sprintf("tests/tmp/document-%d", documentID)
	require.NoError(t, os.RemoveAll(docDir))
	defer os.RemoveAll(docDir)

	imagePaths, totalPages, err := env.client.DownloadDocumentAsImages(context.Background(), documentID, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, totalPages)
	require.Len(t, imagePaths, 1)
	content, err := os.ReadFile(imagePaths[0])
	require.NoError(t, err)
	assert.Equal(t, onePixelPNG, content)
}

// TestDownloadDocumentAsImages_NonPDFUnsupported verifies a descriptive error
// (with detected mime) instead of "fitz: cannot open document".
func TestDownloadDocumentAsImages_NonPDFUnsupported(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	documentID := 992
	downloadPath := fmt.Sprintf("/api/documents/%d/download/", documentID)
	env.setMockResponse(downloadPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("just some text, definitely not a pdf"))
	})

	env.client.CacheFolder = "tests/tmp"
	_, _, err := env.client.DownloadDocumentAsImages(context.Background(), documentID, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a PDF")
	assert.Contains(t, err.Error(), "text/plain")
}

// TestDownloadDocumentAsPDF_NonPDFImageSplitFalse verifies whole-document
// consumers can proceed with image bytes (providers sniff the content).
func TestDownloadDocumentAsPDF_NonPDFImageSplitFalse(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	documentID := 993
	downloadPath := fmt.Sprintf("/api/documents/%d/download/", documentID)
	env.setMockResponse(downloadPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(onePixelPNG)
	})

	env.client.CacheFolder = "tests/tmp"
	paths, data, totalPages, err := env.client.DownloadDocumentAsPDF(context.Background(), documentID, 0, false)
	require.NoError(t, err)
	assert.Empty(t, paths)
	assert.Equal(t, onePixelPNG, data)
	assert.Equal(t, 1, totalPages)
}

// TestDownloadDocumentAsPDF_NonPDFImageSplitTrue verifies per-page PDF
// splitting rejects images with guidance toward image mode.
func TestDownloadDocumentAsPDF_NonPDFImageSplitTrue(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	documentID := 994
	downloadPath := fmt.Sprintf("/api/documents/%d/download/", documentID)
	env.setMockResponse(downloadPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(onePixelPNG)
	})

	env.client.CacheFolder = "tests/tmp"
	_, _, _, err := env.client.DownloadDocumentAsPDF(context.Background(), documentID, 0, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a PDF")
	assert.Contains(t, err.Error(), "image mode")
}

// TestDownloadDocumentAsPDF_NonPDFUnsupported verifies office/text originals
// fail descriptively instead of in fitz/pdfcpu.
func TestDownloadDocumentAsPDF_NonPDFUnsupported(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	documentID := 995
	downloadPath := fmt.Sprintf("/api/documents/%d/download/", documentID)
	env.setMockResponse(downloadPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("just some text, definitely not a pdf"))
	})

	env.client.CacheFolder = "tests/tmp"
	_, _, _, err := env.client.DownloadDocumentAsPDF(context.Background(), documentID, 0, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a PDF")
}
