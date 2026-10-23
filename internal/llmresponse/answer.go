// Package llmresponse reads the model's answer and token usage out of a langchaingo response.
package llmresponse

import (
	"strings"

	"github.com/tmc/langchaingo/llms"
)

// Answer returns the choice that carries the model's answer.
//
// langchaingo's Anthropic client turns every content block of a reply into its
// own choice. Claude models that think by default (adaptive thinking, e.g.
// claude-haiku-5-5 and claude-sonnet-5-5) answer with a thinking block first
// and the text block after it, so Choices[0] is the thinking block, whose
// Content is always empty. Reading Choices[0] then silently drops the answer.
//
// Answer skips choices that only carry reasoning (thinking or redacted
// thinking) and returns the first one that doesn't. If there is none — e.g.
// max_tokens was reached while thinking — it returns the last choice, so callers
// see an empty answer with that choice's stop reason. It never returns nil.
func Answer(resp *llms.ContentResponse) *llms.ContentChoice {
	if resp == nil || len(resp.Choices) == 0 {
		return &llms.ContentChoice{}
	}
	for _, c := range resp.Choices {
		if c != nil && !reasoningOnly(c) {
			return c
		}
	}
	if last := resp.Choices[len(resp.Choices)-1]; last != nil {
		return last
	}
	return &llms.ContentChoice{}
}

// reasoningOnly reports whether a choice holds nothing but model reasoning.
func reasoningOnly(c *llms.ContentChoice) bool {
	if c.Content != "" || len(c.ToolCalls) > 0 || c.FuncCall != nil {
		return false
	}
	if len(c.Parts) == 0 {
		return c.ReasoningContent != ""
	}
	for _, part := range c.Parts {
		switch part.(type) {
		case llms.ThinkingContent, llms.RedactedThinkingContent:
		default:
			return false
		}
	}
	return true
}

// TokenUsage returns only the token counters from a choice's GenerationInfo
// (InputTokens, OutputTokens, TotalTokens, CacheReadInputTokens, ...), for
// logging. GenerationInfo can also carry the generated text itself (e.g.
// langchaingo's Anthropic client puts it in "OutputContent"), which must not
// end up in the logs.
func TokenUsage(info map[string]any) map[string]any {
	usage := make(map[string]any)
	for key, value := range info {
		if !strings.HasSuffix(key, "Tokens") {
			continue
		}
		switch value.(type) {
		case int, int32, int64, float32, float64:
			usage[key] = value
		}
	}
	return usage
}
