package ocr

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

type rateLimitMockLLM struct {
	generateResponses []*llms.ContentResponse
	generateErrors    []error
	generateIndex     int
}

func (m *rateLimitMockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", errors.New("not implemented")
}

func (m *rateLimitMockLLM) CreateEmbedding(_ context.Context, _ []string) ([][]float32, error) {
	return nil, errors.New("not implemented")
}

func (m *rateLimitMockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if m.generateIndex >= len(m.generateResponses) {
		return nil, errors.New("no more mock responses")
	}

	response := m.generateResponses[m.generateIndex]
	var err error
	if m.generateIndex < len(m.generateErrors) {
		err = m.generateErrors[m.generateIndex]
	}

	m.generateIndex++
	return response, err
}

func TestLLMProvider_RateLimiting(t *testing.T) {
	mock := &rateLimitMockLLM{
		generateResponses: []*llms.ContentResponse{
			nil,
			{
				Choices: []*llms.ContentChoice{
					{Content: "Extracted OCR text after retry"},
				},
			},
		},
		generateErrors: []error{
			errors.New("API returned unexpected status code: 429: Rate limit reached"),
			nil,
		},
	}

	wrappedModel := NewRateLimitedLLM(mock, RateLimitConfig{
		RequestsPerMinute: 60,
		MaxRetries:        3,
		BackoffMaxWait:    100 * time.Millisecond,
	})

	provider := &LLMProvider{
		provider: "openai",
		model:    "test-model",
		llm:      wrappedModel,
		prompt:   "Extract text",
	}

	// Create a valid 1x1 JPEG image
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, nil)
	assert.NoError(t, err)

	result, err := provider.ProcessImage(context.Background(), buf.Bytes(), 1)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Extracted OCR text after retry", result.Text)
	assert.Equal(t, 2, mock.generateIndex, "Should retry and succeed on 2nd attempt")
}
