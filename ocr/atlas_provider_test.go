package ocr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAtlasClient(t *testing.T) {
	t.Setenv("ATLAS_API_KEY", "test-api-key")

	client, err := createAtlasClient(Config{VisionLLMModel: "qwen/qwen3-vl-235b-a22b-thinking"})
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestCreateAtlasClientRequiresAPIKey(t *testing.T) {
	t.Setenv("ATLAS_API_KEY", "")

	client, err := createAtlasClient(Config{VisionLLMModel: "qwen/qwen3-vl-235b-a22b-thinking"})
	assert.Nil(t, client)
	assert.EqualError(t, err, "Atlas Cloud API key is not set")
}

func TestNewLLMProviderAtlas(t *testing.T) {
	t.Setenv("ATLAS_API_KEY", "test-api-key")
	config := Config{
		VisionLLMProvider: "atlas",
		VisionLLMModel:    "qwen/qwen3-vl-235b-a22b-thinking",
		VisionLLMPrompt:   "Extract text from this image",
	}

	provider, err := newLLMProvider(config)
	require.NoError(t, err)
	assert.Equal(t, "atlas", provider.provider)
	assert.Equal(t, config.VisionLLMModel, provider.model)
	assert.Equal(t, config.VisionLLMPrompt, provider.prompt)
}
