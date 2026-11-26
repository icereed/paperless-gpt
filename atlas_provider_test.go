package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateLLMAtlas(t *testing.T) {
	originalProvider, originalModel := llmProvider, llmModel
	t.Cleanup(func() {
		llmProvider, llmModel = originalProvider, originalModel
	})

	llmProvider = "atlas"
	llmModel = "qwen/qwen3.8-max"
	t.Setenv("ATLAS_API_KEY", "test-api-key")

	model, err := createLLM()
	require.NoError(t, err)
	assert.NotNil(t, model)
}

func TestCreateLLMAtlasRequiresAPIKey(t *testing.T) {
	originalProvider, originalModel := llmProvider, llmModel
	t.Cleanup(func() {
		llmProvider, llmModel = originalProvider, originalModel
	})

	llmProvider = "atlas"
	llmModel = "qwen/qwen3.8-max"
	t.Setenv("ATLAS_API_KEY", "")

	model, err := createLLM()
	assert.Nil(t, model)
	assert.EqualError(t, err, "Atlas Cloud API key is not set")
}

func TestCreateVisionLLMAtlas(t *testing.T) {
	originalProvider, originalModel := visionLlmProvider, visionLlmModel
	t.Cleanup(func() {
		visionLlmProvider, visionLlmModel = originalProvider, originalModel
	})

	visionLlmProvider = "atlas"
	visionLlmModel = "qwen/qwen3-vl-235b-a22b-thinking"
	t.Setenv("ATLAS_API_KEY", "test-api-key")

	model, err := createVisionLLM()
	require.NoError(t, err)
	assert.NotNil(t, model)
}
