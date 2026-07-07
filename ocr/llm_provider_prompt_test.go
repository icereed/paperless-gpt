package ocr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLLMProvider_WithPrompt(t *testing.T) {
	original := &LLMProvider{
		prompt: "default prompt",
	}

	clone := original.WithPrompt("custom per-document prompt")
	assert.Equal(t, "custom per-document prompt", clone.GetPrompt())
	// Original is not mutated
	assert.Equal(t, "default prompt", original.GetPrompt())
}

func TestLLMProvider_WithPrompt_Empty(t *testing.T) {
	original := &LLMProvider{
		prompt: "default prompt",
	}

	clone := original.WithPrompt("")
	assert.Equal(t, "", clone.GetPrompt())
	assert.Equal(t, "default prompt", original.GetPrompt())
}
