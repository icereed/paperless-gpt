package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	langchainollama "github.com/tmc/langchaingo/llms/ollama"
)

func TestOllamaMetadataModelWireRequestAndResponse(t *testing.T) {
	var request map[string]any
	var receivedHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/chat", r.URL.Path)
		receivedHeader = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"answer","thinking":"reasoning"},"done":true,"done_reason":"stop","eval_count":4,"prompt_eval_count":6}`+"\n")
	}))
	defer server.Close()

	think := false
	model, err := newOllamaMetadataModel(server.URL, "metadata-model", 8192, &think, 0, &http.Client{Transport: testHeaderTransport{base: http.DefaultTransport}})
	require.NoError(t, err)
	response, err := model.GenerateContent(context.Background(), []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, "system"),
		llms.TextParts(llms.ChatMessageTypeHuman, "Return ", "a title"),
		llms.TextParts(llms.ChatMessageTypeAI, "previous"),
	}, llms.WithMaxTokens(200), llms.WithStopWords([]string{"END"}), llms.WithJSONMode(), llms.WithTopK(20), llms.WithTopP(0.8), llms.WithSeed(42), llms.WithRepetitionPenalty(1.1), llms.WithFrequencyPenalty(0.2), llms.WithPresencePenalty(0.3))
	require.NoError(t, err)
	require.Len(t, response.Choices, 1)
	assert.Equal(t, "answer", response.Choices[0].Content)
	assert.Equal(t, "reasoning", response.Choices[0].ReasoningContent)
	assert.Equal(t, "stop", response.Choices[0].StopReason)
	assert.Equal(t, map[string]any{"CompletionTokens": 4, "PromptTokens": 6, "TotalTokens": 10}, response.Choices[0].GenerationInfo)
	assert.Equal(t, "Bearer a=b", receivedHeader)
	assert.Equal(t, "metadata-model", request["model"])
	assert.Equal(t, false, request["stream"])
	assert.Equal(t, false, request["think"])
	assert.Equal(t, "json", request["format"])
	assert.Equal(t, []any{
		map[string]any{"role": "system", "content": "system"},
		map[string]any{"role": "user", "content": "Return a title"},
		map[string]any{"role": "assistant", "content": "previous"},
	}, request["messages"])
	assert.Equal(t, map[string]any{
		"temperature":       0.0,
		"num_ctx":           8192.0,
		"num_predict":       200.0,
		"stop":              []any{"END"},
		"top_k":             20.0,
		"top_p":             0.8,
		"seed":              42.0,
		"repeat_penalty":    1.1,
		"frequency_penalty": 0.2,
		"presence_penalty":  0.3,
	}, request["options"])
}

func TestOllamaMetadataModelThinkTriState(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		think *bool
		want  any
	}{
		{name: "unset", want: nil},
		{name: "true", think: boolPointer(true), want: true},
		{name: "false", think: boolPointer(false), want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var request map[string]any
			server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				_, _ = io.WriteString(w, `{"message":{"content":"answer"},"done":true}`+"\n")
			})
			model, err := newOllamaMetadataModel(server.URL, "model", 0, testCase.think, 0, nil)
			require.NoError(t, err)
			_, err = model.Call(context.Background(), "prompt")
			require.NoError(t, err)
			if testCase.want == nil {
				assert.NotContains(t, request, "think")
			} else {
				assert.Equal(t, testCase.want, request["think"])
			}
			assert.NotContains(t, request["options"].(map[string]any), "think")
		})
	}
}

func TestExistingLangChainOllamaSendsZeroTemperature(t *testing.T) {
	var request map[string]any
	server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		_, _ = io.WriteString(w, `{"message":{"content":"title"},"done":true}`+"\n")
	})
	model, err := langchainollama.New(langchainollama.WithModel("test-model"), langchainollama.WithServerURL(server.URL))
	require.NoError(t, err)
	response, err := model.Call(context.Background(), "Return a title")
	require.NoError(t, err)
	assert.Equal(t, "title", response)
	assert.Equal(t, 0.0, request["options"].(map[string]any)["temperature"])
}

func TestCreateLLMOllamaUsesNativeClientAndBaselineTemperature(t *testing.T) {
	var request map[string]any
	var authorization string
	server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		_, _ = io.WriteString(w, `{"message":{"content":"title"},"done":true}`+"\n")
	})
	t.Setenv("OLLAMA_HOST", server.URL)
	t.Setenv("OLLAMA_HEADERS", "Authorization=Bearer a=b")
	t.Setenv("OLLAMA_CONTEXT_LENGTH", "4096")
	t.Setenv("OLLAMA_THINK", "false")
	previousProvider, previousModel := llmProvider, llmModel
	llmProvider, llmModel = "ollama", "test-model"
	t.Cleanup(func() { llmProvider, llmModel = previousProvider, previousModel })

	model, err := createLLM()
	require.NoError(t, err)
	_, ok := model.(*RateLimitedLLM)
	require.True(t, ok, "the existing rate-limit wrapper must remain in place")
	response, err := model.Call(context.Background(), "Return a title")
	require.NoError(t, err)
	assert.Equal(t, "title", response)
	assert.Equal(t, "Bearer a=b", authorization)
	assert.Equal(t, "test-model", request["model"])
	assert.Equal(t, false, request["stream"])
	assert.Equal(t, false, request["think"])
	assert.Equal(t, 0.0, request["options"].(map[string]any)["temperature"])
	assert.Equal(t, 4096.0, request["options"].(map[string]any)["num_ctx"])
}

func TestCreateLLMOllamaTemperature(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		value string
		want  float64
	}{
		{name: "unset", want: 0},
		{name: "empty", value: "", want: 0},
		{name: "zero", value: "0", want: 0},
		{name: "override", value: "0.2", want: 0.2},
		{name: "invalid", value: "not-a-number", want: 0},
		{name: "negative", value: "-0.1", want: 0},
		{name: "NaN", value: "NaN", want: 0},
		{name: "Inf", value: "Inf", want: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var request map[string]any
			server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				_, _ = io.WriteString(w, `{"message":{"content":"title"},"done":true}`+"\n")
			})
			t.Setenv("OLLAMA_HOST", server.URL)
			t.Setenv("LLM_TEMPERATURE", testCase.value)
			previousProvider, previousModel := llmProvider, llmModel
			llmProvider, llmModel = "ollama", "test-model"
			t.Cleanup(func() { llmProvider, llmModel = previousProvider, previousModel })

			model, err := createLLM()
			require.NoError(t, err)
			_, err = model.Call(context.Background(), "Return a title")
			require.NoError(t, err)
			assert.Equal(t, testCase.want, request["options"].(map[string]any)["temperature"])
		})
	}
}

func TestOllamaMetadataModelCallTemperatureOverridesConfiguredValue(t *testing.T) {
	var temperatures []float64
	server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		temperatures = append(temperatures, request["options"].(map[string]any)["temperature"].(float64))
		_, _ = io.WriteString(w, `{"message":{"content":"title"},"done":true}`+"\n")
	})
	model, err := newOllamaMetadataModel(server.URL, "model", 0, nil, 0.7, nil)
	require.NoError(t, err)

	for _, options := range [][]llms.CallOption{nil, {llms.WithTemperature(0)}, {llms.WithTemperature(0.2)}} {
		_, err := model.Call(context.Background(), "Return a title", options...)
		require.NoError(t, err)
	}
	assert.Equal(t, []float64{0.7, 0, 0.2}, temperatures)
}

func TestLLMTemperatureDoesNotAffectOllamaVision(t *testing.T) {
	var request map[string]any
	server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		_, _ = io.WriteString(w, `{"message":{"content":"OCR result"},"done":true}`+"\n")
	})
	t.Setenv("OLLAMA_HOST", server.URL)
	t.Setenv("LLM_TEMPERATURE", "0.2")
	previousProvider, previousModel := visionLlmProvider, visionLlmModel
	visionLlmProvider, visionLlmModel = "ollama", "vision-model"
	t.Cleanup(func() { visionLlmProvider, visionLlmModel = previousProvider, previousModel })

	model, err := createVisionLLM()
	require.NoError(t, err)
	_, err = model.Call(context.Background(), "Read this image")
	require.NoError(t, err)
	assert.Equal(t, 0.0, request["options"].(map[string]any)["temperature"])
}

func TestLLMTemperatureDoesNotAffectOpenAI(t *testing.T) {
	var request map[string]any
	server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"title"}}]}`)
	})
	t.Setenv("OPENAI_BASE_URL", server.URL)
	t.Setenv("LLM_TEMPERATURE", "0.2")
	previousProvider, previousModel, previousKey := llmProvider, llmModel, openaiAPIKey
	llmProvider, llmModel, openaiAPIKey = "openai", "test-model", "test-key"
	t.Cleanup(func() { llmProvider, llmModel, openaiAPIKey = previousProvider, previousModel, previousKey })

	model, err := createLLM()
	require.NoError(t, err)
	_, err = model.Call(context.Background(), "Return a title")
	require.NoError(t, err)
	assert.Equal(t, 0.0, request["temperature"])
}

func TestOllamaMetadataModelStreamingKeepsThinkingSeparate(t *testing.T) {
	server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, true, request["stream"])
		_, _ = io.WriteString(w, `{"message":{"content":"final ","thinking":"first"},"done":false}`+"\n")
		_, _ = io.WriteString(w, `{"message":{"content":"answer","thinking":" second"},"done":true,"done_reason":"stop"}`+"\n")
	})
	model, err := newOllamaMetadataModel(server.URL, "model", 0, nil, 0, nil)
	require.NoError(t, err)
	var contentChunks, reasoningChunks []string
	response, err := model.GenerateContent(context.Background(), []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "prompt")},
		llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
			contentChunks = append(contentChunks, string(chunk))
			return nil
		}),
		llms.WithStreamingReasoningFunc(func(_ context.Context, reasoningChunk, contentChunk []byte) error {
			reasoningChunks = append(reasoningChunks, fmt.Sprintf("%s|%s", reasoningChunk, contentChunk))
			return nil
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"final ", "answer"}, contentChunks)
	assert.Equal(t, []string{"first|final ", " second|answer"}, reasoningChunks)
	assert.Equal(t, "final answer", response.Choices[0].Content)
	assert.Equal(t, "first second", response.Choices[0].ReasoningContent)
}

func TestOllamaMetadataModelReturnsUsefulErrors(t *testing.T) {
	t.Run("callback", func(t *testing.T) {
		server := testOllamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"message":{"content":"chunk"},"done":false}`+"\n")
		})
		model, err := newOllamaMetadataModel(server.URL, "model", 0, nil, 0, nil)
		require.NoError(t, err)
		callbackErr := errors.New("stop streaming")
		_, err = model.GenerateContent(context.Background(), []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "prompt")}, llms.WithStreamingFunc(func(context.Context, []byte) error { return callbackErr }))
		require.ErrorIs(t, err, callbackErr)
	})

	for _, testCase := range []struct {
		name string
		body string
		want string
	}{
		{name: "invalid JSON", body: "not-json\n", want: "not-json"},
		{name: "incomplete", body: `{"message":{"content":"partial"},"done":false}` + "\n", want: "ended before completion"},
		{name: "thinking only", body: `{"message":{"thinking":"internal"},"done":true}` + "\n", want: "contained no content"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := testOllamaServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, testCase.body) })
			model, err := newOllamaMetadataModel(server.URL, "model", 0, nil, 0, nil)
			require.NoError(t, err)
			_, err = model.Call(context.Background(), "prompt")
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.want)
		})
	}

	t.Run("http status", func(t *testing.T) {
		server := testOllamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"error":"upstream unavailable"}`+"\n")
		})
		model, err := newOllamaMetadataModel(server.URL, "model", 0, nil, 0, nil)
		require.NoError(t, err)
		_, err = model.Call(context.Background(), "prompt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "upstream unavailable")
	})

	t.Run("context cancellation", func(t *testing.T) {
		started := make(chan struct{})
		server := testOllamaServer(t, func(_ http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			close(started)
			<-r.Context().Done()
		})
		model, err := newOllamaMetadataModel(server.URL, "model", 0, nil, 0, nil)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := model.Call(ctx, "prompt"); done <- err }()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("Ollama request did not reach the test server")
		}
		cancel()
		select {
		case callErr := <-done:
			require.ErrorIs(t, callErr, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("cancelled Ollama request did not return")
		}
	})
}

func TestOllamaMetadataModelRejectsUnsupportedMessagesAndTools(t *testing.T) {
	model, err := newOllamaMetadataModel("http://127.0.0.1:11434", "model", 0, nil, 0, nil)
	require.NoError(t, err)
	_, err = model.GenerateContent(context.Background(), []llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.BinaryPart("image/png", []byte("image"))}}})
	assert.ErrorContains(t, err, "does not support llms.BinaryContent")
	_, err = model.GenerateContent(context.Background(), []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "prompt")}, llms.WithTools([]llms.Tool{{Type: "function"}}))
	assert.ErrorContains(t, err, "does not support tool requests")
	_, err = newOllamaMetadataModel("http://127.0.0.1:11434", "model", 0, nil, math.NaN(), nil)
	assert.ErrorContains(t, err, "invalid Ollama temperature")
}

func testOllamaServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func boolPointer(value bool) *bool {
	return &value
}

type testHeaderTransport struct {
	base http.RoundTripper
}

func (t testHeaderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer a=b")
	return t.base.RoundTrip(request)
}
