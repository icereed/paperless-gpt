package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

// rateLimitMockLLM implements the llms.Model interface for testing rate limiting functionality
type rateLimitMockLLM struct {
	callResponses     []string
	callErrors        []error
	callIndex         int
	generateResponses []*llms.ContentResponse
	generateErrors    []error
	generateIndex     int
	callDelay         time.Duration
}

// Call implements the llms.Model interface for testing
func (m *rateLimitMockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	// Simulate processing delay if specified
	if m.callDelay > 0 {
		time.Sleep(m.callDelay)
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	// If we've run out of responses, return empty string and nil error
	if m.callIndex >= len(m.callResponses) {
		return "", errors.New("no more mock responses")
	}

	response := m.callResponses[m.callIndex]
	var err error
	if m.callIndex < len(m.callErrors) {
		err = m.callErrors[m.callIndex]
	}

	m.callIndex++
	return response, err
}

// CreateEmbedding is not used in these tests but required by the interface
func (m *rateLimitMockLLM) CreateEmbedding(_ context.Context, _ []string) ([][]float32, error) {
	return nil, errors.New("not implemented")
}

// GenerateContent implements the llms.Model interface for testing
func (m *rateLimitMockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	// Simulate processing delay if specified
	if m.callDelay > 0 {
		time.Sleep(m.callDelay)
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// If we've run out of responses, return nil and error
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

// newSuccessfulRateLimitMock creates a mock LLM that always returns successful responses
func newSuccessfulRateLimitMock() *rateLimitMockLLM {
	return &rateLimitMockLLM{
		callResponses: []string{"mock response", "mock response", "mock response", "mock response", "mock response"},
		callErrors:    []error{nil, nil, nil, nil, nil},
		generateResponses: []*llms.ContentResponse{
			{
				Choices: []*llms.ContentChoice{
					{
						Content: "mock content response",
					},
				},
			},
			{
				Choices: []*llms.ContentChoice{
					{
						Content: "mock content response",
					},
				},
			},
			{
				Choices: []*llms.ContentChoice{
					{
						Content: "mock content response",
					},
				},
			},
			{
				Choices: []*llms.ContentChoice{
					{
						Content: "mock content response",
					},
				},
			},
			{
				Choices: []*llms.ContentChoice{
					{
						Content: "mock content response",
					},
				},
			},
		},
		generateErrors: []error{nil, nil, nil, nil, nil},
	}
}

// newFailingRateLimitMock creates a mock LLM that always returns errors
func newFailingRateLimitMock() *rateLimitMockLLM {
	// Create arrays with multiple error responses to allow for retry counting
	callResponses := make([]string, 4) // 1 initial + 3 retries
	callErrors := make([]error, 4)
	generateResponses := make([]*llms.ContentResponse, 4)
	generateErrors := make([]error, 4)

	// Fill arrays with error responses
	for i := 0; i < 4; i++ {
		callResponses[i] = ""
		callErrors[i] = errors.New("mock error")
		generateResponses[i] = nil
		generateErrors[i] = errors.New("mock error")
	}

	return &rateLimitMockLLM{
		callResponses:     callResponses,
		callErrors:        callErrors,
		generateResponses: generateResponses,
		generateErrors:    generateErrors,
	}
}

// newEventuallySuccessfulRateLimitMock creates a mock LLM that fails a specified number of times before succeeding
func newEventuallySuccessfulRateLimitMock(failCount int) *rateLimitMockLLM {
	callResponses := make([]string, failCount+1)
	callErrors := make([]error, failCount+1)
	generateResponses := make([]*llms.ContentResponse, failCount+1)
	generateErrors := make([]error, failCount+1)

	// Set up failures for the specified count
	for i := 0; i < failCount; i++ {
		callResponses[i] = ""
		callErrors[i] = errors.New("mock error")
		generateResponses[i] = nil
		generateErrors[i] = errors.New("mock error")
	}

	// Set up success for the last item
	callResponses[failCount] = "successful response after retries"
	callErrors[failCount] = nil
	generateResponses[failCount] = &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "successful content after retries",
			},
		},
	}
	generateErrors[failCount] = nil

	return &rateLimitMockLLM{
		callResponses:     callResponses,
		callErrors:        callErrors,
		generateResponses: generateResponses,
		generateErrors:    generateErrors,
	}
}

func TestRateLimitedLLM_Call_Success(t *testing.T) {
	mockLLM := newSuccessfulRateLimitMock()
	config := RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second
		MaxRetries:        3,
		BackoffMaxWait:    5 * time.Second,
	}

	rateLimitedLLM := NewRateLimitedLLM(mockLLM, config)

	response, err := rateLimitedLLM.Call(context.Background(), "test prompt")

	assert.NoError(t, err)
	assert.Equal(t, "mock response", response)
	assert.Equal(t, 1, mockLLM.callIndex, "Should have made exactly one call")
}

func TestRateLimitedLLM_Call_Error(t *testing.T) {
	mockLLM := newFailingRateLimitMock()
	config := RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second
		MaxRetries:        3,
		BackoffMaxWait:    1 * time.Second, // Short backoff for tests
	}

	rateLimitedLLM := NewRateLimitedLLM(mockLLM, config)

	response, err := rateLimitedLLM.Call(context.Background(), "test prompt")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "all retry attempts failed")
	assert.Equal(t, "", response)
	assert.Equal(t, 4, mockLLM.callIndex, "Should have made 1 initial + 3 retry calls")
}

func TestRateLimitedLLM_Call_EventualSuccess(t *testing.T) {
	mockLLM := newEventuallySuccessfulRateLimitMock(2) // Fail twice, succeed on third try
	config := RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second
		MaxRetries:        3,
		BackoffMaxWait:    1 * time.Second, // Short backoff for tests
	}

	rateLimitedLLM := NewRateLimitedLLM(mockLLM, config)

	response, err := rateLimitedLLM.Call(context.Background(), "test prompt")

	assert.NoError(t, err)
	assert.Equal(t, "successful response after retries", response)
	assert.Equal(t, 3, mockLLM.callIndex, "Should have made 3 calls total (2 failures + 1 success)")
}

func TestRateLimitedLLM_GenerateContent_Success(t *testing.T) {
	mockLLM := newSuccessfulRateLimitMock()
	config := RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second
		MaxRetries:        3,
		BackoffMaxWait:    5 * time.Second,
	}

	rateLimitedLLM := NewRateLimitedLLM(mockLLM, config)

	message := llms.MessageContent{
		Role: "user",
		Parts: []llms.ContentPart{
			llms.TextContent{Text: "test message"},
		},
	}

	response, err := rateLimitedLLM.GenerateContent(context.Background(), []llms.MessageContent{message})

	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "mock content response", response.Choices[0].Content)
	assert.Equal(t, 1, mockLLM.generateIndex, "Should have made exactly one call")
}

func TestRateLimitedLLM_GenerateContent_Error(t *testing.T) {
	mockLLM := newFailingRateLimitMock()
	config := RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second
		MaxRetries:        3,
		BackoffMaxWait:    1 * time.Second, // Short backoff for tests
	}

	rateLimitedLLM := NewRateLimitedLLM(mockLLM, config)

	message := llms.MessageContent{
		Role: "user",
		Parts: []llms.ContentPart{
			llms.TextContent{Text: "test message"},
		},
	}

	response, err := rateLimitedLLM.GenerateContent(context.Background(), []llms.MessageContent{message})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "all retry attempts failed")
	assert.Nil(t, response)
	assert.Equal(t, 4, mockLLM.generateIndex, "Should have made 1 initial + 3 retry calls")
}

func TestRateLimitedLLM_GenerateContent_EventualSuccess(t *testing.T) {
	mockLLM := newEventuallySuccessfulRateLimitMock(2) // Fail twice, succeed on third try
	config := RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second
		MaxRetries:        3,
		BackoffMaxWait:    1 * time.Second, // Short backoff for tests
	}

	rateLimitedLLM := NewRateLimitedLLM(mockLLM, config)

	message := llms.MessageContent{
		Role: "user",
		Parts: []llms.ContentPart{
			llms.TextContent{Text: "test message"},
		},
	}

	response, err := rateLimitedLLM.GenerateContent(context.Background(), []llms.MessageContent{message})

	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "successful content after retries", response.Choices[0].Content)
	assert.Equal(t, 3, mockLLM.generateIndex, "Should have made 3 calls total (2 failures + 1 success)")
}

func TestRateLimitedLLM_ContextCancellation(t *testing.T) {
	mockLLM := &rateLimitMockLLM{
		callDelay: 500 * time.Millisecond,
	}
	config := RateLimitConfig{
		RequestsPerMinute: 60,
		MaxRetries:        3,
		BackoffMaxWait:    5 * time.Second,
	}

	rateLimitedLLM := NewRateLimitedLLM(mockLLM, config)

	// Create a context that cancels after a short time
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := rateLimitedLLM.Call(ctx, "test prompt")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

func TestRateLimitedLLM_RateLimiting(t *testing.T) {
	mockLLM := newSuccessfulRateLimitMock()
	config := RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second
		MaxRetries:        0,  // No retries for this test
	}

	rateLimitedLLM := NewRateLimitedLLM(mockLLM, config)

	// Make multiple calls and measure time
	start := time.Now()

	// Make 3 calls
	for i := 0; i < 3; i++ {
		_, err := rateLimitedLLM.Call(context.Background(), "test prompt")
		assert.NoError(t, err)
	}

	elapsed := time.Since(start)

	// With rate limit of 1 per second, 3 calls should take at least 2 seconds
	// (first call immediate, then wait 1s, second call, wait 1s, third call)
	assert.GreaterOrEqual(t, elapsed.Seconds(), 2.0,
		"Rate limiting should space requests by at least 1 second")
}
