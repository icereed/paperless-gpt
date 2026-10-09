package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"paperless-gpt/internal/pdfrender"

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
	// Re-encoded as JPEG: the LLM OCR provider labels page images
	// image/jpeg, and providers that validate the media type reject a PNG
	// under that label. Dimensions must survive.
	assertJPEGOfSize(t, content, 1, 1)
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
	assertJPEGOfSize(t, data, 1, 1)
	assert.Equal(t, 1, totalPages)
}

func TestDownloadDocumentAsPDF_WholePDFUsesDecryptedBytes(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	documentID := 996
	path := encryptedFixture(t, "required-user-password", "owner-password")
	encrypted, err := os.ReadFile(path)
	require.NoError(t, err)
	originalHash := sha256.Sum256(encrypted)
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/download/", documentID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(encrypted)
	})

	ctx := context.WithValue(context.Background(), pdfPasswordContextKey{}, "required-user-password")
	paths, decrypted, pages, err := env.client.DownloadDocumentAsPDF(ctx, documentID, 0, false)
	require.NoError(t, err)
	assert.Empty(t, paths)
	assert.Positive(t, pages)
	assert.NotEqual(t, encrypted, decrypted, "whole-PDF OCR must receive decrypted bytes")

	decryptedDoc, err := pdfrender.Open(context.Background(), decrypted)
	require.NoError(t, err)
	assert.Positive(t, decryptedDoc.NumPages())
	assert.NoError(t, decryptedDoc.Close())

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, originalHash, sha256.Sum256(after), "the encrypted source must remain unchanged")
}

func TestDownloadDocumentAsPDF_SplitUsesDecryptedSource(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	documentID := 997
	encryptedPath := filepath.Join(t.TempDir(), "encrypted.pdf")
	require.NoError(t, api.EncryptFile("tests/pdf/five-pager.pdf", encryptedPath, model.NewAESConfiguration("required-user-password", "owner-password", 256)))
	encrypted, err := os.ReadFile(encryptedPath)
	require.NoError(t, err)
	originalHash := sha256.Sum256(encrypted)
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/download/", documentID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(encrypted)
	})

	env.client.CacheFolder = filepath.Join(t.TempDir(), "cache")
	ctx := context.WithValue(context.Background(), pdfPasswordContextKey{}, "required-user-password")
	paths, decrypted, pages, err := env.client.DownloadDocumentAsPDF(ctx, documentID, 2, true)
	require.NoError(t, err)
	assert.Len(t, paths, 2)
	assert.Equal(t, 5, pages)
	assert.NotEqual(t, encrypted, decrypted)
	for _, p := range paths {
		doc, err := pdfrender.Open(context.Background(), mustReadFile(t, p))
		require.NoError(t, err)
		assert.Equal(t, 1, doc.NumPages())
		assert.NoError(t, doc.Close())
	}
	after, err := os.ReadFile(encryptedPath)
	require.NoError(t, err)
	assert.Equal(t, originalHash, sha256.Sum256(after))
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}

// assertJPEGOfSize checks that data is a JPEG with the given dimensions.
func assertJPEGOfSize(t *testing.T, data []byte, width, height int) {
	t.Helper()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, "jpeg", format)
	assert.Equal(t, width, cfg.Width)
	assert.Equal(t, height, cfg.Height)
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
