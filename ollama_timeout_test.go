package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOllamaRequestTimeout verifies the OLLAMA_TIMEOUT_SECONDS parsing,
// including the default and the opt-out (<= 0) behavior.
func TestOllamaRequestTimeout(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		expected time.Duration
	}{
		{name: "default when unset", setEnv: false, expected: 300 * time.Second},
		{name: "custom value", envValue: "600", setEnv: true, expected: 600 * time.Second},
		{name: "zero disables timeout", envValue: "0", setEnv: true, expected: 0},
		{name: "negative disables timeout", envValue: "-1", setEnv: true, expected: 0},
		{name: "invalid falls back to default", envValue: "notanumber", setEnv: true, expected: 300 * time.Second},
		{name: "empty falls back to default", envValue: "", setEnv: true, expected: 300 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("OLLAMA_TIMEOUT_SECONDS", tt.envValue)
			} else {
				// t.Setenv with empty string still "sets" it; to test the
				// truly-unset path we rely on the env being clean here.
				t.Setenv("OLLAMA_TIMEOUT_SECONDS", "")
			}
			assert.Equal(t, tt.expected, ollamaRequestTimeout())
		})
	}
}

// TestOllamaHTTPClientWithTimeout_DefaultWithoutHeaders is the core regression
// test: when OLLAMA_HEADERS is unset (the common case), the Ollama client used
// to be nil, causing langchaingo to fall back to a timeout-less http.Client and
// freeze the whole tagging loop on a single stalled request. The client must
// now be non-nil and carry the configured timeout.
func TestOllamaHTTPClientWithTimeout_DefaultWithoutHeaders(t *testing.T) {
	t.Setenv("OLLAMA_HEADERS", "")
	t.Setenv("OLLAMA_TIMEOUT_SECONDS", "")

	client := ollamaHTTPClientWithTimeout()
	require.NotNil(t, client, "client must not be nil so the request carries a timeout")
	assert.Equal(t, 300*time.Second, client.Timeout, "default 300s timeout must be applied")
}

// TestOllamaHTTPClientWithTimeout_PreservesHeaders verifies that when
// OLLAMA_HEADERS is configured, the header-injecting transport is preserved and
// the timeout is applied on top of it.
func TestOllamaHTTPClientWithTimeout_PreservesHeaders(t *testing.T) {
	t.Setenv("OLLAMA_HEADERS", "Authorization=Bearer token123")
	t.Setenv("OLLAMA_TIMEOUT_SECONDS", "42")

	client := ollamaHTTPClientWithTimeout()
	require.NotNil(t, client)
	assert.Equal(t, 42*time.Second, client.Timeout)

	// The custom transport must still inject the header.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestOllamaHTTPClientWithTimeout_OptOut verifies that a non-positive timeout
// with no headers returns nil, restoring the previous (unbounded) behavior for
// users who explicitly opt out.
func TestOllamaHTTPClientWithTimeout_OptOut(t *testing.T) {
	t.Setenv("OLLAMA_HEADERS", "")
	t.Setenv("OLLAMA_TIMEOUT_SECONDS", "0")

	assert.Nil(t, ollamaHTTPClientWithTimeout(),
		"with no headers and timeout disabled, client should be nil (upstream default)")
}

// TestOllamaHTTPClientWithTimeout_ActuallyTimesOut proves end-to-end that a
// stalled server causes the request to fail instead of hanging forever, which
// is the whole point of the fix.
func TestOllamaHTTPClientWithTimeout_ActuallyTimesOut(t *testing.T) {
	t.Setenv("OLLAMA_HEADERS", "")
	t.Setenv("OLLAMA_TIMEOUT_SECONDS", "1")

	// Server that never responds within the timeout window.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := ollamaHTTPClientWithTimeout()
	require.NotNil(t, client)

	start := time.Now()
	_, err := client.Get(server.URL)
	elapsed := time.Since(start)

	require.Error(t, err, "request against a stalled server must return an error, not hang")
	assert.Less(t, elapsed, 3*time.Second, "request must abort at the timeout, well before the server would reply")
}

// TestCreateLLMOllamaAppliesTimeout is the wiring test: the tests above prove
// the client behaves correctly, but not that createLLM actually hands it to the
// model. That gap is how the original bug survived — a timeout only matters if
// it reaches the request.
//
// It guards the metadata path specifically, which #1063 moved from langchaingo
// to Ollama's official client. That model falls back to http.DefaultClient
// (no timeout) when passed nil, so a future refactor dropping the client
// argument would silently restore the freeze this fixes.
//
// The assertion window is deliberately loose. createLLM wraps the model in the
// rate-limited retry layer, and that layer cannot be switched off for the test:
// NewRateLimitedLLM coerces a MaxRetries of 0 back to 3. So one 1s timeout
// becomes four attempts plus exponential backoff, around 11s. What matters is
// that it aborts far short of the server's 30s stall — which is what a missing
// timeout would produce.
func TestCreateLLMOllamaAppliesTimeout(t *testing.T) {
	t.Setenv("OLLAMA_HEADERS", "")
	t.Setenv("OLLAMA_TIMEOUT_SECONDS", "1")

	// Accepts the request, then never answers.
	server := testOllamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(30 * time.Second)
	})
	t.Setenv("OLLAMA_HOST", server.URL)

	previousProvider, previousModel := llmProvider, llmModel
	llmProvider, llmModel = "ollama", "test-model"
	t.Cleanup(func() { llmProvider, llmModel = previousProvider, previousModel })

	model, err := createLLM()
	require.NoError(t, err)

	start := time.Now()
	_, err = model.Call(context.Background(), "Return a title")
	elapsed := time.Since(start)

	require.Error(t, err, "a stalled Ollama server must fail the call, not block the loop")
	assert.Less(t, elapsed, 20*time.Second,
		"the call must abort on the configured timeout, not wait out the stalled server")
}
