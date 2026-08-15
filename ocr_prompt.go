package main

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

// maxOCRExistingContentLen caps the existing-content reference injected into the
// OCR prompt. Vision model context is shared with image tokens, so we keep
// this modest. 8000 chars ≈ 2000 tokens — plenty for cross-referencing.
const maxOCRExistingContentLen = 8000

// renderOCRPrompt renders the OCR template with per-document data.
// This follows the same pattern as getSuggestedTitle, getSuggestedTags, etc.
// which all render their templates per-document with the document's content.
func renderOCRPrompt(existingContent string) (string, error) {
	if len(existingContent) > maxOCRExistingContentLen {
		existingContent = existingContent[:maxOCRExistingContentLen]
	}

	templateMutex.RLock()
	defer templateMutex.RUnlock()

	var buf bytes.Buffer
	err := ocrTemplate.Execute(&buf, map[string]interface{}{
		"Language": getLikelyLanguage(),
		"Content":  existingContent,
	})
	if err != nil {
		return "", fmt.Errorf("error executing OCR template: %w", err)
	}
	return buf.String(), nil
}

// renderOCRPromptOverride renders a run-scoped prompt template (Prompt
// Override) with the same data the saved template gets. The override never
// touches the saved template.
func renderOCRPromptOverride(override string, existingContent string) (string, error) {
	if len(existingContent) > maxOCRExistingContentLen {
		existingContent = existingContent[:maxOCRExistingContentLen]
	}

	tmpl, err := template.New("ocr_prompt_override").Funcs(sprig.FuncMap()).Parse(override)
	if err != nil {
		return "", fmt.Errorf("invalid prompt override: %w", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]interface{}{
		"Language": getLikelyLanguage(),
		"Content":  existingContent,
	})
	if err != nil {
		return "", fmt.Errorf("error executing prompt override: %w", err)
	}
	return buf.String(), nil
}
