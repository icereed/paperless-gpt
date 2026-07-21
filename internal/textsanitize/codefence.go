package textsanitize

import "strings"

// StripCodeFences removes a single Markdown code fence that wraps the entire
// content — e.g. a vision model returning its OCR output inside a
// ```markdown … ``` block. Only a fence enclosing the whole text is removed;
// fenced blocks that are genuinely part of the recognized document (or inline
// backticks) are left untouched, so the recognized text is never mangled.
func StripCodeFences(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	lines := strings.Split(trimmed, "\n")

	// A fence-delimiter line is one whose trimmed text starts with ```.
	var fenceLines []int
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenceLines = append(fenceLines, i)
		}
	}

	// Only unwrap when exactly one fence encloses the whole content: an opening
	// line at the top and a closing line at the very bottom. Anything else is
	// ambiguous (multiple blocks, no closing fence) and is left as-is.
	if len(fenceLines) != 2 || fenceLines[0] != 0 || fenceLines[1] != len(lines)-1 {
		return trimmed
	}

	// The opening line must carry only an optional language tag (e.g. "markdown"),
	// never real content that happened to start with backticks.
	info := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), "```"))
	if strings.ContainsAny(info, " \t") {
		return trimmed
	}

	inner := strings.Join(lines[1:fenceLines[1]], "\n")
	return strings.TrimSpace(inner)
}
