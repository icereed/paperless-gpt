package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

type groupingTestReply struct {
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}

func TestMetadataGroupingPreservesUpstreamFilters(t *testing.T) {
	prepareGroupingFixture(t)
	oldLimit, oldFail, oldComplete, oldOCRComplete := correspondentPromptLimit, failTag, autoTagComplete, pdfOCRCompleteTag
	t.Cleanup(func() {
		correspondentPromptLimit, failTag, autoTagComplete, pdfOCRCompleteTag = oldLimit, oldFail, oldComplete, oldOCRComplete
	})
	correspondentPromptLimit = 1
	failTag, autoTagComplete, pdfOCRCompleteTag = "failed", "complete", "ocr-complete"
	createNewTags = true
	doc := Document{ID: 1, Title: "Invoice", Content: "Synthetic invoice from Vendor.", Tags: []string{"invoice", "FAILED", autoTag, "COMPLETE", pdfOCRCompleteTag}}
	provider := &groupingTestModel{replies: []string{"Vendor invoice", `{"tags":["invoice","PAPERLESS-GPT","FAILED","COMPLETE","ocr-complete"],"correspondent":"Vendor","document_type":"Invoice","created_date":"2026-09-01"}`}}
	app := &App{LLM: provider, Client: &mockPaperlessClient{}}
	request := GenerateSuggestionsRequest{GenerateTitles: true, GenerateTags: true, GenerateCorrespondents: true, GenerateDocumentTypes: true, GenerateCreatedDate: true}
	candidates := suggestionGenerationContext{availableTagNames: []string{"invoice", failTag, autoTagComplete, pdfOCRCompleteTag}, availableCorrespondentNames: []string{"Unrelated", "Vendor"}, availableDocumentTypeNames: []string{"Invoice"}}
	suggestion, err := app.generateSingleDocumentSuggestion(context.Background(), request, doc, candidates, log.WithField("test", "upstream filters"))
	require.NoError(t, err)
	require.Equal(t, []string{"invoice"}, suggestion.SuggestedTags, "System tags must never be reapplied as suggestions")
	require.Equal(t, "Vendor", suggestion.SuggestedCorrespondent)
	require.Len(t, provider.calls, 2, "The opt-in success path has a two-request budget")
	var input groupedMetadataInput
	prompt := provider.calls[1].Prompt
	require.NoError(t, json.Unmarshal([]byte(prompt[strings.Index(prompt, "{"):]), &input))
	require.Equal(t, []string{"Vendor"}, input.Correspondents, "The provider must receive only the configured candidate subset")
	require.Equal(t, []string{"invoice"}, input.AvailableTags)
}

func TestCorrespondentBlacklistConfigTrimsEntries(t *testing.T) {
	require.Equal(t, []string{"John Doe", "Jane Smith"}, parseCorrespondentBlacklist(" John Doe, Jane Smith, ,  "))
}

func TestSpacedCorrespondentBlacklistStopsGroupedFallback(t *testing.T) {
	prepareGroupingFixture(t)
	correspondentBlackList = parseCorrespondentBlacklist("John Doe, Jane Smith")
	doc := Document{ID: 1, Content: "Synthetic invoice from Jane Smith.", Tags: []string{"invoice"}}
	provider := &groupingTestModel{replies: []string{
		"Invoice",
		`{"tags":["invoice"],"correspondent":"jane smith","document_type":"Invoice","created_date":"2026-09-01"}`,
		"invoice",
		" Jane Smith ",
	}}
	app := &App{LLM: provider, Client: &mockPaperlessClient{}}
	request := GenerateSuggestionsRequest{GenerateTitles: true, GenerateTags: true, GenerateCorrespondents: true, GenerateDocumentTypes: true, GenerateCreatedDate: true}
	candidates := suggestionGenerationContext{availableTagNames: []string{"invoice"}, availableCorrespondentNames: []string{"Vendor"}, availableDocumentTypeNames: []string{"Invoice"}}

	suggestion, err := app.generateSingleDocumentSuggestion(context.Background(), request, doc, candidates, log.WithField("test", "spaced correspondent blacklist"))

	require.ErrorContains(t, err, "suggested correspondent is blacklisted")
	require.Equal(t, DocumentSuggestion{}, suggestion)
}

type groupingTestModel struct {
	replies []string
	calls   []groupingTestReply
}

func (p *groupingTestModel) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", fmt.Errorf("unexpected string API; metadata uses GenerateContent")
}

func (p *groupingTestModel) GenerateContent(_ context.Context, messages []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	var prompt strings.Builder
	for _, message := range messages {
		for _, part := range message.Parts {
			text, ok := part.(llms.TextContent)
			if !ok {
				return nil, fmt.Errorf("non-text metadata input")
			}
			prompt.WriteString(text.Text)
		}
	}
	index := len(p.calls)
	if index >= len(p.replies) {
		p.calls = append(p.calls, groupingTestReply{Prompt: prompt.String(), Response: "UNEXPECTED EXTRA CALL"})
		return nil, fmt.Errorf("unexpected extra model call")
	}
	reply := p.replies[index]
	p.calls = append(p.calls, groupingTestReply{Prompt: prompt.String(), Response: reply})
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: reply}}}, nil
}

type boundaryProvider struct{ groupingTestModel }

func (p *boundaryProvider) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	completion, err := p.groupingTestModel.GenerateContent(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	switch completion.Choices[0].Content {
	case "PROVIDER_ERROR":
		return nil, fmt.Errorf("synthetic provider failure")
	case "EMPTY_CHOICES":
		return &llms.ContentResponse{}, nil
	}
	return completion, nil
}

func prepareGroupingFixture(t *testing.T) {
	t.Helper()
	root, err := os.Getwd()
	require.NoError(t, err)
	oldTitle, oldTag, oldCorrespondent := titleTemplate, tagTemplate, correspondentTemplate
	oldType, oldDate, oldCustom, oldOCR, oldAdhoc := documentTypeTemplate, createdDateTemplate, customFieldTemplate, ocrTemplate, adhocAnalysisTemplate
	oldLimit, oldCreate, oldBlacklist, oldSettings := tokenLimit, createNewTags, correspondentBlackList, settings
	oldManual, oldAuto, oldAutoOCR := manualTag, autoTag, autoOcrTag
	t.Cleanup(func() {
		titleTemplate, tagTemplate, correspondentTemplate = oldTitle, oldTag, oldCorrespondent
		documentTypeTemplate, createdDateTemplate, customFieldTemplate, ocrTemplate, adhocAnalysisTemplate = oldType, oldDate, oldCustom, oldOCR, oldAdhoc
		tokenLimit, createNewTags, correspondentBlackList, settings = oldLimit, oldCreate, oldBlacklist, oldSettings
		manualTag, autoTag, autoOcrTag = oldManual, oldAuto, oldAutoOCR
	})
	t.Chdir(t.TempDir())
	require.NoError(t, os.CopyFS("default_prompts", os.DirFS(filepath.Join(root, "default_prompts"))))
	require.NoError(t, loadTemplates())
	t.Setenv("LLM_LANGUAGE", "English")
	t.Setenv("LLM_METADATA_GROUPING", "true")
	tokenLimit, createNewTags = 0, false
	correspondentBlackList = []string{"Blocked synthetic sender"}
	settings = Settings{}
	manualTag, autoTag, autoOcrTag = "paperless-gpt", "paperless-gpt-auto", "paperless-gpt-ocr-auto"
}

func TestMetadataGroupingPreservesBoundaries(t *testing.T) {
	cases := []string{"opt-out", "title-only", "three-fields", "custom-fields", "empty-tags", "empty-correspondents", "empty-document-types", "edited-tag-template", "token-limit", "malformed-json", "missing-field", "null-tags", "extra-field", "unknown-document-type", "unknown-tag", "invalid-date", "blacklisted-correspondent", "provider-error", "fallback-error", "empty-choices", "new-tags", "empty-tags-response", "normalized-document-type"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			prepareGroupingFixture(t)
			doc := Document{ID: 1, Title: "Original synthetic invoice", Content: "Synthetic invoice SYN-001 from Vendor on 2026-09-01 for 42.00 USD.", Tags: []string{"invoice"}, DocumentTypeName: "Invoice", CreatedDate: "2026-09-01"}
			request := GenerateSuggestionsRequest{GenerateTitles: true, GenerateTags: true, GenerateCorrespondents: true, GenerateDocumentTypes: true, GenerateCreatedDate: true}
			candidates := suggestionGenerationContext{availableTagNames: []string{"invoice", "unrelated", manualTag}, availableCorrespondentNames: []string{"Vendor"}, availableDocumentTypeNames: []string{"Invoice"}}
			groupReply := `{"tags":["invoice"],"correspondent":"Vendor","document_type":"Invoice","created_date":"2026-09-01"}`
			tail := []string{"invoice", "Vendor", "Invoice", "2026-09-01"}
			fallback := false
			baseline := false
			expected := DocumentSuggestion{ID: doc.ID, OriginalDocument: doc, SuggestedTitle: "Generated synthetic invoice", SuggestedTags: []string{"invoice"}, SuggestedCorrespondent: "Vendor", SuggestedDocumentType: "Invoice", SuggestedCreatedDate: "2026-09-01", RemoveTags: []string{manualTag, autoTag}}
			switch name {
			case "opt-out":
				t.Setenv("LLM_METADATA_GROUPING", "false")
				baseline = true
			case "title-only":
				request = GenerateSuggestionsRequest{GenerateTitles: true}
				tail = nil
				baseline = true
				expected.SuggestedCorrespondent = ""
				expected.SuggestedDocumentType = ""
				expected.SuggestedCreatedDate = ""
			case "three-fields":
				request.GenerateDocumentTypes = false
				request.GenerateCreatedDate = false
				tail = tail[:2]
				baseline = true
				expected.SuggestedDocumentType = ""
				expected.SuggestedCreatedDate = ""
			case "custom-fields":
				request.GenerateCustomFields = true
				settings = Settings{CustomFieldsSelectedIDs: []int{1}}
				baseline = true
				tail = append(tail, `[{"field":"Invoice Number","value":"SYN-001"}]`)
				expected.SuggestedCustomFields = []CustomFieldSuggestion{{ID: 1, Name: "Invoice Number", Value: "SYN-001"}}
			case "empty-tags":
				candidates.availableTagNames = nil
				baseline = true
				expected.SuggestedTags = []string{}
			case "empty-correspondents":
				candidates.availableCorrespondentNames = nil
				baseline = true
			case "empty-document-types":
				candidates.availableDocumentTypeNames = nil
				baseline = true
				tail = []string{tail[0], tail[1], tail[3]}
				expected.SuggestedDocumentType = ""
			case "edited-tag-template":
				tagTemplate = template.Must(template.New("tag_prompt.tmpl").Parse("CUSTOM-TAG-PROMPT {{.Title}} {{.Content}}"))
				baseline = true
			case "token-limit":
				tokenLimit = 4096
				baseline = true
			case "malformed-json":
				groupReply = "{bad"
				fallback = true
			case "missing-field":
				groupReply = `{"tags":["invoice"],"correspondent":"Vendor","document_type":"Invoice"}`
				fallback = true
			case "null-tags":
				groupReply = strings.Replace(groupReply, `["invoice"]`, "null", 1)
				fallback = true
			case "extra-field":
				groupReply = strings.TrimSuffix(groupReply, "}") + `,"unexpected":true}`
				fallback = true
			case "unknown-document-type":
				groupReply = strings.Replace(groupReply, `"Invoice"`, `"Unknown type"`, 1)
				fallback = true
			case "unknown-tag":
				groupReply = strings.Replace(groupReply, `["invoice"]`, `["Unavailable"]`, 1)
				fallback = true
			case "invalid-date":
				groupReply = strings.Replace(groupReply, "2026-09-01", "2026-02-30", 1)
				fallback = true
			case "blacklisted-correspondent":
				groupReply = strings.Replace(groupReply, "Vendor", "Blocked synthetic sender", 1)
				fallback = true
			case "provider-error":
				groupReply = "PROVIDER_ERROR"
				fallback = true
			case "fallback-error":
				groupReply = "{bad"
				tail = []string{"PROVIDER_ERROR"}
				fallback = true
			case "empty-choices":
				groupReply = "EMPTY_CHOICES"
				fallback = true
			case "new-tags":
				createNewTags = true
				groupReply = strings.Replace(groupReply, `["invoice"]`, `["new"]`, 1)
				expected.SuggestedTags = []string{"invoice", "new"}
			case "empty-tags-response":
				groupReply = strings.Replace(groupReply, `["invoice"]`, `[]`, 1)
			case "normalized-document-type":
				groupReply = strings.Replace(groupReply, `"Invoice"`, `"invoice"`, 1)
			}
			replies := []string{"Generated synthetic invoice"}
			if !baseline {
				replies = append(replies, groupReply)
			}
			if baseline || fallback {
				replies = append(replies, tail...)
			}
			provider := &boundaryProvider{groupingTestModel: groupingTestModel{replies: replies}}
			app := &App{LLM: provider, Client: &mockPaperlessClient{CustomFields: []CustomField{{ID: 1, Name: "Invoice Number", DataType: "string"}}}}
			suggestion, err := app.generateSingleDocumentSuggestion(context.Background(), request, doc, candidates, log.WithField("boundary", name))
			if name == "fallback-error" {
				require.ErrorContains(t, err, "synthetic provider failure")
				require.Equal(t, DocumentSuggestion{}, suggestion)
				return
			}
			require.NoError(t, err)
			require.Equal(t, expected, suggestion)
		})
	}
}
