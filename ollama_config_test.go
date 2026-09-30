package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

func TestApplyOllamaMetadataEnvironment(t *testing.T) {
	// A non-empty starting point, so "invalid input is ignored" can be told
	// apart from "invalid input resets to zero".
	baseline := func() OllamaMetadataConfig {
		return OllamaMetadataConfig{
			Defaults: OllamaGenerationSettings{
				Temperature:   floatPointer(0.1),
				MaxTokens:     intPointer(20),
				ContextLength: intPointer(512),
				KeepAlive:     stringPointer("5m"),
				Think:         json.RawMessage("true"),
			},
		}
	}

	t.Run("invalid values are ignored rather than applied", func(t *testing.T) {
		t.Setenv("LLM_TEMPERATURE", "NaN")
		t.Setenv("LLM_MAX_TOKENS", "0")
		t.Setenv("OLLAMA_CONTEXT_LENGTH", "-1")
		t.Setenv("OLLAMA_KEEP_ALIVE", "yesterday")
		t.Setenv("OLLAMA_THINK", "maximum")

		current := baseline()
		applyOllamaMetadataEnvironment(&current)
		assert.Equal(t, 0.1, *current.Defaults.Temperature)
		assert.Equal(t, 20, *current.Defaults.MaxTokens)
		assert.Equal(t, 512, *current.Defaults.ContextLength)
		assert.Equal(t, "5m", *current.Defaults.KeepAlive)
		assert.JSONEq(t, "true", string(current.Defaults.Think))
	})

	t.Run("valid values overlay the defaults", func(t *testing.T) {
		t.Setenv("LLM_TEMPERATURE", "0")
		t.Setenv("LLM_MAX_TOKENS", "-1")
		t.Setenv("OLLAMA_CONTEXT_LENGTH", "4096")
		t.Setenv("OLLAMA_KEEP_ALIVE", "0")
		t.Setenv("OLLAMA_THINK", "high")

		current := baseline()
		applyOllamaMetadataEnvironment(&current)
		// Explicit zero and -1 have to survive: they are meaningful values
		// here, not "unset".
		assert.Equal(t, 0.0, *current.Defaults.Temperature)
		assert.Equal(t, -1, *current.Defaults.MaxTokens)
		assert.Equal(t, 4096, *current.Defaults.ContextLength)
		assert.Equal(t, "0", *current.Defaults.KeepAlive)
		assert.JSONEq(t, `"high"`, string(current.Defaults.Think))
	})
}

// TestCreateLLMOllamaSendsThinkAtTopLevel is the regression test for #1024 and
// #1056: OLLAMA_THINK had no effect because langchaingo nests `think` inside
// the request's `options` object, where the Ollama server ignores it, and drops
// the value entirely when it is false. Both are asserted on the actual wire
// payload rather than on our own config structs, since that is where the bug
// lived.
func TestCreateLLMOllamaSendsThinkAtTopLevel(t *testing.T) {
	for _, tc := range []struct {
		name      string
		env       string
		wantThink any
	}{
		{name: "think false is transmitted", env: "false", wantThink: false},
		{name: "think true is transmitted", env: "true", wantThink: true},
		{name: "reasoning level is transmitted", env: "high", wantThink: "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request map[string]any
			server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				_, _ = io.WriteString(w, `{"message":{"content":"title"},"done":true,"done_reason":"stop"}`+"\n")
			})
			t.Setenv("OLLAMA_HOST", server.URL)
			t.Setenv("OLLAMA_THINK", tc.env)
			previousProvider, previousModel := llmProvider, llmModel
			llmProvider, llmModel = "ollama", "test-model"
			t.Cleanup(func() { llmProvider, llmModel = previousProvider, previousModel })

			model, err := createLLM()
			require.NoError(t, err)
			_, err = model.Call(context.Background(), "Return a title")
			require.NoError(t, err)

			assert.Equal(t, tc.wantThink, request["think"],
				"think must be a top-level request field")
			if options, ok := request["options"].(map[string]any); ok {
				assert.NotContains(t, options, "think",
					"think inside options is silently ignored by Ollama")
			}
		})
	}
}

func TestCreateLLMOllamaAppliesEnvironmentAndCallOptions(t *testing.T) {
	var requests []map[string]any
	server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		requests = append(requests, request)
		_, _ = io.WriteString(w, `{"message":{"content":"title"},"done":true,"done_reason":"stop"}`+"\n")
	})
	t.Setenv("OLLAMA_HOST", server.URL)
	t.Setenv("LLM_TEMPERATURE", "0.3")
	t.Setenv("LLM_MAX_TOKENS", "30")
	t.Setenv("OLLAMA_CONTEXT_LENGTH", "4096")
	t.Setenv("OLLAMA_KEEP_ALIVE", "0")
	t.Setenv("OLLAMA_THINK", "high")
	previousProvider, previousModel := llmProvider, llmModel
	llmProvider, llmModel = "ollama", "test-model"
	t.Cleanup(func() { llmProvider, llmModel = previousProvider, previousModel })

	model, err := createLLM()
	require.NoError(t, err)
	_, err = model.GenerateContent(
		context.Background(),
		[]llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "Return a title")},
		// An explicit call option wins over the environment, and an explicit
		// zero must not be mistaken for "not set".
		llms.WithTemperature(0),
	)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, map[string]any{
		"temperature": 0.0,
		"num_predict": 30.0,
		"num_ctx":     4096.0,
	}, requests[0]["options"])
	assert.Equal(t, "0s", requests[0]["keep_alive"])
	assert.Equal(t, "high", requests[0]["think"])
}

// The Ollama-only settings must not leak into another provider's client
// construction.
func TestOllamaSettingsDoNotAffectOtherMetadataProviders(t *testing.T) {
	server := testOllamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"title"}}]}`)
	})
	t.Setenv("LLM_TEMPERATURE", "0.3")
	t.Setenv("OLLAMA_THINK", "high")
	t.Setenv("OPENAI_BASE_URL", server.URL)
	previousProvider, previousModel, previousKey := llmProvider, llmModel, openaiAPIKey
	llmProvider, llmModel, openaiAPIKey = "openai", "test-model", "test-key"
	t.Cleanup(func() { llmProvider, llmModel, openaiAPIKey = previousProvider, previousModel, previousKey })

	_, err := createLLM()
	require.NoError(t, err)
}
