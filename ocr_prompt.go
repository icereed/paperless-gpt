package main

import (
	"bytes"
	"fmt"
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
