package ocr

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

// fakeVisionLLM returns the queued errors in order, then succeeds.
type fakeVisionLLM struct {
	errs  []error
	calls int
}

func (f *fakeVisionLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) {
		return nil, f.errs[i]
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ocr text"}}}, nil
}

func (f *fakeVisionLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", errors.New("not used")
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil)
	assert.NoError(t, err)
	return buf.Bytes()
}

func retryTestProvider(llm llms.Model) *LLMProvider {
	return &LLMProvider{
		provider: "openai",
		model:    "test-model",
		llm:      llm,
		prompt:   "OCR this",
	}
}

func TestProcessImage_RetriesTransientErrors(t *testing.T) {
	t.Setenv("VISION_LLM_MAX_RETRIES", "8")
	t.Setenv("VISION_LLM_BACKOFF_MAX_WAIT", "1ms") // keep the test fast

	llm := &fakeVisionLLM{errs: []error{
		errors.New("API returned unexpected status code: 429"),
		errors.New("API returned unexpected status code: 503"),
	}}
	result, err := retryTestProvider(llm).ProcessImage(context.Background(), testJPEG(t), 1)

	assert.NoError(t, err)
	assert.Equal(t, "ocr text", result.Text)
	assert.Equal(t, 3, llm.calls)
}

func TestProcessImage_NonTransientErrorFailsFast(t *testing.T) {
	t.Setenv("VISION_LLM_MAX_RETRIES", "8")
	t.Setenv("VISION_LLM_BACKOFF_MAX_WAIT", "1ms")

	llm := &fakeVisionLLM{errs: []error{
		errors.New("API returned unexpected status code: 400"),
	}}
	_, err := retryTestProvider(llm).ProcessImage(context.Background(), testJPEG(t), 1)

	assert.Error(t, err)
	assert.Equal(t, 1, llm.calls)
}

func TestProcessImage_RetriesExhausted(t *testing.T) {
	t.Setenv("VISION_LLM_MAX_RETRIES", "2")
	t.Setenv("VISION_LLM_BACKOFF_MAX_WAIT", "1ms")

	llm := &fakeVisionLLM{errs: []error{
		errors.New("API returned unexpected status code: 429"),
		errors.New("API returned unexpected status code: 429"),
		errors.New("API returned unexpected status code: 429"),
	}}
	_, err := retryTestProvider(llm).ProcessImage(context.Background(), testJPEG(t), 1)

	assert.Error(t, err)
	assert.Equal(t, 3, llm.calls) // initial call + 2 retries
}

func TestProcessImage_ContextCanceledDuringBackoff(t *testing.T) {
	t.Setenv("VISION_LLM_MAX_RETRIES", "8")
	t.Setenv("VISION_LLM_BACKOFF_MAX_WAIT", "10s") // long backoff so cancellation fires first

	llm := &fakeVisionLLM{errs: []error{
		errors.New("API returned unexpected status code: 429"),
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := retryTestProvider(llm).ProcessImage(ctx, testJPEG(t), 1)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, llm.calls)
}

func TestProcessImage_ZeroRetriesRestoresFailFast(t *testing.T) {
	t.Setenv("VISION_LLM_MAX_RETRIES", "0")

	llm := &fakeVisionLLM{errs: []error{
		errors.New("API returned unexpected status code: 429"),
	}}
	_, err := retryTestProvider(llm).ProcessImage(context.Background(), testJPEG(t), 1)

	assert.Error(t, err)
	assert.Equal(t, 1, llm.calls)
}
