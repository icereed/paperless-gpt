package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"text/template"

	"paperless-gpt/extension"

	"github.com/Masterminds/sprig/v3"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeVocabulary accepts the listed values case-insensitively, maps
// "Unknown" to no value and rejects everything else.
type fakeVocabulary struct {
	values []string
	err    error
}

func (f fakeVocabulary) Candidates(context.Context) ([]string, error) {
	return f.values, f.err
}

func (f fakeVocabulary) Resolve(_ context.Context, proposed string) (extension.Resolution, error) {
	if f.err != nil {
		return extension.Resolution{}, f.err
	}
	if proposed == "" || strings.EqualFold(proposed, "Unknown") {
		return extension.Resolution{Accepted: true}, nil
	}
	for _, v := range f.values {
		if strings.EqualFold(v, proposed) {
			return extension.Resolution{Accepted: true, Value: v}, nil
		}
	}
	return extension.Resolution{Reason: "not in vocabulary"}, nil
}

func registerCorrespondentVocabulary(t *testing.T, v extension.Vocabulary) {
	t.Helper()
	extension.Reset()
	t.Cleanup(extension.Reset)
	extension.RegisterVocabulary(extension.FieldCorrespondent, v)
}

func setCorrespondentTestTemplate(t *testing.T) {
	t.Helper()
	prev, prevTokenLimit := correspondentTemplate, tokenLimit
	t.Cleanup(func() { correspondentTemplate, tokenLimit = prev, prevTokenLimit })
	tokenLimit = 0
	var err error
	correspondentTemplate, err = template.New("correspondent").Funcs(sprig.FuncMap()).Parse(
		`Candidates: {{.AvailableCorrespondents | join ", "}}` + "\nContent: {{.Content}}")
	require.NoError(t, err)
}

func TestVocabularyReplacesPromptCandidates(t *testing.T) {
	registerCorrespondentVocabulary(t, fakeVocabulary{values: []string{"Acme", "Globex"}})

	app := &App{Client: &mockPaperlessClient{}}
	generationContext, err := app.prepareSuggestionGenerationContext(context.Background(), GenerateSuggestionsRequest{GenerateCorrespondents: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"Acme", "Globex"}, generationContext.availableCorrespondentNames,
		"the vocabulary, not paperless-ngx (\"Vendor\"), defines the candidates")
}

func TestVocabularyUnavailableFailsGeneration(t *testing.T) {
	registerCorrespondentVocabulary(t, fakeVocabulary{err: errors.New("source never loaded")})

	app := &App{Client: &mockPaperlessClient{}}
	_, err := app.prepareSuggestionGenerationContext(context.Background(), GenerateSuggestionsRequest{GenerateCorrespondents: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source never loaded")
}

func TestVocabularyResolvesGeneratedCorrespondent(t *testing.T) {
	setCorrespondentTestTemplate(t)
	registerCorrespondentVocabulary(t, fakeVocabulary{values: []string{"Acme", "Globex"}})

	tests := []struct {
		name         string
		llmResponse  string
		wantValue    string
		wantRejected []string
	}{
		{name: "known value is canonicalized", llmResponse: "acme", wantValue: "Acme"},
		{name: "hallucinated value is rejected", llmResponse: "Initech", wantValue: "", wantRejected: []string{"correspondent"}},
		{name: "unknown means no value", llmResponse: "Unknown", wantValue: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llm := &mockLLM{Response: tt.llmResponse}
			app := &App{LLM: llm, Client: &mockPaperlessClient{}}
			request := GenerateSuggestionsRequest{GenerateCorrespondents: true}
			generationContext, err := app.prepareSuggestionGenerationContext(context.Background(), request)
			require.NoError(t, err)

			suggestion, err := app.generateSingleDocumentSuggestion(context.Background(), request,
				Document{ID: 1, Content: "Invoice from ACME"}, generationContext, logrus.NewEntry(logrus.New()))
			require.NoError(t, err)

			assert.Equal(t, tt.wantValue, suggestion.SuggestedCorrespondent)
			assert.Equal(t, tt.wantRejected, suggestion.RejectedFields)
			assert.Contains(t, llm.lastPrompt, "Candidates: Acme, Globex")
		})
	}
}

func TestUpdateDocumentsEnforcesVocabulary(t *testing.T) {
	tests := []struct {
		name          string
		suggested     string
		isUndo        bool
		wantPatched   interface{} // nil = correspondent not in PATCH body
		wantDropped   bool
		wantCreatePOS bool
	}{
		{name: "value outside the vocabulary is never created", suggested: "Initech", wantDropped: true},
		{name: "value is canonicalized before lookup", suggested: "beta", wantPatched: float64(2)},
		{name: "undo is never blocked", suggested: "Initech", isUndo: true, wantCreatePOS: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registerCorrespondentVocabulary(t, fakeVocabulary{values: []string{"Alpha", "Beta"}})

			env := newTestEnv(t)
			defer env.teardown()

			created := false
			env.setMockResponse("/api/correspondents/", func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					created = true
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"id": 3, "name": "Initech"}`))
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"results": [{"id": 1, "name": "Alpha"}, {"id": 2, "name": "Beta"}]}`))
			})
			env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"results": [{"id": 5, "name": "keep"}]}`))
			})
			var patched map[string]interface{}
			env.setMockResponse("/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPatch {
					_ = json.NewDecoder(r.Body).Decode(&patched)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			})

			doc := DocumentSuggestion{
				ID:                     1,
				OriginalDocument:       Document{ID: 1, Title: "Old", Tags: []string{"keep"}},
				SuggestedTitle:         "New",
				SuggestedCorrespondent: tt.suggested,
			}
			err := env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{doc}, env.db, tt.isUndo)

			if tt.wantDropped {
				var partial *PartialUpdateError
				require.ErrorAs(t, err, &partial)
				assert.Equal(t, []string{"correspondent"}, partial.DroppedFields)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantCreatePOS, created, "correspondent creation")
			assert.Equal(t, "New", patched["title"], "the other fields are still applied")
			if tt.wantPatched == nil {
				if !tt.wantCreatePOS {
					assert.NotContains(t, patched, "correspondent")
				}
			} else {
				assert.Equal(t, tt.wantPatched, patched["correspondent"])
			}
		})
	}
}

func TestAutoTagAppliesFailTagForRejectedField(t *testing.T) {
	setCorrespondentTestTemplate(t)
	registerCorrespondentVocabulary(t, fakeVocabulary{values: []string{"Alpha", "Beta"}})

	prev := []string{failTag, autoTag, autoGenerateTitle, autoGenerateTags, autoGenerateCorrespondents, autoGenerateDocumentType, autoGenerateCreatedDate}
	t.Cleanup(func() {
		failTag, autoTag, autoGenerateTitle, autoGenerateTags, autoGenerateCorrespondents, autoGenerateDocumentType, autoGenerateCreatedDate =
			prev[0], prev[1], prev[2], prev[3], prev[4], prev[5], prev[6]
	})
	failTag = "paperless-gpt-failed"
	autoTag = "paperless-gpt-auto"
	autoGenerateTitle, autoGenerateTags, autoGenerateDocumentType, autoGenerateCreatedDate = "false", "false", "false", "false"
	autoGenerateCorrespondents = "true"

	env := newTestEnv(t)
	defer env.teardown()
	client := &recordingClient{
		PaperlessClient: env.client,
		taggedDocuments: map[string][]Document{autoTag: {{ID: 21, Title: "Doc", Tags: []string{autoTag}}}},
	}
	app := &App{Client: client, LLM: &mockLLM{Response: "Initech"}}

	count, err := app.processAutoTagDocuments(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	require.Len(t, client.calls, 2, "suggestion update, then fail-tag update")
	assert.Empty(t, client.calls[0].SuggestedCorrespondent, "the rejected value is never applied")
	assert.Equal(t, 21, client.calls[1].ID)
	assert.Equal(t, []string{failTag}, client.calls[1].SuggestedTags)
}
