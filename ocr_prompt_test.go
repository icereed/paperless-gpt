package main

import (
	"testing"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderOCRPrompt_WithContent(t *testing.T) {
	tmpl, err := template.New("test").Funcs(sprig.FuncMap()).Parse(
		`OCR this.{{- if .Content}} Existing text: {{.Content}}{{- end}}`)
	require.NoError(t, err)

	templateMutex.Lock()
	origTemplate := ocrTemplate
	ocrTemplate = tmpl
	templateMutex.Unlock()
	defer func() {
		templateMutex.Lock()
		ocrTemplate = origTemplate
		templateMutex.Unlock()
	}()

	result, err := renderOCRPrompt("existing tesseract text")
	require.NoError(t, err)
	assert.Contains(t, result, "existing tesseract text")
	assert.Contains(t, result, "OCR this.")
}

func TestRenderOCRPrompt_WithoutContent(t *testing.T) {
	tmpl, err := template.New("test").Funcs(sprig.FuncMap()).Parse(
		`OCR this.{{- if .Content}} Existing text: {{.Content}}{{- end}}`)
	require.NoError(t, err)

	templateMutex.Lock()
	origTemplate := ocrTemplate
	ocrTemplate = tmpl
	templateMutex.Unlock()
	defer func() {
		templateMutex.Lock()
		ocrTemplate = origTemplate
		templateMutex.Unlock()
	}()

	result, err := renderOCRPrompt("")
	require.NoError(t, err)
	assert.Equal(t, "OCR this.", result)
	assert.NotContains(t, result, "Existing text")
}

func TestRenderOCRPrompt_LanguageIncluded(t *testing.T) {
	t.Setenv("LLM_LANGUAGE", "German")

	tmpl, err := template.New("test").Funcs(sprig.FuncMap()).Parse(
		`Language: {{.Language}}`)
	require.NoError(t, err)

	templateMutex.Lock()
	origTemplate := ocrTemplate
	ocrTemplate = tmpl
	templateMutex.Unlock()
	defer func() {
		templateMutex.Lock()
		ocrTemplate = origTemplate
		templateMutex.Unlock()
	}()

	result, err := renderOCRPrompt("")
	require.NoError(t, err)
	assert.Equal(t, "Language: German", result)
}
