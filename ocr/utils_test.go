package ocr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripReasoning(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "No reasoning tags",
			input:    "This is a test content without reasoning tags.",
			expected: "This is a test content without reasoning tags.",
		},
		{
			name:     "Reasoning tags at the start",
			input:    "<think>Start reasoning</think>\n\nContent      \n\n",
			expected: "Content",
		},
		{
			name:     "Reasoning tags in the middle",
			input:    "Before text <think>Some reasoning here</think> After text",
			expected: "Before text  After text",
		},
		{
			name:     "Reasoning tags at the end",
			input:    "Main content\n<think>Final thoughts</think>",
			expected: "Main content",
		},
		{
			name:     "Empty content",
			input:    "",
			expected: "",
		},
		{
			name:     "Only reasoning tags",
			input:    "<think>Just reasoning</think>",
			expected: "",
		},
		{
			name:     "Multiple lines with reasoning",
			input:    "Line 1\n<think>Reasoning\nMultiple lines\nOf thinking</think>\nLine 2",
			expected: "Line 1\n\nLine 2",
		},
		{
			name:     "Unclosed think tag",
			input:    "Content <think>Unclosed reasoning",
			expected: "Content <think>Unclosed reasoning",
		},
		{
			name:     "Only closing tag",
			input:    "Content </think>",
			expected: "Content </think>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := stripReasoning(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
