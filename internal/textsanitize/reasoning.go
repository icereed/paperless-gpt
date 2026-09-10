package textsanitize

import "strings"

const (
	openThinkTag  = "<think>"
	closeThinkTag = "</think>"
)

// StripReasoning removes reasoning content indicated by <think> and </think> tags.
// It is resilient to malformed or dangling tags and always trims the final output.
func StripReasoning(content string) string {
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
