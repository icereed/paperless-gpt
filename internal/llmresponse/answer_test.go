package llmresponse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func thinkingChoice(text string) *llms.ContentChoice {
	return &llms.ContentChoice{
		StopReason:       "end_turn",
		ReasoningContent: text,
		Parts:            []llms.ContentPart{llms.ThinkingContent{Thinking: text, Signature: "sig"}},
	}
}

func textChoice(text string) *llms.ContentChoice {
	return &llms.ContentChoice{
		Content:    text,
		StopReason: "end_turn",
		Parts:      []llms.ContentPart{llms.TextContent{Text: text}},
	}
}

func TestAnswer(t *testing.T) {
	redacted := &llms.ContentChoice{Parts: []llms.ContentPart{llms.RedactedThinkingContent{Data: "x"}}}
	maxTokens := thinkingChoice("")
	maxTokens.StopReason = "max_tokens"

	tests := []struct {
		name string
		resp *llms.ContentResponse
		want string
	}{
		{"text only", &llms.ContentResponse{Choices: []*llms.ContentChoice{textChoice("Invoice")}}, "Invoice"},
		// Claude 5.x default: thinking display "omitted" → empty thinking block first.
		{"omitted thinking before text", &llms.ContentResponse{Choices: []*llms.ContentChoice{thinkingChoice(""), textChoice("Invoice")}}, "Invoice"},
		{"summarized thinking before text", &llms.ContentResponse{Choices: []*llms.ContentChoice{thinkingChoice("The sender bills…"), textChoice("Invoice")}}, "Invoice"},
		{"redacted thinking before text", &llms.ContentResponse{Choices: []*llms.ContentChoice{redacted, textChoice("Invoice")}}, "Invoice"},
		// The empty reply RateLimitedLLM hands out for ErrEmptyResponse.
		{"empty placeholder choice", &llms.ContentResponse{Choices: []*llms.ContentChoice{{}}}, ""},
		{"no choices", &llms.ContentResponse{}, ""},
		{"nil response", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Answer(tt.resp)
			if assert.NotNil(t, got) {
				assert.Equal(t, tt.want, got.Content)
			}
		})
	}

	t.Run("only thinking keeps the stop reason", func(t *testing.T) {
		got := Answer(&llms.ContentResponse{Choices: []*llms.ContentChoice{maxTokens}})
		assert.Empty(t, got.Content)
		assert.Equal(t, "max_tokens", got.StopReason)
	})
}

func TestTokenUsage(t *testing.T) {
	info := map[string]any{
		"InputTokens":          120,
		"OutputTokens":         int64(45),
		"CacheReadInputTokens": 0,
		"TotalTokens":          float64(165),
		"OutputContent":        "recognized page text",
		"ThinkingContent":      "reasoning",
		"StrangeTokens":        "not a number",
	}
	assert.Equal(t, map[string]any{
		"InputTokens":          120,
		"OutputTokens":         int64(45),
		"CacheReadInputTokens": 0,
		"TotalTokens":          float64(165),
	}, TokenUsage(info))
	assert.Empty(t, TokenUsage(nil))
}
