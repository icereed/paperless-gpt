package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"paperless-gpt/ocr"

	"github.com/gardar/ocrchestra/pkg/hocr"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadDocumentVersionSendsOnlyDocumentAndLabel(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	pdfData := []byte("%PDF-1.7 searchable")
	var gotFilename, gotLabel string
	var gotBody []byte
	var otherFields []string
	env.setMockResponse("/api/documents/42/update_version/", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Token test-token", r.Header.Get("Authorization"))
		require.NoError(t, r.ParseMultipartForm(10<<20))
		file, header, err := r.FormFile("document")
		require.NoError(t, err)
		gotFilename = header.Filename
		gotBody, _ = io.ReadAll(file)
		gotLabel = r.FormValue("version_label")
		for key := range r.MultipartForm.Value {
			if key != "version_label" {
				otherFields = append(otherFields, key)
			}
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`"task-abc"`))
	})

	taskID, err := env.client.UploadDocumentVersion(context.Background(), 42, pdfData, "00000042_paperless-gpt_ocr.pdf", "paperless-gpt OCR")
	require.NoError(t, err)
	assert.Equal(t, "task-abc", taskID)
	assert.Equal(t, "00000042_paperless-gpt_ocr.pdf", gotFilename)
	assert.Equal(t, pdfData, gotBody)
	assert.Equal(t, "paperless-gpt OCR", gotLabel)
	assert.Empty(t, otherFields, "update_version accepts only document and version_label")
}

func TestUploadDocumentVersionTruncatesLongLabel(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	var gotLabel string
	env.setMockResponse("/api/documents/7/update_version/", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(10<<20))
		gotLabel = r.FormValue("version_label")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`"task-1"`))
	})

	label := strings.Repeat("é", maxVersionLabelLength+10)
	_, err := env.client.UploadDocumentVersion(context.Background(), 7, []byte("%PDF"), "f.pdf", label)
	require.NoError(t, err)
	assert.Equal(t, maxVersionLabelLength, len([]rune(gotLabel)), "label is cut to paperless-ngx's limit on rune boundaries")
}

func TestUploadDocumentVersionNotFoundMentionsVersionSupport(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	env.setMockResponse("/api/documents/9/update_version/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"Not found."}`))
	})

	_, err := env.client.UploadDocumentVersion(context.Background(), 9, []byte("%PDF"), "f.pdf", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "3.0")
}

func TestUploadProcessedPDFVersionModeNeverCreatesOrDeletes(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	documentID := 123
	versionUploads := 0
	env.setMockResponse("/api/documents/123/update_version/", func(w http.ResponseWriter, r *http.Request) {
		versionUploads++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`"task-v1"`))
	})
	taskPolls := 0
	env.setMockResponse("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		taskPolls++
		assert.Equal(t, "task-v1", r.URL.Query().Get("task_id"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"count":1,"next":null,"previous":null,"results":[{"task_id":"task-v1","status":"success"}]}`))
	})
	env.setMockResponse("/api/documents/post_document/", func(w http.ResponseWriter, r *http.Request) {
		t.Error("version mode must not upload a new document")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`"task-new"`))
	})
	env.setMockResponse("/api/documents/123/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			t.Error("version mode must never delete the original")
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(GetDocumentApiResponse{ID: documentID, Title: "Scan"})
	})

	app := &App{
		Client:            env.client,
		Database:          env.db,
		pdfOCRTagging:     true,
		pdfOCRCompleteTag: "paperless-gpt-ocr-complete",
	}
	options := OCROptions{UploadPDF: true, UploadMode: PDFUploadModeVersion, CopyMetadata: true}

	err := app.uploadProcessedPDF(context.Background(), documentID, []byte("%PDF-1.7"), options, logrus.WithField("test", "version"))
	require.NoError(t, err)
	assert.Equal(t, 1, versionUploads)
	assert.Equal(t, 1, taskPolls, "the version import result is checked once it reports success")
}

func TestUploadProcessedPDFVersionModeReportsRejectedImport(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	shortenTaskPolling(t)

	env.setMockResponse("/api/documents/55/update_version/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`"task-bad"`))
	})
	env.setMockResponse("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"count":1,"results":[{"task_id":"task-bad","status":"failure","result_data":{"error":"Not consuming: it is a duplicate"}}]}`))
	})

	app := &App{Client: env.client, Database: env.db}
	options := OCROptions{UploadPDF: true, UploadMode: PDFUploadModeVersion}
	err := app.uploadProcessedPDF(context.Background(), 55, []byte("%PDF-1.7"), options, logrus.WithField("test", "version"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejected the new version")
	assert.Contains(t, err.Error(), "duplicate")
}

func TestUploadProcessedPDFVersionModeTreatsPendingImportAsUnconfirmed(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	shortenTaskPolling(t)

	env.setMockResponse("/api/documents/56/update_version/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`"task-slow"`))
	})
	polls := 0
	env.setMockResponse("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		polls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"count":1,"results":[{"task_id":"task-slow","status":"started"}]}`))
	})

	app := &App{Client: env.client, Database: env.db}
	options := OCROptions{UploadPDF: true, UploadMode: PDFUploadModeVersion}
	err := app.uploadProcessedPDF(context.Background(), 56, []byte("%PDF-1.7"), options, logrus.WithField("test", "version"))
	var unconfirmed *versionUnconfirmedError
	require.ErrorAs(t, err, &unconfirmed, "a still-importing version is reported as unconfirmed, not failed")
	assert.Equal(t, "task-slow", unconfirmed.taskID)
	assert.Equal(t, taskPollAttempts, polls)
}

func TestUploadProcessedPDFVersionModeKeepsPollingAfterTransientError(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	shortenTaskPolling(t)

	env.setMockResponse("/api/documents/57/update_version/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`"task-flaky"`))
	})
	polls := 0
	env.setMockResponse("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"count":1,"results":[{"task_id":"task-flaky","status":"failure","result":"bad pdf"}]}`))
	})

	app := &App{Client: env.client, Database: env.db}
	options := OCROptions{UploadPDF: true, UploadMode: PDFUploadModeVersion}
	err := app.uploadProcessedPDF(context.Background(), 57, []byte("%PDF-1.7"), options, logrus.WithField("test", "version"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejected the new version", "a later FAILURE is still seen after one failed status check")
	assert.Equal(t, 2, polls)
}

// hocrStubProvider stands in for Google Document AI: one recognized word per
// page, with hOCR, so ProcessDocumentOCR builds a real searchable PDF.
type hocrStubProvider struct {
	pages []hocr.Page
}

func (p *hocrStubProvider) ProcessImage(_ context.Context, _ []byte, pageNumber int) (*ocr.OCRResult, error) {
	word := hocr.Word{ID: fmt.Sprintf("word_%d", pageNumber), Text: "Invoice", Confidence: 99,
		BBox: hocr.BoundingBox{X1: 200, Y1: 200, X2: 600, Y2: 260}}
	p.pages = append(p.pages, hocr.Page{
		ID:         fmt.Sprintf("page_%d", pageNumber),
		PageNumber: pageNumber,
		BBox:       hocr.BoundingBox{X1: 0, Y1: 0, X2: 2480, Y2: 3508},
		Lines: []hocr.Line{{ID: fmt.Sprintf("line_%d", pageNumber), BBox: word.BBox,
			Words: []hocr.Word{word}}},
	})
	return &ocr.OCRResult{Text: "Invoice"}, nil
}
func (p *hocrStubProvider) IsHOCREnabled() bool       { return true }
func (p *hocrStubProvider) GetHOCRPages() []hocr.Page { return p.pages }
func (p *hocrStubProvider) GetHOCRDocument() (*hocr.HOCR, error) {
	return &hocr.HOCR{Title: "test", Pages: p.pages}, nil
}
func (p *hocrStubProvider) ResetHOCR() { p.pages = nil }

func TestProcessDocumentOCRVersionModeUploadsSearchablePDF(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	require.NoError(t, env.db.AutoMigrate(&OCRPageResult{}))
	shortenTaskPolling(t)

	original, err := os.ReadFile("tests/pdf/sample.pdf")
	require.NoError(t, err)

	const documentID = 2044
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/download/", documentID), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(original)
	})
	env.client.CacheFolder = t.TempDir()

	var uploaded []byte
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/update_version/", documentID), func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(10<<20))
		file, _, err := r.FormFile("document")
		require.NoError(t, err)
		uploaded, _ = io.ReadAll(file)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`"task-e2e"`))
	})
	env.setMockResponse("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"count":1,"results":[{"task_id":"task-e2e","status":"success"}]}`))
	})
	env.setMockResponse("/api/documents/post_document/", func(w http.ResponseWriter, r *http.Request) {
		t.Error("version mode must not upload a new document")
	})

	app := &App{Client: env.client, Database: env.db, ocrProvider: &hocrStubProvider{}, ocrProcessMode: "pdf"}
	// A prompt override avoids needing the OCR prompt template, as in ocr_test.go.
	options := OCROptions{UploadPDF: true, UploadMode: PDFUploadModeVersion, ProcessMode: "pdf", PromptOverride: "OCR"}

	doc, err := app.ProcessDocumentOCR(context.Background(), documentID, options, "")
	require.NoError(t, err)
	assert.Equal(t, "versioned", doc.PDFAction, doc.PDFDetail)
	require.NotEmpty(t, uploaded, "the searchable PDF is sent as a new version")
	assert.True(t, bytes.HasPrefix(uploaded, []byte("%PDF")))
	assert.NotEqual(t, original, uploaded, "the uploaded version carries the added text layer")
}

func shortenTaskPolling(t *testing.T) {
	attempts, interval := taskPollAttempts, taskPollInterval
	taskPollAttempts, taskPollInterval = 3, time.Millisecond
	t.Cleanup(func() { taskPollAttempts, taskPollInterval = attempts, interval })
}

func TestProcessDocumentOCRRejectsReplaceInVersionMode(t *testing.T) {
	app := &App{}
	options := OCROptions{UploadPDF: true, UploadMode: PDFUploadModeVersion, ReplaceOriginal: true}

	_, err := app.ProcessDocumentOCR(context.Background(), 1, options, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UploadMode=version")
}

func TestEffectiveOCRDefaultsVersionModeDropsReplace(t *testing.T) {
	app := &App{pdfUpload: true, pdfUploadMode: PDFUploadModeVersion, pdfReplace: true, ocrProcessMode: "pdf"}

	settingsMutex.Lock()
	origSettings := settings
	replace := true
	settings.OCR = OCRDefaults{ReplaceOriginal: &replace}
	settingsMutex.Unlock()
	defer func() {
		settingsMutex.Lock()
		settings = origSettings
		settingsMutex.Unlock()
	}()

	opts := app.effectiveOCRDefaults()
	assert.True(t, opts.UploadPDF)
	assert.Equal(t, PDFUploadModeVersion, opts.UploadMode)
	assert.False(t, opts.ReplaceOriginal, "a saved replace_original must not survive version mode")
}

func TestOCRHandlersRejectReplaceInVersionMode(t *testing.T) {
	t.Chdir(t.TempDir()) // the handlers save config/settings.json relative to the working directory
	gin.SetMode(gin.TestMode)
	db := newOCRRunTestDB(t)
	app := &App{Client: &mockPaperlessClient{}, Database: db, pdfUpload: true, pdfUploadMode: PDFUploadModeVersion}

	settingsMutex.Lock()
	origSettings := settings
	settingsMutex.Unlock()
	defer func() {
		settingsMutex.Lock()
		settings = origSettings
		settingsMutex.Unlock()
	}()

	router := gin.New()
	router.POST("/api/documents/:id/ocr", app.submitOCRJobHandler)
	router.PUT("/api/ocr/defaults", app.updateOCRDefaultsHandler)

	send := func(method, path, body string) *httptest.ResponseRecorder {
		req, err := http.NewRequest(method, path, bytes.NewBufferString(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	w := send(http.MethodPost, "/api/documents/12/ocr", `{"upload_pdf": true, "replace_original": true}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "PDF_UPLOAD_MODE=version")

	w = send(http.MethodPut, "/api/ocr/defaults", `{"replace_original": true}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "PDF_UPLOAD_MODE=version")
}
