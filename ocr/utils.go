package ocr

import "strings"

const (
	openThinkTag  = "<think>"
	closeThinkTag = "</think>"
)

// stripReasoning removes the reasoning from the content indicated by <think> and </think> tags.
// This is useful for models that include reasoning in their output which should be removed
// from the final OCR text.
func stripReasoning(content string) string {
	if content == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(content))

	remaining := content
	for len(remaining) > 0 {
		openIdx := strings.Index(remaining, openThinkTag)
		closeIdx := strings.Index(remaining, closeThinkTag)

		switch {
		case openIdx == -1 && closeIdx == -1:
			builder.WriteString(remaining)
			remaining = ""
		case closeIdx != -1 && (openIdx == -1 || closeIdx < openIdx):
			builder.Reset()
			remaining = remaining[closeIdx+len(closeThinkTag):]
		case openIdx != -1 && (closeIdx == -1 || openIdx < closeIdx):
			builder.WriteString(remaining[:openIdx])
			remaining = remaining[openIdx+len(openThinkTag):]
			nextClose := strings.Index(remaining, closeThinkTag)
			if nextClose == -1 {
				remaining = ""
			} else {
				remaining = remaining[nextClose+len(closeThinkTag):]
			}
		}
	}

	return strings.TrimSpace(builder.String())
}
