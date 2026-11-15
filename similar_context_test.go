package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"text/template"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withIgnoredTags(t *testing.T, tags []string) {
	t.Helper()
	settingsMutex.Lock()
	previous := append([]string{}, settings.IgnoredTags...)
	settings.IgnoredTags = tags
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings.IgnoredTags = previous
		settingsMutex.Unlock()
	})
}

func TestFormatSimilarDocumentsOmitsIgnoredTags(t *testing.T) {
	withIgnoredTags(t, []string{"Inbox", "AI Review"})
	block := formatSimilarDocuments([]similarDocument{
		{Title: "Bill", Correspondent: "Stadtwerke", DocumentType: "Invoice", Tags: []string{"utilities", "inbox", "AI Review"}, Excerpt: "amount due"},
	})
	assert.Contains(t, block, "Tags used on similar documents: utilities")
	assert.Contains(t, block, `tags="utilities"`)
	assert.NotContains(t, block, "inbox")
	assert.NotContains(t, block, "AI Review")
}

func TestIgnoredTagsAreRemovedFromSuggestionsIncludingTagsAlreadyOnTheDocument(t *testing.T) {
	withIgnoredTags(t, []string{"inbox", "AI Review"})
	previousTemplate := tagTemplate
	previousCreate := createNewTags
	t.Cleanup(func() {
		tagTemplate = previousTemplate
		createNewTags = previousCreate
	})
	var err error
	tagTemplate, err = template.New("tag").Parse("{{range .AvailableTags}}{{.}} {{end}}")
	require.NoError(t, err)
	createNewTags = true

	llm := &mockLLM{Response: "utilities, INBOX, AI Review, brand-new"}
	app := &App{LLM: llm}
	suggested, err := app.getSuggestedTags(
		context.Background(),
		"electric bill",
		"scan",
		[]string{"utilities", "inbox", "AI Review"},
		[]string{"inbox"},
		logrus.NewEntry(logrus.New()),
		nil,
	)
	require.NoError(t, err)
	assert.Contains(t, suggested, "utilities")
	assert.Contains(t, suggested, "brand-new")
	assert.NotContains(t, suggested, "inbox")
	assert.NotContains(t, suggested, "AI Review")
	assert.NotContains(t, suggested, "INBOX")
	assert.NotContains(t, llm.lastPrompt, "inbox")
	assert.NotContains(t, llm.lastPrompt, "AI Review")

	suggestion, err := app.generateSingleDocumentSuggestion(
		context.Background(),
		GenerateSuggestionsRequest{GenerateTags: true},
		Document{ID: 7, Title: "scan", Content: "electric bill", Tags: []string{"inbox", "utilities"}},
		suggestionGenerationContext{availableTagNames: []string{"utilities", "inbox", "AI Review"}},
		logrus.NewEntry(logrus.New()),
	)
	require.NoError(t, err)
	assert.NotContains(t, suggestion.SuggestedTags, "inbox")
	assert.Contains(t, suggestion.RemoveTags, "inbox")
}

func TestNormalizeTagList(t *testing.T) {
	assert.Equal(t, []string{"Inbox", "AI Review"}, normalizeTagList([]string{" Inbox ", "inbox", "", "AI Review"}))
	assert.Empty(t, normalizeTagList(nil))
}

func TestFormatSimilarDocumentsDedupesMetadata(t *testing.T) {
	block := formatSimilarDocuments([]similarDocument{
		{Title: "Invoice <1>", Correspondent: "Stadtwerke", DocumentType: "Invoice", Tags: []string{"utilities", "invoice"}, Excerpt: "pay <now>"},
		{Title: "Reminder", Correspondent: "Stadtwerke", DocumentType: "Invoice", Tags: []string{"invoice", "reminder"}},
	})
	assert.Contains(t, block, "Tags used on similar documents: utilities, invoice, reminder")
	assert.Contains(t, block, "Correspondents: Stadtwerke")
	assert.Contains(t, block, "Document types: Invoice")
	assert.Contains(t, block, `title="Invoice &lt;1&gt;"`)
	assert.Contains(t, block, "pay &lt;now&gt;")
	assert.NotContains(t, block, "pay <now>")
	assert.Equal(t, "", formatSimilarDocuments(nil))
}

func TestSuggestionPromptIncludesSimilarDocuments(t *testing.T) {
	previous := tagTemplate
	t.Cleanup(func() { tagTemplate = previous })
	var err error
	tagTemplate, err = template.New("tag").Parse("BLOCK\n{{.SimilarDocuments}}\nCONTENT {{.Content}}")
	require.NoError(t, err)

	llm := &mockLLM{Response: "utilities"}
	app := &App{LLM: llm}
	ctx := contextWithSimilarDocuments(context.Background(), "<similar_documents>\nTags used on similar documents: utilities\n</similar_documents>")
	tags, err := app.getSuggestedTags(ctx, "electric bill", "scan", []string{"utilities"}, nil, logrus.NewEntry(logrus.New()), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"utilities"}, tags)
	assert.Contains(t, llm.lastPrompt, "Tags used on similar documents: utilities")
	assert.Contains(t, llm.lastPrompt, "electric bill")
}

type failingEmbedder struct{}

func (failingEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("embedder down")
}

func (failingEmbedder) Model() string { return "test-model" }

func TestSuggestionPromptIncludesNeighborsFromTheStore(t *testing.T) {
	previous := titleTemplate
	t.Cleanup(func() { titleTemplate = previous })
	var err error
	titleTemplate, err = template.New("title").Parse("{{if .SimilarDocuments}}{{.SimilarDocuments}}{{end}}\n{{.Content}}")
	require.NoError(t, err)

	store := NewEmbeddingStore(openEmbeddingDB(t), "nomic")
	require.NoError(t, store.Upsert(mustRow(t, Document{
		ID: 8, Title: "Neighbor bill", Content: "amount", Correspondent: "Stadtwerke", DocumentTypeName: "Invoice", Tags: []string{"utilities"},
	}, []float32{1, 0, 0})))
	require.NoError(t, store.Upsert(mustRow(t, Document{
		ID: 7, Title: "This document", Content: "amount", Tags: []string{"secret-self"},
	}, []float32{1, 0, 0})))

	llm := &mockLLM{Response: "Electric bill"}
	app := &App{
		LLM:          llm,
		Embedder:     &staticEmbedder{},
		Embeddings:   store,
		similarLimit: 5,
	}
	_, err = app.generateSingleDocumentSuggestion(
		context.Background(),
		GenerateSuggestionsRequest{GenerateTitles: true},
		Document{ID: 7, Title: "scan.pdf", Content: "amount due"},
		suggestionGenerationContext{},
		logrus.NewEntry(logrus.New()),
	)
	require.NoError(t, err)
	assert.Contains(t, llm.lastPrompt, "<similar_documents>")
	assert.Contains(t, llm.lastPrompt, "Stadtwerke")
	assert.Contains(t, llm.lastPrompt, "utilities")
	assert.NotContains(t, llm.lastPrompt, "secret-self")

	emptyLLM := &mockLLM{Response: "Electric bill"}
	app.LLM = emptyLLM
	// A second openEmbeddingDB call reuses this test's shared memory database,
	// so a different model name is what makes the neighbor lookup empty.
	app.Embeddings = NewEmbeddingStore(openEmbeddingDB(t), "unused-model")
	_, err = app.generateSingleDocumentSuggestion(
		context.Background(),
		GenerateSuggestionsRequest{GenerateTitles: true},
		Document{ID: 7, Title: "scan.pdf", Content: "amount due"},
		suggestionGenerationContext{},
		logrus.NewEntry(logrus.New()),
	)
	require.NoError(t, err)
	assert.NotContains(t, emptyLLM.lastPrompt, "<similar_documents>")
}

func TestEmbedderErrorDoesNotFailSuggestion(t *testing.T) {
	previous := titleTemplate
	t.Cleanup(func() { titleTemplate = previous })
	var err error
	titleTemplate, err = template.New("title").Parse("{{.Content}}")
	require.NoError(t, err)

	app := &App{
		LLM:          &mockLLM{Response: "Electric bill"},
		Embedder:     failingEmbedder{},
		Embeddings:   NewEmbeddingStore(openEmbeddingDB(t), "test-model"),
		similarLimit: 5,
	}
	suggestion, err := app.generateSingleDocumentSuggestion(
		context.Background(),
		GenerateSuggestionsRequest{GenerateTitles: true},
		Document{ID: 7, Title: "scan.pdf", Content: "amount due 40"},
		suggestionGenerationContext{},
		logrus.NewEntry(logrus.New()),
	)
	require.NoError(t, err)
	assert.Equal(t, "Electric bill", suggestion.SuggestedTitle)
}

func TestDisabledEmbeddingsMakeNoHTTPCall(t *testing.T) {
	previous := titleTemplate
	t.Cleanup(func() { titleTemplate = previous })
	var err error
	titleTemplate, err = template.New("title").Parse("{{.Content}}")
	require.NoError(t, err)

	app := &App{LLM: &mockLLM{Response: "Electric bill"}}
	_, err = app.generateSingleDocumentSuggestion(
		context.Background(),
		GenerateSuggestionsRequest{GenerateTitles: true},
		Document{ID: 1, Title: "scan.pdf", Content: "amount due"},
		suggestionGenerationContext{},
		logrus.NewEntry(logrus.New()),
	)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/embeddings/build", nil)
	app.buildEmbeddingsHandler(ctx)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/embeddings", nil)
	app.getEmbeddingsHandler(ctx)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"enabled":false`)
}
