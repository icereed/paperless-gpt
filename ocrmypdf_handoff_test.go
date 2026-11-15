package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"paperless-gpt/internal/pdfrender"
	"paperless-gpt/ocr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type wholePDFResultProvider struct {
	pdf  []byte
	text string
}

func (p wholePDFResultProvider) ProcessImage(_ context.Context, _ []byte, page int) (*ocr.OCRResult, error) {
	if page != 0 {
		return nil, fmt.Errorf("expected whole PDF page marker, got %d", page)
	}
	return &ocr.OCRResult{Text: p.text, PDF: p.pdf, Metadata: map[string]string{"provider": "ocrmypdf", "engine": "paddleocr"}}, nil
}
func (wholePDFResultProvider) ProducesSearchablePDF() bool { return true }

func TestProcessDocumentOCRWholePDFReturnsAndVersionsProviderPDF(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	require.NoError(t, env.db.AutoMigrate(&OCRPageResult{}))
	shortenTaskPolling(t)

	original, err := os.ReadFile("tests/pdf/five-pager.pdf")
	require.NoError(t, err)
	const documentID = 8201
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/download/", documentID), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(original)
	})
	var uploaded []byte
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/update_version/", documentID), func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(10<<20))
		file, _, err := r.FormFile("document")
		require.NoError(t, err)
		uploaded, err = io.ReadAll(file)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`"task-pdf-output"`))
	})
	env.setMockResponse("/api/tasks/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"count":1,"results":[{"task_id":"task-pdf-output","status":"success"}]}`))
	})

	localDir := t.TempDir()
	app := &App{
		Client: env.client, Database: env.db,
		ocrProvider:    wholePDFResultProvider{pdf: original, text: "final extracted text"},
		ocrProcessMode: "whole_pdf", localPDFPath: localDir, createLocalPDF: true,
	}
	doc, err := app.ProcessDocumentOCR(context.Background(), documentID, OCROptions{
		ProcessMode: "whole_pdf", UploadPDF: true, UploadMode: PDFUploadModeVersion,
	}, "")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "final extracted text", doc.Text)
	assert.Equal(t, "versioned", doc.PDFAction)
	assert.Equal(t, sha256.Sum256(original), sha256.Sum256(doc.PDFData), "provider's full searchable PDF remains available to callers")
	assert.Equal(t, sha256.Sum256(original), sha256.Sum256(uploaded), "the provider PDF, not an empty hOCR rebuild, becomes the new version")
	localPDF, err := os.ReadFile(fmt.Sprintf("%s/%08d_paperless-gpt_ocr.pdf", localDir, documentID))
	require.NoError(t, err)
	assert.Equal(t, sha256.Sum256(original), sha256.Sum256(localPDF), "local PDF output retains provider bytes")
}

func TestProcessDocumentOCRWholePDFRejectsIncompleteProviderPDF(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	require.NoError(t, env.db.AutoMigrate(&OCRPageResult{}))
	original, err := os.ReadFile("tests/pdf/five-pager.pdf")
	require.NoError(t, err)
	incomplete, err := os.ReadFile("tests/pdf/sample.pdf")
	require.NoError(t, err)

	ctx := context.Background()
	source, err := pdfrender.Open(ctx, original)
	require.NoError(t, err)
	defer source.Close()
	processed, err := pdfrender.Open(ctx, incomplete)
	require.NoError(t, err)
	processedPages := processed.NumPages()
	require.NoError(t, processed.Close())
	require.NotEqual(t, source.NumPages(), processedPages, "fixtures must represent different page counts")

	const documentID = 8202
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/download/", documentID), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(original)
	})
	versionRequestCount := 0
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/update_version/", documentID), func(w http.ResponseWriter, _ *http.Request) {
		versionRequestCount++
		w.WriteHeader(http.StatusOK)
	})
	app := &App{Client: env.client, Database: env.db, ocrProvider: wholePDFResultProvider{pdf: incomplete, text: "incomplete"}, ocrProcessMode: "whole_pdf"}

	doc, err := app.ProcessDocumentOCR(ctx, documentID, OCROptions{ProcessMode: "whole_pdf", UploadPDF: true, UploadMode: PDFUploadModeVersion}, "")
	require.Error(t, err)
	assert.Nil(t, doc)
	assert.Zero(t, versionRequestCount, "incomplete PDF must not be uploaded")
}

func TestProcessDocumentOCRWholePDFUploadFailureIsNotSuccess(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	require.NoError(t, env.db.AutoMigrate(&OCRPageResult{}))
	original, err := os.ReadFile("tests/pdf/five-pager.pdf")
	require.NoError(t, err)
	const documentID = 8203
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/download/", documentID), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(original)
	})
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/update_version/", documentID), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	app := &App{Client: env.client, Database: env.db, ocrProvider: wholePDFResultProvider{pdf: original, text: "final text"}, ocrProcessMode: "whole_pdf"}

	doc, err := app.ProcessDocumentOCR(context.Background(), documentID, OCROptions{ProcessMode: "whole_pdf", UploadPDF: true, UploadMode: PDFUploadModeVersion}, "")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "failed", doc.PDFAction)
	assert.NotEmpty(t, doc.PDFDetail)
	assert.Equal(t, sha256.Sum256(original), sha256.Sum256(doc.PDFData), "failed upload must not discard the generated PDF returned for local inspection")
}

func TestProcessDocumentOCRWholePDFUnconfirmedVersionIsNotSuccess(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	require.NoError(t, env.db.AutoMigrate(&OCRPageResult{}))
	shortenTaskPolling(t)
	original, err := os.ReadFile("tests/pdf/five-pager.pdf")
	require.NoError(t, err)
	const documentID = 8204
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/download/", documentID), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(original)
	})
	versionRequests := 0
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/update_version/", documentID), func(w http.ResponseWriter, _ *http.Request) {
		versionRequests++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`"task-unconfirmed"`))
	})
	env.setMockResponse("/api/tasks/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"count":1,"results":[{"task_id":"task-unconfirmed","status":"started"}]}`))
	})
	app := &App{Client: env.client, Database: env.db, ocrProvider: wholePDFResultProvider{pdf: original, text: "final text"}, ocrProcessMode: "whole_pdf"}

	doc, err := app.ProcessDocumentOCR(context.Background(), documentID, OCROptions{ProcessMode: "whole_pdf", UploadPDF: true, UploadMode: PDFUploadModeVersion}, "")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "failed", doc.PDFAction)
	assert.Contains(t, doc.PDFDetail, "did not confirm")
	assert.Equal(t, 1, versionRequests, "an uncertain version import must not issue another POST during status polling")
}

func TestMutationAuditBlocksHTTPWhenIntentCannotBeWritten(t *testing.T) {
	t.Setenv("OCR_AUDIT_PATH", "")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := NewPaperlessClient(server.URL, "test-token")
	client.HTTPClient = server.Client()

	_, err := client.Do(context.Background(), http.MethodPatch, "api/documents/12/", bytes.NewBufferString(`{"content":"private"}`))
	require.Error(t, err)
	assert.Zero(t, requests, "Paperless mutation must not be sent before durable audit intent")
}

func TestMutationAuditPersistsIntentBeforeRequestAndOutcome(t *testing.T) {
	path := t.TempDir() + "/events.jsonl"
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	t.Setenv("OCR_AUDIT_PATH", path)
	var records []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
		require.Len(t, lines, 1, "only fsynced intent exists before request dispatch")
		var intent map[string]any
		require.NoError(t, json.Unmarshal(lines[0], &intent))
		assert.Equal(t, "intent", intent["phase"])
		assert.NotContains(t, string(b), "private")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := NewPaperlessClient(server.URL, "test-token")
	client.HTTPClient = server.Client()
	resp, err := client.Do(context.Background(), http.MethodPatch, "api/documents/12/", bytes.NewBufferString(`{"content":"private"}`))
	require.NoError(t, err)
	resp.Body.Close()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var record map[string]any
		require.NoError(t, json.Unmarshal(line, &record))
		records = append(records, record)
	}
	require.Len(t, records, 2)
	assert.Equal(t, "intent", records[0]["phase"])
	assert.Equal(t, "outcome", records[1]["phase"])
	assert.Equal(t, "success", records[1]["outcome"])
	assert.NotEmpty(t, records[0]["correlation_id"], "intent must identify its request before dispatch")
	assert.Equal(t, "intent_persisted", records[0]["outcome"])
	assert.Equal(t, records[0]["correlation_id"], records[1]["correlation_id"])
	assert.NotEqual(t, records[0]["event_id"], records[1]["event_id"], "each durable record needs its own event ID")
	assert.NotContains(t, string(data), "test-token")
	assert.NotContains(t, string(data), "private")
}

func TestMutationAuditNoOpRecordsWithoutHTTP(t *testing.T) {
	path := t.TempDir() + "/events.jsonl"
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	t.Setenv("OCR_AUDIT_PATH", path)
	client := NewPaperlessClient("http://127.0.0.1:1", "test-token")
	require.NoError(t, client.recordMutationNoOp("api/documents/19/", "document_update", 19, "no changed fields"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"phase":"no_op"`)
	assert.NotContains(t, string(data), "no changed fields")
}

type failedPDFDocumentProcessor struct{ calls int }

func (p *failedPDFDocumentProcessor) ProcessDocumentOCR(_ context.Context, documentID int, _ OCROptions, _ string) (*ProcessedDocument, error) {
	p.calls++
	return &ProcessedDocument{ID: documentID, Text: "recognized text", PDFAction: "failed", PDFDetail: "version import unconfirmed"}, nil
}

func TestAutoOCRLeavesPendingTagAndDoesNotResendFailedVersion(t *testing.T) {
	settingsMutex.Lock()
	oldSettings := settings
	settings = Settings{}
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = oldSettings
		settingsMutex.Unlock()
	})

	oldAutoOCRTag := autoOcrTag
	autoOcrTag = "pending-ocr"
	t.Cleanup(func() { autoOcrTag = oldAutoOCRTag })

	db := newOCRRunTestDB(t)
	client := newMockClient(&PaperlessClient{})
	document := Document{ID: 8301, Title: "scan", Tags: []string{autoOcrTag}}
	client.taggedDocuments[autoOcrTag] = []Document{document}
	processor := &failedPDFDocumentProcessor{}
	app := &App{
		Client:            client,
		Database:          db,
		docProcessor:      processor,
		pdfUpload:         true,
		pdfUploadMode:     PDFUploadModeVersion,
		pdfOCRTagging:     true,
		pdfOCRCompleteTag: "ocr-complete",
		ocrProcessMode:    "whole_pdf",
		ocrProviderLabel:  "ocrmypdf (paddleocr)",
	}

	processed, err := app.processAutoOcrTagDocuments(context.Background())
	require.Error(t, err, "unconfirmed searchable PDF upload must not complete automatic OCR")
	assert.Zero(t, processed)
	assert.Equal(t, 1, processor.calls)
	assert.False(t, client.updateDocsCalled, "failed upload must not apply OCR text or change pending/completion tags")
	assert.Equal(t, []Document{document}, client.taggedDocuments[autoOcrTag], "pending-tag document remains queued")

	processed, err = app.processAutoOcrTagDocuments(context.Background())
	require.NoError(t, err)
	assert.Zero(t, processed)
	assert.Equal(t, 1, processor.calls, "failed or unconfirmed version upload must not trigger another POST automatically")
	assert.False(t, client.updateDocsCalled)
}
