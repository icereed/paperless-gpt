package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasUnresolvedPDFUploadOlderUnconfirmedUploadSurvivesLaterPreUploadFailure(t *testing.T) {
	db := newOCRRunTestDB(t)
	older := &OCRRun{JobID: "upload-unconfirmed", DocumentID: 801, UploadPDF: true}
	require.NoError(t, CreateOCRRun(db, older))
	require.NoError(t, FinishOCRRun(db, older.JobID, "failed", "upload uncertain", 0, 1, "failed", ""))
	later := &OCRRun{JobID: "later-pre-upload-failure", DocumentID: 801, UploadPDF: true}
	require.NoError(t, CreateOCRRun(db, later))
	require.NoError(t, FinishOCRRun(db, later.JobID, "failed", "failed before upload", 0, 1, "none", ""))

	blocked, err := HasUnresolvedPDFUpload(db, 801, false)
	require.NoError(t, err)
	assert.True(t, blocked, "later failures before uploading must not hide an earlier uncertain upload")

	blocked, err = HasUnresolvedPDFUpload(db, 801, true)
	require.NoError(t, err)
	assert.True(t, blocked, "a stale completion tag must not clear an unresolved failed upload")

	reconciled := &OCRRun{JobID: "upload-reconciled", DocumentID: 801, UploadPDF: true}
	require.NoError(t, CreateOCRRun(db, reconciled))
	require.NoError(t, FinishOCRRun(db, reconciled.JobID, "completed", "", 1, 1, "versioned", ""))
	blocked, err = HasUnresolvedPDFUpload(db, 801, true)
	require.NoError(t, err)
	assert.False(t, blocked, "a confirmed upload plus completion tag clears the hold")
}

func (r *recordingClient) GetDocumentCountByTag(_ context.Context, tag string) (int, error) {
	return len(r.taggedDocuments[tag]), nil
}

type reviewPaginationClient struct {
	*recordingClient
	count         int
	requestedSize int
}

func (c *reviewPaginationClient) GetDocumentCountByTag(context.Context, string) (int, error) {
	return c.count, nil
}

func (c *reviewPaginationClient) GetDocumentsByTag(_ context.Context, tag string, pageSize int) ([]Document, error) {
	c.requestedSize = pageSize
	docs := c.taggedDocuments[tag]
	if pageSize < len(docs) {
		docs = docs[:pageSize]
	}
	return docs, nil
}

func TestProcessAutoOcrTagDocumentsDoesNotStarveBeyondInitialBatch(t *testing.T) {
	previousAutoTag, previousCompleteTag, previousTagging := autoOcrTag, pdfOCRCompleteTag, pdfOCRTagging
	t.Cleanup(func() {
		autoOcrTag, pdfOCRCompleteTag, pdfOCRTagging = previousAutoTag, previousCompleteTag, previousTagging
	})
	autoOcrTag, pdfOCRCompleteTag, pdfOCRTagging = "review-ocr-auto", "review-ocr-complete", true

	docs := make([]Document, 26)
	db := newOCRRunTestDB(t)
	for i := range docs {
		docs[i] = Document{ID: i + 1, Tags: []string{autoOcrTag}}
		if i < 25 {
			run := &OCRRun{JobID: fmt.Sprintf("held-%d", i+1), DocumentID: i + 1, UploadPDF: true}
			require.NoError(t, CreateOCRRun(db, run))
			require.NoError(t, FinishOCRRun(db, run.JobID, "failed", "upload unconfirmed", 0, 1, "failed", ""))
		}
	}
	client := &reviewPaginationClient{
		recordingClient: &recordingClient{taggedDocuments: map[string][]Document{autoOcrTag: docs}},
		count:           len(docs),
	}
	app := &App{
		Client: client, Database: db, docProcessor: &mockDocumentProcessor{mockText: "processed"},
		pdfOCRTagging: true, pdfOCRCompleteTag: pdfOCRCompleteTag,
	}

	processed, err := app.processAutoOcrTagDocuments(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, processed, "only document 26 should progress; first 25 remain safely held")
	require.Len(t, client.calls, 1)
	assert.Equal(t, 26, client.calls[0].ID, "later eligible document must reach processing despite held prefix")
	assert.GreaterOrEqual(t, client.requestedSize, len(docs), "queue fetch must include documents beyond first 25")
}

type reviewRequiredUploadProcessor struct{}

func (reviewRequiredUploadProcessor) ProcessDocumentOCR(context.Context, int, OCROptions, string) (*ProcessedDocument, error) {
	return &ProcessedDocument{PDFAction: "skipped", PDFDetail: "failed before upload"}, nil
}

func TestProcessAutoOcrTagDocumentsCountsRequiredUploadFailuresTowardRetryLimit(t *testing.T) {
	previousAutoTag, previousFailTag, previousRetries, previousUpload := autoOcrTag, failTag, ocrMaxRetries, settings.OCR.UploadPDF
	t.Cleanup(func() {
		autoOcrTag, failTag, ocrMaxRetries = previousAutoTag, previousFailTag, previousRetries
		settingsMutex.Lock()
		settings.OCR.UploadPDF = previousUpload
		settingsMutex.Unlock()
	})
	autoOcrTag, failTag, ocrMaxRetries = "review-ocr-auto", "review-failed", 2
	settingsMutex.Lock()
	upload := true
	settings.OCR.UploadPDF = &upload
	settingsMutex.Unlock()
	doc := Document{ID: 802, Title: "upload failure", Tags: []string{autoOcrTag}}
	client := &reviewPaginationClient{
		recordingClient: &recordingClient{taggedDocuments: map[string][]Document{autoOcrTag: {doc}}},
		count:           1,
	}
	app := &App{
		Client: client, Database: newOCRRunTestDB(t), docProcessor: reviewRequiredUploadProcessor{},
		pdfUpload: true,
	}

	_, err := app.processAutoOcrTagDocuments(context.Background())
	require.Error(t, err, "first pre-upload failure should surface below retry limit")
	require.Empty(t, client.calls, "no recovery update before reaching retry limit")

	_, err = app.processAutoOcrTagDocuments(context.Background())
	require.NoError(t, err, "failure reaching retry limit must be handled after fail-tagging")
	require.Len(t, client.calls, 1, "retry limit reached by second failure; trigger tag must be removed")
	assert.Equal(t, []string{autoOcrTag}, client.calls[0].RemoveTags)
	assert.Equal(t, []string{failTag}, client.calls[0].SuggestedTags)
}
