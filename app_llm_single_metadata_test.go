package main

import (
	"context"
	"errors"
	"testing"
	"text/template"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setSingleRequestMetadata enables the single-request metadata path and swaps
// in a known template, restoring both after the test.
func setSingleRequestMetadata(t *testing.T, enabled bool, tmpl string) {
	t.Helper()
	oldFlag := llmSingleRequestMetadata
	oldTemplate := metadataTemplate
	llmSingleRequestMetadata = enabled
	metadataTemplate = template.Must(template.New("metadata_prompt").Parse(tmpl))
	t.Cleanup(func() {
		llmSingleRequestMetadata = oldFlag
		metadataTemplate = oldTemplate
	})
}

// setCustomFieldSelection points the global custom-field selection at the given
// ids and restores it afterwards.
func setCustomFieldSelection(t *testing.T, ids []int) {
	t.Helper()
	settingsMutex.Lock()
	old := settings.CustomFieldsSelectedIDs
	settings.CustomFieldsSelectedIDs = ids
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings.CustomFieldsSelectedIDs = old
		settingsMutex.Unlock()
	})
}

func singleRequestTestLogger() *logrus.Entry {
	return logrus.WithField("test", "single-request-metadata")
}

func TestGenerateSingleDocumentSuggestion_SingleRequestMetadata(t *testing.T) {
	setSingleRequestMetadata(t, true, `{{.Content}} {{.AvailableTags}} {{.AvailableCorrespondents}} {{.AvailableDocumentTypes}} {{.CustomFieldsXML}}`)
	setCustomFieldSelection(t, []int{1})

	app := &App{
		LLM: &mockLLM{Response: `{"title":"Invoice ACME","tags":["rechnung","steuer","unbekannt"],"correspondent":"ACME Corp","document_type":"invoice","created_date":"2024-01-15","custom_fields":[{"field":"amount","value":"42"}]}`},
		Client: &mockPaperlessClient{
			CustomFields: []CustomField{{ID: 1, Name: "amount", DataType: "number"}},
		},
	}

	req := GenerateSuggestionsRequest{
		GenerateTitles:         true,
		GenerateTags:           true,
		GenerateCorrespondents: true,
		GenerateDocumentTypes:  true,
		GenerateCreatedDate:    true,
		GenerateCustomFields:   true,
	}
	doc := Document{ID: 7, Title: "old.pdf", Content: "invoice body text"}
	genCtx := suggestionGenerationContext{
		availableTagNames:           []string{"rechnung", "steuer"},
		availableCorrespondentNames: []string{"ACME Corp"},
		availableDocumentTypeNames:  []string{"invoice"},
	}

	suggested, err := app.generateSingleDocumentSuggestion(context.Background(), req, doc, genCtx, singleRequestTestLogger())
	require.NoError(t, err)

	assert.Equal(t, "Invoice ACME", suggested.SuggestedTitle)
	assert.Equal(t, "ACME Corp", suggested.SuggestedCorrespondent)
	// "unbekannt" is not an available tag and CREATE_NEW_TAGS is off -> filtered out.
	assert.ElementsMatch(t, []string{"rechnung", "steuer"}, suggested.SuggestedTags)
	assert.Equal(t, "invoice", suggested.SuggestedDocumentType)
	assert.Equal(t, "2024-01-15", suggested.SuggestedCreatedDate)
	require.Len(t, suggested.SuggestedCustomFields, 1)
	assert.Equal(t, "amount", suggested.SuggestedCustomFields[0].Name)
	assert.Equal(t, "42", suggested.SuggestedCustomFields[0].Value)
	assert.Equal(t, 7, suggested.ID)
}

func TestGenerateSingleDocumentSuggestion_SingleRequestMetadataStripsCodeFence(t *testing.T) {
	setSingleRequestMetadata(t, true, `{{.Content}}`)

	app := &App{LLM: &mockLLM{Response: "```json\n{\"title\":\"T\",\"tags\":[],\"correspondent\":\"\",\"document_type\":\"\",\"created_date\":\"2024-01-02\"}\n```"}}
	req := GenerateSuggestionsRequest{GenerateTitles: true, GenerateCreatedDate: true}

	suggested, err := app.generateSingleDocumentSuggestion(context.Background(), req, Document{ID: 1, Content: "body"}, suggestionGenerationContext{}, singleRequestTestLogger())
	require.NoError(t, err)
	assert.Equal(t, "T", suggested.SuggestedTitle)
	assert.Equal(t, "2024-01-02", suggested.SuggestedCreatedDate)
}

func TestGenerateSingleDocumentSuggestion_SingleRequestMetadataNormalizesDotDate(t *testing.T) {
	setSingleRequestMetadata(t, true, `{{.Content}}`)

	app := &App{LLM: &mockLLM{Response: `{"created_date":"2024.01.15"}`}}
	req := GenerateSuggestionsRequest{GenerateCreatedDate: true}

	suggested, err := app.generateSingleDocumentSuggestion(context.Background(), req, Document{ID: 1, Content: "body"}, suggestionGenerationContext{}, singleRequestTestLogger())
	require.NoError(t, err)
	assert.Equal(t, "2024-01-15", suggested.SuggestedCreatedDate)
}

func TestGenerateSingleDocumentSuggestion_SingleRequestMetadataRejectsUnknownDocType(t *testing.T) {
	setSingleRequestMetadata(t, true, `{{.Content}}`)

	app := &App{LLM: &mockLLM{Response: `{"document_type":"memo"}`}}
	req := GenerateSuggestionsRequest{GenerateDocumentTypes: true}
	genCtx := suggestionGenerationContext{availableDocumentTypeNames: []string{"invoice"}}

	suggested, err := app.generateSingleDocumentSuggestion(context.Background(), req, Document{ID: 1, Content: "body"}, genCtx, singleRequestTestLogger())
	require.NoError(t, err)
	assert.Empty(t, suggested.SuggestedDocumentType, "a document type not in the available list must be dropped")
}

func TestGenerateSingleDocumentSuggestion_SingleRequestMetadataInvalidJSON(t *testing.T) {
	setSingleRequestMetadata(t, true, `{{.Content}}`)

	app := &App{LLM: &mockLLM{Response: "this is not json at all"}}
	req := GenerateSuggestionsRequest{GenerateTitles: true}

	_, err := app.generateSingleDocumentSuggestion(context.Background(), req, Document{ID: 1, Content: "body"}, suggestionGenerationContext{}, singleRequestTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse single-request metadata JSON")
}

func TestGenerateSingleDocumentSuggestion_SingleRequestMetadataEmptyResponse(t *testing.T) {
	setSingleRequestMetadata(t, true, `{{.Content}}`)

	app := &App{LLM: &mockLLM{Response: "   "}}
	req := GenerateSuggestionsRequest{GenerateTitles: true}

	_, err := app.generateSingleDocumentSuggestion(context.Background(), req, Document{ID: 1, Content: "body"}, suggestionGenerationContext{}, singleRequestTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty LLM response")
}

func TestGenerateSingleDocumentSuggestion_SingleRequestMetadataLLMError(t *testing.T) {
	setSingleRequestMetadata(t, true, `{{.Content}}`)

	app := &App{LLM: &mockLLM{Error: errors.New("provider down")}}
	req := GenerateSuggestionsRequest{GenerateTitles: true}

	_, err := app.generateSingleDocumentSuggestion(context.Background(), req, Document{ID: 1, Content: "body"}, suggestionGenerationContext{}, singleRequestTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider down")
}

func TestGenerateSingleDocumentSuggestion_SingleRequestMetadataPromptContent(t *testing.T) {
	setSingleRequestMetadata(t, true, `lang={{.Language}} title={{.Title}} tags={{.AvailableTags}} corr={{.AvailableCorrespondents}} types={{.AvailableDocumentTypes}} fields={{.CustomFieldsXML}} content={{.Content}}`)

	llm := &mockLLM{Response: `{}`}
	app := &App{LLM: llm}
	req := GenerateSuggestionsRequest{GenerateTitles: true, GenerateTags: true, GenerateCorrespondents: true, GenerateDocumentTypes: true}
	doc := Document{ID: 1, Title: "doc.pdf", Content: "unique invoice body"}
	genCtx := suggestionGenerationContext{
		availableTagNames:           []string{"rechnung"},
		availableCorrespondentNames: []string{"ACME Corp"},
		availableDocumentTypeNames:  []string{"invoice"},
	}

	_, err := app.generateSingleDocumentSuggestion(context.Background(), req, doc, genCtx, singleRequestTestLogger())
	require.NoError(t, err)

	for _, want := range []string{"unique invoice body", "ACME Corp", "rechnung", "invoice", "doc.pdf"} {
		assert.Contains(t, llm.lastPrompt, want)
	}
}

func TestGenerateSingleDocumentSuggestion_SingleRequestDisabledUsesPerFieldPath(t *testing.T) {
	setSingleRequestMetadata(t, false, `{{.Content}}`)

	// The per-field path uses the global title template; point it at a known one.
	oldTitleTemplate := titleTemplate
	titleTemplate = template.Must(template.New("title").Parse(`{{.Language}} {{.Title}} {{.Content}}`))
	t.Cleanup(func() { titleTemplate = oldTitleTemplate })

	llm := &mockLLM{Response: "title only"}
	app := &App{LLM: llm}
	req := GenerateSuggestionsRequest{GenerateTitles: true}

	suggested, err := app.generateSingleDocumentSuggestion(context.Background(), req, Document{ID: 1, Content: "body"}, suggestionGenerationContext{}, singleRequestTestLogger())
	require.NoError(t, err)
	// The per-field title path trims quotes/whitespace from the raw reply.
	assert.Equal(t, "title only", suggested.SuggestedTitle)
}
