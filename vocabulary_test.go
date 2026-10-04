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
// "Unknown" to no value and rejects everything else. With unrestricted set it
// constrains nothing.
type fakeVocabulary struct {
	values       []string
	hints        map[string]string
	unrestricted bool
	err          error
	lastRequest  *extension.CandidatesRequest
	resolved     *[]extension.ResolveRequest
}

func (f fakeVocabulary) Candidates(_ context.Context, req extension.CandidatesRequest) (extension.Candidates, error) {
	if f.lastRequest != nil {
		*f.lastRequest = req
	}
	return extension.Candidates{Unrestricted: f.unrestricted, Values: f.values, Hints: f.hints}, f.err
}

func (f fakeVocabulary) Resolve(_ context.Context, req extension.ResolveRequest) (extension.Resolution, error) {
	if f.resolved != nil {
		*f.resolved = append(*f.resolved, req)
	}
	if f.err != nil {
		return extension.Resolution{}, f.err
	}
	if f.unrestricted {
		return extension.Resolution{Accepted: true, Value: req.Proposed}, nil
	}
	if req.Proposed == "" || strings.EqualFold(req.Proposed, "Unknown") {
		return extension.Resolution{Accepted: true}, nil
	}
	for _, v := range f.values {
		if strings.EqualFold(v, req.Proposed) {
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

// generateCorrespondent runs correspondent generation for one document.
func generateCorrespondent(t *testing.T, llm *mockLLM) (DocumentSuggestion, error) {
	t.Helper()
	app := &App{LLM: llm, Client: &mockPaperlessClient{}}
	request := GenerateSuggestionsRequest{GenerateCorrespondents: true}
	generationContext, err := app.prepareSuggestionGenerationContext(context.Background(), request)
	require.NoError(t, err)
	return app.generateSingleDocumentSuggestion(context.Background(), request,
		Document{ID: 1, Title: "Invoice 42", Content: "Invoice from ACME"}, generationContext, logrus.NewEntry(logrus.New()))
}

func TestVocabularyCandidatesSeeTheDocument(t *testing.T) {
	setCorrespondentTestTemplate(t)
	var got extension.CandidatesRequest
	registerCorrespondentVocabulary(t, fakeVocabulary{values: []string{"Acme"}, lastRequest: &got})

	_, err := generateCorrespondent(t, &mockLLM{Response: "Acme"})
	require.NoError(t, err)
	assert.Equal(t, extension.CandidatesRequest{
		Field:      extension.FieldCorrespondent,
		DocumentID: 1,
		Title:      "Invoice 42",
		Content:    "Invoice from ACME",
	}, got)
}

func TestVocabularyUnavailableFailsGeneration(t *testing.T) {
	setCorrespondentTestTemplate(t)
	registerCorrespondentVocabulary(t, fakeVocabulary{err: errors.New("source never loaded")})

	_, err := generateCorrespondent(t, &mockLLM{Response: "Acme"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source never loaded")
}

func TestUnrestrictedVocabularyKeepsDefaultBehaviour(t *testing.T) {
	setCorrespondentTestTemplate(t)
	registerCorrespondentVocabulary(t, fakeVocabulary{unrestricted: true})

	llm := &mockLLM{Response: "Brand New Corp"}
	suggestion, err := generateCorrespondent(t, llm)
	require.NoError(t, err)
	assert.Contains(t, llm.lastPrompt, "Candidates: Vendor", "paperless-ngx values are used")
	assert.Equal(t, "Brand New Corp", suggestion.SuggestedCorrespondent)
	assert.Empty(t, suggestion.RejectedFields)
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
			suggestion, err := generateCorrespondent(t, llm)
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

func TestResolveRequestsCarryTheStage(t *testing.T) {
	setCorrespondentTestTemplate(t)
	var resolved []extension.ResolveRequest
	registerCorrespondentVocabulary(t, fakeVocabulary{values: []string{"Alpha", "Beta"}, resolved: &resolved})

	_, err := generateCorrespondent(t, &mockLLM{Response: "Alpha"})
	require.NoError(t, err)

	env := newTestEnv(t)
	defer env.teardown()
	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results": []}`))
	})
	env.setMockResponse("/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	require.NoError(t, env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{{
		ID: 1, OriginalDocument: Document{ID: 1, Title: "Old"}, SuggestedTitle: "New", SuggestedCorrespondent: "Beta",
	}}, env.db, false))

	require.Len(t, resolved, 2)
	assert.Equal(t, extension.StageGenerate, resolved[0].Stage)
	assert.Equal(t, extension.StageApply, resolved[1].Stage)
	assert.Equal(t, 1, resolved[1].DocumentID)
}

// docTypeVocabulary is unrestricted, contributes hints, records resolve
// requests and rejects one value.
type docTypeVocabulary struct {
	hints    map[string]string
	reject   string
	resolved *[]extension.ResolveRequest
}

func (v docTypeVocabulary) Candidates(context.Context, extension.CandidatesRequest) (extension.Candidates, error) {
	return extension.Candidates{Unrestricted: true, Hints: v.hints}, nil
}

func (v docTypeVocabulary) Resolve(_ context.Context, req extension.ResolveRequest) (extension.Resolution, error) {
	*v.resolved = append(*v.resolved, req)
	if req.Proposed == v.reject {
		return extension.Resolution{Reason: "not wanted"}, nil
	}
	return extension.Resolution{Accepted: true, Value: req.Proposed}, nil
}

func generateDocumentType(t *testing.T, llm *mockLLM) (DocumentSuggestion, error) {
	t.Helper()
	app := &App{LLM: llm, Client: &mockPaperlessClient{}}
	request := GenerateSuggestionsRequest{GenerateDocumentTypes: true}
	generationContext, err := app.prepareSuggestionGenerationContext(context.Background(), request)
	require.NoError(t, err)
	return app.generateSingleDocumentSuggestion(context.Background(), request,
		Document{ID: 7, Title: "Rechnung 42", Content: "Rechnungsnummer 42"}, generationContext, logrus.NewEntry(logrus.New()))
}

func setDocumentTypeTemplate(t *testing.T, text string) {
	t.Helper()
	prev, prevLimit := documentTypeTemplate, tokenLimit
	t.Cleanup(func() { documentTypeTemplate, tokenLimit = prev, prevLimit })
	tokenLimit = 0
	documentTypeTemplate = template.Must(template.New("document_type").Funcs(sprig.FuncMap()).Parse(text))
}

func TestDocumentTypeHintsReachThePrompt(t *testing.T) {
	var resolved []extension.ResolveRequest
	extension.Reset()
	t.Cleanup(extension.Reset)
	extension.RegisterVocabulary(extension.FieldDocumentType, docTypeVocabulary{
		hints: map[string]string{"Invoice": "Bill with an amount due"}, resolved: &resolved})

	// A template that places the hints itself.
	setDocumentTypeTemplate(t, "Types: {{.AvailableDocumentTypes | join \", \"}}{{range $k, $v := .DocumentTypeHints}} [{{$k}}={{$v}}]{{end}}\n{{.Content}}")
	llm := &mockLLM{Response: "invoice"}
	suggestion, err := generateDocumentType(t, llm)
	require.NoError(t, err)
	assert.Equal(t, "Invoice", suggestion.SuggestedDocumentType)
	assert.Contains(t, llm.lastPrompt, "[Invoice=Bill with an amount due]")
	assert.NotContains(t, llm.lastPrompt, "<document_type_descriptions>", "not appended twice")

	require.Len(t, resolved, 1)
	assert.Equal(t, extension.ResolveRequest{Field: extension.FieldDocumentType, DocumentID: 7, Proposed: "invoice",
		Known: []string{"Invoice"}, Stage: extension.StageGenerate}, resolved[0], "the raw answer and the known types")

	// A customized template from before hints existed: they are appended.
	setDocumentTypeTemplate(t, "Types: {{.AvailableDocumentTypes | join \", \"}}\n{{.Content}}")
	llm = &mockLLM{Response: "Invoice"}
	_, err = generateDocumentType(t, llm)
	require.NoError(t, err)
	assert.Contains(t, llm.lastPrompt, "<document_type_descriptions>\n- Invoice: Bill with an amount due\n</document_type_descriptions>")
}

func TestDocumentTypeRejectionClearsTheSuggestion(t *testing.T) {
	var resolved []extension.ResolveRequest
	extension.Reset()
	t.Cleanup(extension.Reset)
	extension.RegisterVocabulary(extension.FieldDocumentType, docTypeVocabulary{reject: "Invoice", resolved: &resolved})
	setDocumentTypeTemplate(t, "{{.Content}}")

	suggestion, err := generateDocumentType(t, &mockLLM{Response: "Invoice"})
	require.NoError(t, err)
	assert.Empty(t, suggestion.SuggestedDocumentType)
	assert.Equal(t, []string{"document_type"}, suggestion.RejectedFields)

	// An invented type is not applied either way, but the vocabulary sees it.
	resolved = nil
	suggestion, err = generateDocumentType(t, &mockLLM{Response: "Space Travel Voucher"})
	require.NoError(t, err)
	assert.Empty(t, suggestion.SuggestedDocumentType)
	require.Len(t, resolved, 1)
	assert.Equal(t, "Space Travel Voucher", resolved[0].Proposed)
}

func TestExtensionHostReadsPaperless(t *testing.T) {
	h := extensionHost{client: &mockPaperlessClient{}}
	correspondents, err := h.Correspondents(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"Vendor"}, correspondents)
	types, err := h.DocumentTypes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"Invoice"}, types)
}
