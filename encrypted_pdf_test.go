package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/gardar/ocrchestra/pkg/pdfocr"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"paperless-gpt/internal/pdfrender"
)

// encryptPDF returns pdf encrypted with the given passwords.
func encryptPDF(t *testing.T, pdf []byte, conf *model.Configuration) []byte {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, api.Encrypt(bytes.NewReader(pdf), &out, conf))
	return out.Bytes()
}

// PDFs restricted with an owner password but an empty user password (common
// for bank statements and invoices) open without a password in any viewer.
// Before the PDFium switch in v0.29 they failed OCR with fitz's "needs
// password" error (#1173). Every path that opens a PDF must accept them.
func TestOwnerPasswordPDFsWithEmptyUserPassword(t *testing.T) {
	plain, err := os.ReadFile("tests/pdf/five-pager.pdf")
	require.NoError(t, err)

	ciphers := map[string]*model.Configuration{
		"AES-256": model.NewAESConfiguration("", "owner-secret", 256),
		"AES-128": model.NewAESConfiguration("", "owner-secret", 128),
		"RC4-128": model.NewRC4Configuration("", "owner-secret", 128),
	}
	for name, conf := range ciphers {
		t.Run(name, func(t *testing.T) {
			encrypted := encryptPDF(t, plain, conf)
			require.False(t, bytes.Equal(plain, encrypted))

			// Image mode, page routing and search render through PDFium.
			doc, err := pdfrender.Open(context.Background(), encrypted)
			require.NoError(t, err, "PDFium opens it")
			assert.Equal(t, 5, doc.NumPages())
			img, err := doc.RenderDPI(0, 72)
			require.NoError(t, err)
			assert.Positive(t, img.Bounds().Dx())
			analysis, err := doc.AnalyzePage(0)
			require.NoError(t, err)
			assert.NotEmpty(t, analysis.Text, "its text layer is readable too")
			require.NoError(t, doc.Close())

			// The download paths: image mode and PDF mode (split, and
			// trimmed to a page limit).
			env := newTestEnv(t)
			defer env.teardown()
			env.client.CacheFolder = t.TempDir()
			env.setMockResponse("/api/documents/7/download/", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(encrypted)
			})

			images, total, err := env.client.DownloadDocumentAsImages(context.Background(), 7, 0)
			require.NoError(t, err, "image mode")
			assert.Equal(t, 5, total)
			assert.Len(t, images, 5)

			pages, _, total, err := env.client.DownloadDocumentAsPDF(context.Background(), 7, 0, true)
			require.NoError(t, err, "PDF mode, split")
			assert.Equal(t, 5, total)
			assert.Len(t, pages, 5)

			env.client.CacheFolder = t.TempDir()
			pages, _, _, err = env.client.DownloadDocumentAsPDF(context.Background(), 7, 2, true)
			require.NoError(t, err, "PDF mode, trimmed to OCR_LIMIT_PAGES")
			assert.Len(t, pages, 2)

			// PDF_SKIP_EXISTING_OCR inspects the PDF before OCR.
			_, err = pdfocr.DetectOCR(encrypted, pdfocr.DefaultConfig())
			assert.NoError(t, err, "existing-OCR detection")
		})
	}
}

// A PDF that really needs a user password still fails, with an error that
// says so, instead of being processed as an empty document.
func TestUserPasswordPDFIsRefused(t *testing.T) {
	plain, err := os.ReadFile("tests/pdf/sample.pdf")
	require.NoError(t, err)
	encrypted := encryptPDF(t, plain, model.NewAESConfiguration("user-secret", "owner-secret", 256))

	_, err = pdfrender.Open(context.Background(), encrypted)
	require.Error(t, err)
	assert.Contains(t, fmt.Sprint(err), "password")
}
