package ocr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLLMProvider_SetPrompt(t *testing.T) {
	provider := &LLMProvider{
		prompt: "default prompt",
	}

	assert.Equal(t, "default prompt", provider.GetPrompt())

	provider.SetPrompt("custom per-document prompt")
	assert.Equal(t, "custom per-document prompt", provider.GetPrompt())
}

func TestLLMProvider_SetPrompt_Empty(t *testing.T) {
	provider := &LLMProvider{
		prompt: "default prompt",
	}

	// Setting empty string should still work (caller's responsibility)
	provider.SetPrompt("")
	assert.Equal(t, "", provider.GetPrompt())
}
