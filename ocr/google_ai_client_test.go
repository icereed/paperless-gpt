package ocr

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func int32Ptr(v int32) *int32 { return &v }

func TestNewGoogleAIProvider(t *testing.T) {
	tests := []struct {
		name           string
		model          string
		apiKey         string
		thinkingBudget *int32
		wantErr        bool
		errContains    string
	}{
		{
			name:    "valid config",
			model:   "gemini-2.5-flash",
			apiKey:  "test-api-key",
			wantErr: false,
		},
		{
			name:           "valid config with thinking budget",
			model:          "gemini-2.5-flash-preview",
			apiKey:         "test-api-key",
			thinkingBudget: int32Ptr(16384),
			wantErr:        false,
		},
		{
			name:        "missing API key",
			model:       "gemini-2.5-flash",
			apiKey:      "",
			wantErr:     true,
			errContains: "GOOGLEAI_API_KEY environment variable is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewGoogleAIProvider(context.Background(), tt.model, tt.apiKey, tt.thinkingBudget)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, tt.model, provider.model)
				assert.NotNil(t, provider.client)
				if tt.thinkingBudget != nil {
					assert.Equal(t, *tt.thinkingBudget, *provider.thinkingBudget)
				}
			}
		})
	}
}

func TestNewLLMProvider_GoogleAI(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		wantErr     bool
		errContains string
	}{
		{
			name: "valid googleai config",
			config: Config{
				VisionLLMProvider: "googleai",
				VisionLLMModel:    "gemini-2.5-flash",
				GoogleAIAPIKey:    "test-api-key",
			},
			wantErr: false,
		},
		{
			name: "googleai missing API key",
			config: Config{
				VisionLLMProvider: "googleai",
				VisionLLMModel:    "gemini-2.5-flash",
				GoogleAIAPIKey:    "",
			},
			wantErr:     true,
			errContains: "GOOGLEAI_API_KEY environment variable is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				assert.Equal(t, "googleai", provider.provider)
				assert.Equal(t, tt.config.VisionLLMModel, provider.model)
			}
		})
	}
}

func TestNewLLMProvider_GoogleAIWithOptions(t *testing.T) {
	temperature := 0.7
	thinkingBudget := int32(8192)
	config := Config{
		VisionLLMProvider:      "googleai",
		VisionLLMModel:         "gemini-2.5-flash-preview",
		VisionLLMPrompt:        "Extract text from this image",
		VisionLLMMaxTokens:     4096,
		VisionLLMTemperature:   &temperature,
		GoogleAIAPIKey:         "test-api-key",
		GoogleAIThinkingBudget: &thinkingBudget,
	}

	provider, err := newLLMProvider(config)

	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "googleai", provider.provider)
	assert.Equal(t, "gemini-2.5-flash-preview", provider.model)
	assert.Equal(t, "Extract text from this image", provider.prompt)
	assert.Equal(t, 4096, provider.maxTokens)
	assert.NotNil(t, provider.temperature)
	assert.Equal(t, 0.7, *provider.temperature)
}

func TestGoogleAIProvider_GenerateContent_EmptyMessages(t *testing.T) {
	provider := &GoogleAIProvider{
		model: "gemini-2.5-flash",
	}

	resp, err := provider.GenerateContent(context.Background(), []llms.MessageContent{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no prompt provided")
	assert.Nil(t, resp)
}

func TestGoogleAIProvider_GenerateContent_EmptyParts(t *testing.T) {
	provider := &GoogleAIProvider{
		model: "gemini-2.5-flash",
	}

	messages := []llms.MessageContent{
		{Parts: []llms.ContentPart{}},
	}
	resp, err := provider.GenerateContent(context.Background(), messages, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no valid content parts found")
	assert.Nil(t, resp)
}

func TestGoogleAIProvider_GenerateContent_UnsupportedPartType(t *testing.T) {
	provider := &GoogleAIProvider{
		model: "gemini-2.5-flash",
	}

	messages := []llms.MessageContent{
		{Parts: []llms.ContentPart{
			llms.ToolCallResponse{},
		}},
	}
	resp, err := provider.GenerateContent(context.Background(), messages, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported content part type")
	assert.Nil(t, resp)
}

func TestGoogleAIProvider_GenerateContent_NonDataURL(t *testing.T) {
	provider := &GoogleAIProvider{
		model: "gemini-2.5-flash",
	}

	messages := []llms.MessageContent{
		{Parts: []llms.ContentPart{
			llms.ImageURLContent{URL: "https://example.com/image.jpg"},
		}},
	}
	resp, err := provider.GenerateContent(context.Background(), messages, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported ImageURLContent with non-data URL")
	assert.Nil(t, resp)
}

func TestGoogleAIProvider_GenerateContent_InvalidDataURL(t *testing.T) {
	provider := &GoogleAIProvider{
		model: "gemini-2.5-flash",
	}

	messages := []llms.MessageContent{
		{Parts: []llms.ContentPart{
			llms.ImageURLContent{URL: "data:image/jpeg;base64,not,valid,format"},
		}},
	}
	resp, err := provider.GenerateContent(context.Background(), messages, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid data URL format")
	assert.Nil(t, resp)
}

func TestGoogleAIProvider_GenerateContent_InvalidBase64(t *testing.T) {
	provider := &GoogleAIProvider{
		model: "gemini-2.5-flash",
	}

	messages := []llms.MessageContent{
		{Parts: []llms.ContentPart{
			llms.ImageURLContent{URL: "data:image/jpeg;base64,!!!not-base64!!!"},
		}},
	}
	resp, err := provider.GenerateContent(context.Background(), messages, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode base64 image")
	assert.Nil(t, resp)
}

func TestGoogleAIProvider_Close(t *testing.T) {
	provider := &GoogleAIProvider{}
	assert.NoError(t, provider.Close())
}

