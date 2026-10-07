package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// TestDocument containing extra parameters for testing
type TestDocument struct {
	ID         int
	Title      string
	Tags       []string
	FailUpdate bool // simulate update failure
}

// Use this for TestCases in your tests
type TestCase struct {
	name           string
	documents      []TestDocument
	expectedCount  int
	expectedError  string
	updateResponse int // HTTP status code for update response
}

// Test our HTTP-Client
func TestCreateCustomHTTPClient(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify custom header
		assert.Equal(t, "paperless-gpt", r.Header.Get("X-Title"), "Expected X-Title header")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Get custom client
	client := createCustomHTTPClient()
	require.NotNil(t, client, "HTTP client should not be nil")

	// Make a request
	resp, err := client.Get(server.URL)
	require.NoError(t, err, "Request should not fail")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK response")
}

// OPENAI_HEADERS can carry gateway credentials. They must reach the
// OpenAI-compatible endpoint and nothing else: before, the custom client was
// http.DefaultClient itself, so the headers leaked into every other request
// made through it (Anthropic, tiktoken downloads, Ollama metadata).
func TestOpenAIHeadersStayOnTheOpenAIClient(t *testing.T) {
	t.Setenv("OPENAI_HEADERS", "Authorization=Bearer gateway-secret, X-Team = docs,broken")

	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := createCustomHTTPClient()
	require.NotSame(t, http.DefaultClient, client)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, "Bearer gateway-secret", got.Get("Authorization"))
	assert.Equal(t, "docs", got.Get("X-Team"), "spaces around key and value are dropped")
	assert.Equal(t, "paperless-gpt", got.Get("X-Title"))

	resp, err = http.DefaultClient.Get(server.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Empty(t, got.Get("Authorization"), "http.DefaultClient must not carry OPENAI_HEADERS")
	assert.Empty(t, got.Get("X-Title"))
}

// The metadata Google AI client used to be returned bare. Suggestion
// generation then ignored LLM_REQUESTS_PER_MINUTE and LLM_MAX_RETRIES,
// while the vision Google AI client and every other provider were wrapped.
func TestCreateLLMGoogleAIIsRateLimited(t *testing.T) {
	t.Setenv("GOOGLEAI_API_KEY", "test-key")
	t.Setenv("LLM_REQUESTS_PER_MINUTE", "8")

	previousProvider, previousModel := llmProvider, llmModel
	llmProvider, llmModel = "googleai", "gemini-test"
	t.Cleanup(func() { llmProvider, llmModel = previousProvider, previousModel })

	model, err := createLLM()
	require.NoError(t, err)

	wrapped, ok := model.(*RateLimitedLLM)
	require.True(t, ok, "Google AI metadata calls must go through the same rate-limit wrapper as the other providers")
	_, isGoogle := wrapped.llm.(*GoogleAIProvider)
	require.True(t, isGoogle)
	require.NotNil(t, wrapped.rateLimiter)
	assert.Equal(t, rate.Limit(8.0/60.0), wrapped.rateLimiter.Limit())
}
