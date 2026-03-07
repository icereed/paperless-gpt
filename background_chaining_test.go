package main

import (
	"context"
	"testing"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// mockClientWithCapture extends mockClient to capture UpdateDocuments args
type mockClientWithCapture struct {
	*mockClient
	lastSuggestions []DocumentSuggestion
}

func (m *mockClientWithCapture) UpdateDocuments(ctx context.Context, documents []DocumentSuggestion, db *gorm.DB, isUndo bool) error {
	m.updateDocsCalled = true
	m.lastSuggestions = documents
	return nil
}

func TestClassifyDocument_Exists(t *testing.T) {
	autoTag = "paperless-gpt-auto"
	autoOcrTag = "paperless-gpt-ocr-auto"

	env := setupTest(t)
	defer env.teardown()

	client := newMockClient(env.client)
	app := &App{
		Client:   client,
		Database: env.db,
	}

	doc := Document{
		ID:      1,
		Title:   "Test Doc",
		Content: "OCR text about an electric bill",
		Tags:    []string{autoOcrTag},
	}

	ctx := context.Background()
	docLogger := documentLogger(doc.ID)

	// classifyDocument should exist and be callable.
	// It will error because LLM is nil, but that proves the function exists.
	_, err := app.classifyDocument(ctx, doc, docLogger)
	assert.Error(t, err) // Expected: nil LLM
}

func TestProcessAutoOcrTagDocuments_ChainingDisabled(t *testing.T) {
	autoOcrTag = "paperless-gpt-ocr-auto"
	autoTag = "paperless-gpt-auto"
	pdfOCRCompleteTag = "paperless-gpt-ocr-complete"

	// Ensure chaining is disabled
	origChaining := autoOcrThenClassify
	autoOcrThenClassify = false
	defer func() { autoOcrThenClassify = origChaining }()

	env := setupTest(t)
	defer env.teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	base := newMockClient(env.client)
	client := &mockClientWithCapture{mockClient: base}
	client.AddTag(autoOcrTag, 2)

	doc := Document{
		ID:      1,
		Title:   "Test Doc",
		Content: "Original tesseract text",
		Tags:    []string{autoOcrTag},
	}
	client.AddDocument(doc, []string{autoOcrTag})

	app := &App{
		Client:         client,
		Database:       env.db,
		ocrProvider:    &mockOCRProvider{text: "Better OCR text"},
		docProcessor:   &mockDocumentProcessor{mockText: "Better OCR text"},
		ocrProcessMode: "image",
	}

	count, err := app.processAutoOcrTagDocuments(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.True(t, client.updateDocsCalled)

	// With chaining disabled, UpdateDocuments should only have OCR content, no classification
	require.Len(t, client.lastSuggestions, 1)
	assert.Equal(t, "Better OCR text", client.lastSuggestions[0].SuggestedContent)
	// No title suggestion when chaining is disabled
	assert.Empty(t, client.lastSuggestions[0].SuggestedTitle)
}

func TestProcessAutoOcrTagDocuments_ChainingEnabled_ClassifyFails(t *testing.T) {
	autoOcrTag = "paperless-gpt-ocr-auto"
	autoTag = "paperless-gpt-auto"
	pdfOCRCompleteTag = "paperless-gpt-ocr-complete"

	// Initialize templates (needed by classifyDocument -> generateDocumentSuggestions)
	var err error
	titleTemplate, err = template.New("title").Funcs(sprig.FuncMap()).Parse("{{.Content}}")
	require.NoError(t, err)
	tagTemplate, err = template.New("tag").Funcs(sprig.FuncMap()).Parse("{{.Content}}")
	require.NoError(t, err)
	correspondentTemplate, err = template.New("correspondent").Funcs(sprig.FuncMap()).Parse("{{.Content}}")
	require.NoError(t, err)
	createdDateTemplate, err = template.New("created_date").Funcs(sprig.FuncMap()).Parse("{{.Content}}")
	require.NoError(t, err)
	ocrTemplate, err = template.New("ocr").Funcs(sprig.FuncMap()).Parse("OCR prompt")
	require.NoError(t, err)

	origChaining := autoOcrThenClassify
	autoOcrThenClassify = true
	defer func() { autoOcrThenClassify = origChaining }()

	env := setupTest(t)
	defer env.teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	base := newMockClient(env.client)
	client := &mockClientWithCapture{mockClient: base}
	client.AddTag(autoOcrTag, 2)
	client.AddTag(autoTag, 1)

	doc := Document{
		ID:      1,
		Title:   "Test Doc",
		Content: "Original tesseract text",
		Tags:    []string{autoOcrTag},
	}
	client.AddDocument(doc, []string{autoOcrTag})

	// App has no LLM, so classification will fail — but OCR should still succeed
	app := &App{
		Client:         client,
		Database:       env.db,
		LLM:            nil, // nil LLM will cause classification to fail
		ocrProvider:    &mockOCRProvider{text: "Better OCR text"},
		docProcessor:   &mockDocumentProcessor{mockText: "Better OCR text"},
		ocrProcessMode: "image",
	}

	count, err := app.processAutoOcrTagDocuments(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.True(t, client.updateDocsCalled)

	// Even when classification fails, OCR content should still be written
	require.Len(t, client.lastSuggestions, 1)
	assert.Equal(t, "Better OCR text", client.lastSuggestions[0].SuggestedContent)
}
