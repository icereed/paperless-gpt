package textsanitize

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStripCodeFences verifies that only a fence wrapping the whole content is
// removed, while legitimate fences and inline backticks are preserved.
func TestStripCodeFences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "No fence",
			input:    "Plain recognized text.",
			expected: "Plain recognized text.",
		},
		{
			name:     "Wrapping fence with language tag",
			input:    "```markdown\nRecognized text\nsecond line\n```",
			expected: "Recognized text\nsecond line",
		},
		{
			name:     "Wrapping fence without language tag",
			input:    "```\nRecognized text\n```",
			expected: "Recognized text",
		},
		{
			name:     "Wrapping fence with surrounding whitespace",
			input:    "\n\n```markdown\nRecognized text\n```\n\n",
			expected: "Recognized text",
		},
		{
			name:     "Empty content",
			input:    "",
			expected: "",
		},
		{
			name:     "Fence with only opening (no close) left untouched",
			input:    "```markdown\nRecognized text without a close",
			expected: "```markdown\nRecognized text without a close",
		},
		{
			name:     "Inline backticks preserved",
			input:    "```markdown\nUse the `foo` command here\n```",
			expected: "Use the `foo` command here",
		},
		{
			name:     "Two independent code blocks are not unwrapped",
			input:    "```python\ncode\n```\ntext\n```js\nmore\n```",
			expected: "```python\ncode\n```\ntext\n```js\nmore\n```",
		},
		{
			name:     "Opening line with real content is not a fence",
			input:    "```not a real fence because text follows on same line\nbody\n```",
			expected: "```not a real fence because text follows on same line\nbody\n```",
		},
		{
			name:     "Empty fenced block",
			input:    "```\n\n```",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, StripCodeFences(tc.input))
		})
	}
}
