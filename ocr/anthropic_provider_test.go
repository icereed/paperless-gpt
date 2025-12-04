package ocr

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Verifies that the Anthropic client is created correctly with various configurations,
// including error handling when API key is missing
func TestCreateAnthropicClient(t *testing.T) {
	// Save original env and restore after test
	originalKey := os.Getenv("ANTHROPIC_API_KEY")
	defer os.Setenv("ANTHROPIC_API_KEY", originalKey)

	tests := []struct {
		name        string
		apiKey      string
		config      Config
		wantErr     bool
		errContains string
	}{
		{
			name:   "valid config",
			apiKey: "test-api-key",
			config: Config{
				VisionLLMModel: "claude-sonnet-4-5",
			},
			wantErr: false,
		},
		{
			name:   "valid config with different model",
			apiKey: "test-api-key",
			config: Config{
				VisionLLMModel: "claude-3-7-sonnet-latest",
			},
			wantErr: false,
		},
		{
			name:        "missing API key",
			apiKey:      "",
			config:      Config{VisionLLMModel: "claude-sonnet-4-5"},
			wantErr:     true,
			errContains: "Anthropic API key is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("ANTHROPIC_API_KEY", tt.apiKey)

			client, err := createAnthropicClient(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
			}
		})
	}
}

// Test the creation of LLM provider specifically configured for Anthropic,
// to ensure proper initialisation and API key validation
func TestNewLLMProvider_Anthropic(t *testing.T) {
	// Save original env and restore after test
	originalKey := os.Getenv("ANTHROPIC_API_KEY")
	defer os.Setenv("ANTHROPIC_API_KEY", originalKey)

	tests := []struct {
		name        string
		apiKey      string
		config      Config
		wantErr     bool
		errContains string
	}{
		{
			name:   "valid anthropic config",
			apiKey: "test-api-key",
			config: Config{
				VisionLLMProvider: "anthropic",
				VisionLLMModel:    "claude-sonnet-4-5",
			},
			wantErr: false,
		},
		{
			name:   "anthropic missing API key",
			apiKey: "",
			config: Config{
				VisionLLMProvider: "anthropic",
				VisionLLMModel:    "claude-sonnet-4-5",
			},
			wantErr:     true,
			errContains: "Anthropic API key is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("ANTHROPIC_API_KEY", tt.apiKey)

			provider, err := newLLMProvider(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, "anthropic", provider.provider)
				assert.Equal(t, tt.config.VisionLLMModel, provider.model)
			}
		})
	}
}

// Verify that Anthropic provider correctly handles all optional configuration parameters
func TestNewLLMProvider_AnthropicWithOptions(t *testing.T) {
	// Save original env and restore after test
	originalKey := os.Getenv("ANTHROPIC_API_KEY")
	defer os.Setenv("ANTHROPIC_API_KEY", originalKey)

	os.Setenv("ANTHROPIC_API_KEY", "test-api-key")

	temperature := 0.7
	config := Config{
		VisionLLMProvider:    "anthropic",
		VisionLLMModel:       "claude-sonnet-4-5",
		VisionLLMPrompt:      "Extract text from this image",
		VisionLLMMaxTokens:   4096,
		VisionLLMTemperature: &temperature,
	}

	provider, err := newLLMProvider(config)

	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "anthropic", provider.provider)
	assert.Equal(t, "claude-sonnet-4-5", provider.model)
	assert.Equal(t, "Extract text from this image", provider.prompt)
	assert.Equal(t, 4096, provider.maxTokens)
	assert.NotNil(t, provider.temperature)
	assert.Equal(t, 0.7, *provider.temperature)
}
