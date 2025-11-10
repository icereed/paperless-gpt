package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestCreateLLMWithOpenAICompatible tests that OpenAI-compatible services work without API keys
func TestCreateLLMWithOpenAICompatible(t *testing.T) {
	// Save original env vars and restore after test
	origProvider := llmProvider
	origModel := llmModel
	origAPIKey := openaiAPIKey
	defer func() {
		llmProvider = origProvider
		llmModel = origModel
		openaiAPIKey = origAPIKey
		t.Setenv("OPENAI_API_KEY", "")
		t.Setenv("OPENAI_BASE_URL", "")
	}()

	tests := []struct {
		name        string
		apiKey      string
		baseURL     string
		shouldError bool
	}{
		{
			name:        "OpenAI-compatible with base URL and no API key",
			apiKey:      "",
			baseURL:     "http://localhost:1234/v1",
			shouldError: false,
		},
		{
			name:        "OpenAI-compatible with base URL and API key",
			apiKey:      "test-key",
			baseURL:     "http://localhost:1234/v1",
			shouldError: false,
		},
		{
			name:        "Standard OpenAI with API key and no base URL",
			apiKey:      "sk-test-key",
			baseURL:     "",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			t.Setenv("OPENAI_API_KEY", tt.apiKey)
			t.Setenv("OPENAI_BASE_URL", tt.baseURL)
			
			// Update global vars
			llmProvider = "openai"
			llmModel = "test-model"
			openaiAPIKey = tt.apiKey

			// Create LLM client
			llm, err := createLLM()

			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, llm)
			}
		})
	}
}

// TestCreateVisionLLMWithOpenAICompatible tests that OpenAI-compatible services work without API keys for vision models
func TestCreateVisionLLMWithOpenAICompatible(t *testing.T) {
	// Save original env vars and restore after test
	origProvider := visionLlmProvider
	origModel := visionLlmModel
	origAPIKey := openaiAPIKey
	defer func() {
		visionLlmProvider = origProvider
		visionLlmModel = origModel
		openaiAPIKey = origAPIKey
		t.Setenv("OPENAI_API_KEY", "")
		t.Setenv("OPENAI_BASE_URL", "")
	}()

	tests := []struct {
		name        string
		apiKey      string
		baseURL     string
		shouldError bool
	}{
		{
			name:        "OpenAI-compatible vision with base URL and no API key",
			apiKey:      "",
			baseURL:     "http://localhost:1234/v1",
			shouldError: false,
		},
		{
			name:        "OpenAI-compatible vision with base URL and API key",
			apiKey:      "test-key",
			baseURL:     "http://localhost:1234/v1",
			shouldError: false,
		},
		{
			name:        "Standard OpenAI vision with API key and no base URL",
			apiKey:      "sk-test-key",
			baseURL:     "",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			t.Setenv("OPENAI_API_KEY", tt.apiKey)
			t.Setenv("OPENAI_BASE_URL", tt.baseURL)
			
			// Update global vars
			visionLlmProvider = "openai"
			visionLlmModel = "test-vision-model"
			openaiAPIKey = tt.apiKey

			// Create Vision LLM client
			llm, err := createVisionLLM()

			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, llm)
			}
		})
	}
}
