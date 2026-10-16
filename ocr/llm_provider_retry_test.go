package ocr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
	"google.golang.org/genai"
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

// The Google AI client returns genai.APIError (message "Error 429, ..."),
// wrapped the way googleAIClient.GenerateContent wraps it.
func TestProcessImage_RetriesGoogleAIRateLimitAndServerErrors(t *testing.T) {
	t.Setenv("VISION_LLM_MAX_RETRIES", "8")
	t.Setenv("VISION_LLM_BACKOFF_MAX_WAIT", "1ms")

	// The SDK returns the error as a value; a pointer is matched as well.
	var pointerErr error = &genai.APIError{Code: 429, Status: "RESOURCE_EXHAUSTED"}
	llm := &fakeVisionLLM{errs: []error{
		fmt.Errorf("googleai GenerateContent API error: %w", genai.APIError{Code: 429, Status: "RESOURCE_EXHAUSTED"}),
		fmt.Errorf("googleai GenerateContent API error: %w", genai.APIError{Code: 503, Status: "UNAVAILABLE"}),
		fmt.Errorf("googleai GenerateContent API error: %w", pointerErr),
	}}
	result, err := retryTestProvider(llm).ProcessImage(context.Background(), testJPEG(t), 1)

	assert.NoError(t, err)
	assert.Equal(t, "ocr text", result.Text)
	assert.Equal(t, 4, llm.calls)
}

func TestProcessImage_GoogleAIClientErrorFailsFast(t *testing.T) {
	t.Setenv("VISION_LLM_MAX_RETRIES", "8")
	t.Setenv("VISION_LLM_BACKOFF_MAX_WAIT", "1ms")

	llm := &fakeVisionLLM{errs: []error{
		fmt.Errorf("googleai GenerateContent API error: %w", genai.APIError{Code: 400, Status: "INVALID_ARGUMENT"}),
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

func TestVisionRetryWait(t *testing.T) {
	previous := retryJitter
	t.Cleanup(func() { retryJitter = previous })
	rateLimited := errors.New("API returned unexpected status code: 429")

	// At the maximum, the largest jitter factor must not push the wait above
	// VISION_LLM_BACKOFF_MAX_WAIT; with a 15m maximum that would exceed
	// maxProviderRetryDelay and fail the document instead of waiting.
	retryJitter = func() float64 { return 1.0 }
	assert.Equal(t, 15*time.Minute, visionRetryWait(20, 15*time.Minute, rateLimited))
	assert.Equal(t, 15*time.Minute, visionRetryWait(200, 15*time.Minute, rateLimited), "shift overflow falls back to the maximum")
	assert.Equal(t, 4*time.Second, visionRetryWait(2, time.Minute, rateLimited))

	// The smallest factor still spreads waits that sit at the maximum.
	retryJitter = func() float64 { return 0.8 }
	assert.Equal(t, 12*time.Minute, visionRetryWait(20, 15*time.Minute, rateLimited))

	// A server-provided Retry-After is a lower bound and is not shortened.
	withRetryAfter := errors.New(`{"error":{"message":"upstream rate limit; retry-after-ms:30000"}}`)
	assert.Equal(t, 30*time.Second, visionRetryWait(0, time.Minute, withRetryAfter))
}

func TestRetryJitterRange(t *testing.T) {
	for i := 0; i < 1000; i++ {
		f := retryJitter()
		if f < 0.8 || f > 1.0 {
			t.Fatalf("retryJitter() = %v, want a factor between 0.8 and 1.0", f)
		}
	}
}
