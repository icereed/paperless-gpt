package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"text/template"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

type promptContextRecordingLLM struct {
	prompts []string
}

func (m *promptContextRecordingLLM) Call(ctx context.Context, _ string, _ ...llms.CallOption) (string, error) {
	m.prompts = append(m.prompts, ollamaPromptFromContext(ctx))
	return "analysis", nil
}

func (m *promptContextRecordingLLM) GenerateContent(ctx context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	promptName := ollamaPromptFromContext(ctx)
	m.prompts = append(m.prompts, promptName)
	content := "value"
	if promptName == "custom_field_prompt.tmpl" {
		content = `[{"field":"Amount","value":"12"}]`
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: content}}}, nil
}

func TestMetadataDispatchesTagEveryOllamaPrompt(t *testing.T) {
	previousTokenLimit := tokenLimit
	previousTitleTemplate, previousTagTemplate := titleTemplate, tagTemplate
	previousCorrespondentTemplate, previousDocumentTypeTemplate := correspondentTemplate, documentTypeTemplate
	previousCreatedDateTemplate, previousCustomFieldTemplate := createdDateTemplate, customFieldTemplate
	t.Cleanup(func() {
		tokenLimit = previousTokenLimit
		titleTemplate, tagTemplate = previousTitleTemplate, previousTagTemplate
		correspondentTemplate, documentTypeTemplate = previousCorrespondentTemplate, previousDocumentTypeTemplate
		createdDateTemplate, customFieldTemplate = previousCreatedDateTemplate, previousCustomFieldTemplate
	})

	tokenLimit = 0
	titleTemplate = template.Must(template.New("title").Parse(`{{.Content}}`))
	tagTemplate = template.Must(template.New("tags").Parse(`{{.Content}}`))
	correspondentTemplate = template.Must(template.New("correspondent").Parse(`{{.Content}}`))
	documentTypeTemplate = template.Must(template.New("document-type").Parse(`{{.Content}}`))
	createdDateTemplate = template.Must(template.New("created-date").Parse(`{{.Content}}`))
	customFieldTemplate = template.Must(template.New("custom-field").Parse(`{{.Content}}`))

	recorder := &promptContextRecordingLLM{}
	app := &App{
		LLM: recorder,
		Client: &mockPaperlessClient{CustomFields: []CustomField{
			{ID: 1, Name: "Amount", DataType: "string"},
		}},
	}
	ctx := context.Background()
	logger := logrus.NewEntry(log)

	_, err := app.getSuggestedCorrespondent(ctx, "text", "title", []string{"Vendor"}, nil)
	require.NoError(t, err)
	_, err = app.getSuggestedTags(ctx, "text", "title", []string{"invoice"}, nil, logger)
	require.NoError(t, err)
	_, err = app.getSuggestedDocumentType(ctx, "text", "title", []string{"Invoice"}, logger)
	require.NoError(t, err)
	_, err = app.getSuggestedTitle(ctx, "text", "title", logger)
	require.NoError(t, err)
	_, err = app.getSuggestedCreatedDate(ctx, "text", logger)
	require.NoError(t, err)
	_, err = app.getSuggestedCustomFields(ctx, Document{Content: "text"}, []int{1}, logger)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/analyze-documents", app.analyzeDocumentsHandler)
	request := httptest.NewRequest(http.MethodPost, "/api/analyze-documents", bytes.NewBufferString(`{"document_ids":[],"prompt":"analysis"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)

	assert.Equal(t, []string{
		"correspondent_prompt.tmpl",
		"tag_prompt.tmpl",
		"document_type_prompt.tmpl",
		"title_prompt.tmpl",
		"created_date_prompt.tmpl",
		"custom_field_prompt.tmpl",
		"adhoc-analysis_prompt.tmpl",
	}, recorder.prompts)
}
