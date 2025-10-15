package ocr

import "strings"

// stripReasoning removes the reasoning from the content indicated by <think> and </think> tags.
// This is useful for models that include reasoning in their output which should be removed
// from the final OCR text.
func stripReasoning(content string) string {
	// Remove reasoning from the content
	reasoningStart := strings.Index(content, "<think>")
	if reasoningStart != -1 {
		reasoningEnd := strings.Index(content, "</think>")
		if reasoningEnd != -1 {
			content = content[:reasoningStart] + content[reasoningEnd+len("</think>"):]
		}
	}

	// Trim whitespace
	content = strings.TrimSpace(content)
	return content
}
