package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newOCRRunTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ModificationHistory{}, &OCRPageResult{}, &OCRRun{}))
	return db
}

func TestOCRRunLifecycle(t *testing.T) {
	db := newOCRRunTestDB(t)

	run := &OCRRun{
		JobID:         "job-1",
		DocumentID:    42,
		DocumentTitle: "Invoice",
		Trigger:       "manual",
		LimitPages:    3,
		ProcessMode:   "image",
		PDFAction:     "none",
	}
	require.NoError(t, CreateOCRRun(db, run))

	stored, err := GetOCRRunByJobID(db, "job-1")
	require.NoError(t, err)
	assert.Equal(t, "in_progress", stored.Status)
	assert.False(t, stored.StartedAt.IsZero())

	require.NoError(t, FinishOCRRun(db, "job-1", "completed", "", 3, 3, "attached", ""))

	stored, err = GetOCRRunByJobID(db, "job-1")
	require.NoError(t, err)
	assert.Equal(t, "completed", stored.Status)
	assert.Equal(t, "attached", stored.PDFAction)
	assert.Equal(t, 3, stored.PagesDone)
	require.NotNil(t, stored.FinishedAt)

	runs, total, err := ListOCRRuns(db, 42, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, runs, 1)
}

func TestMarkInterruptedOCRRuns(t *testing.T) {
	db := newOCRRunTestDB(t)
	require.NoError(t, CreateOCRRun(db, &OCRRun{JobID: "job-stale", DocumentID: 1, Trigger: "auto"}))

	require.NoError(t, MarkInterruptedOCRRuns(db))

	stored, err := GetOCRRunByJobID(db, "job-stale")
	require.NoError(t, err)
	assert.Equal(t, "interrupted", stored.Status)
}

func TestPruneOCRRunsKeepsPageTextsForRecentRuns(t *testing.T) {
	db := newOCRRunTestDB(t)

	// Seven runs for one document, each with one page result.
	for i := 1; i <= 7; i++ {
		jobID := fmt.Sprintf("job-%d", i)
		require.NoError(t, CreateOCRRun(db, &OCRRun{JobID: jobID, DocumentID: 7, Trigger: "manual"}))
		require.NoError(t, SaveSingleOcrPageResult(db, 7, jobID, 0, fmt.Sprintf("text %d", i), false, ""))
	}

	require.NoError(t, PruneOCRRuns(db, 7))

	var pageCount int64
	require.NoError(t, db.Model(&OCRPageResult{}).Where("document_id = ?", 7).Count(&pageCount).Error)
	assert.Equal(t, int64(ocrRunsWithPagesPerDocument), pageCount)

	// The newest run's pages survive.
	pages, err := GetOcrPageResults(db, 7, "job-7")
	require.NoError(t, err)
	require.Len(t, pages, 1)
	assert.Equal(t, "text 7", pages[0].Text)

	// Run records themselves are kept (below the global cap).
	_, total, err := ListOCRRuns(db, 7, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(7), total)
}

func TestGetOcrPageResultsDefaultsToLatestRun(t *testing.T) {
	db := newOCRRunTestDB(t)
	require.NoError(t, SaveSingleOcrPageResult(db, 5, "job-old", 0, "old text", false, ""))
	require.NoError(t, SaveSingleOcrPageResult(db, 5, "job-new", 0, "new text", false, ""))

	pages, err := GetOcrPageResults(db, 5, "")
	require.NoError(t, err)
	require.Len(t, pages, 1)
	assert.Equal(t, "new text", pages[0].Text)
}

func TestDocumentIDFromQuery(t *testing.T) {
	assert.Equal(t, 123, documentIDFromQuery("123"))
	assert.Equal(t, 123, documentIDFromQuery(" 123 "))
	assert.Equal(t, 456, documentIDFromQuery("http://paperless.local:8000/documents/456/details"))
	assert.Equal(t, 0, documentIDFromQuery("Rechnung 2026"))
	assert.Equal(t, 0, documentIDFromQuery(""))
	assert.Equal(t, 0, documentIDFromQuery("-5"))
}

func TestSubmitOCRJobHandlerValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newOCRRunTestDB(t)
	app := &App{Client: &mockPaperlessClient{}, Database: db}

	router := gin.New()
	router.POST("/api/documents/:id/ocr", app.submitOCRJobHandler)

	post := func(body string) *httptest.ResponseRecorder {
		req, err := http.NewRequest(http.MethodPost, "/api/documents/12/ocr", bytes.NewBufferString(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// replace_original without upload_pdf is invalid
	w := post(`{"replace_original": true, "upload_pdf": false}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// invalid process mode
	w = post(`{"process_mode": "scanner"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// negative page limit
	w = post(`{"limit_pages": -1}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// prompt override that does not parse
	w = post(`{"prompt_override": "{{ .Broken"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// upload_pdf without an hOCR-capable provider is rejected
	w = post(`{"upload_pdf": true}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// a valid submission is accepted and persists a run record
	w = post(`{"limit_pages": 2, "process_mode": "image"}`)
	require.Equal(t, http.StatusAccepted, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["job_id"])

	run, err := GetOCRRunByJobID(db, resp["job_id"])
	require.NoError(t, err)
	assert.Equal(t, "manual", run.Trigger)
	assert.Equal(t, 2, run.LimitPages)
	assert.Equal(t, "image", run.ProcessMode)

	// Drain the job the valid submission enqueued so no worker picks it up
	// in other tests.
	select {
	case <-jobQueue:
	default:
	}
}

func TestEffectiveOCRDefaultsMergesSettingsOverEnv(t *testing.T) {
	app := &App{pdfUpload: false, pdfReplace: false, pdfCopyMetadata: false, ocrProcessMode: "image"}

	settingsMutex.Lock()
	origSettings := settings
	limit := 9
	mode := "whole_pdf"
	settings.OCR = OCRDefaults{LimitPages: &limit, ProcessMode: &mode}
	settingsMutex.Unlock()
	defer func() {
		settingsMutex.Lock()
		settings = origSettings
		settingsMutex.Unlock()
	}()

	opts := app.effectiveOCRDefaults()
	assert.Equal(t, 9, opts.LimitPages)
	assert.Equal(t, "whole_pdf", opts.ProcessMode)
	assert.False(t, opts.UploadPDF)

	// replace without upload is normalized away
	settingsMutex.Lock()
	replace := true
	settings.OCR.ReplaceOriginal = &replace
	settingsMutex.Unlock()

	opts = app.effectiveOCRDefaults()
	assert.False(t, opts.ReplaceOriginal)
}
