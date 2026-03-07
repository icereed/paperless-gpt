package main

import (
	"bytes"
	"fmt"
)

// renderOCRPrompt renders the OCR template with per-document data.
// This follows the same pattern as getSuggestedTitle, getSuggestedTags, etc.
// which all render their templates per-document with the document's content.
func renderOCRPrompt(existingContent string) (string, error) {
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
