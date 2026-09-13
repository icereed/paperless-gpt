package main

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

func TestOllamaMetadataSettingsMergeAndExplicitZeros(t *testing.T) {
	var requests []map[string]any
	var lock sync.Mutex
	server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		lock.Lock()
		requests = append(requests, request)
		lock.Unlock()
		_, _ = io.WriteString(w, `{"message":{"content":"answer"},"done":true}`+"\n")
	})
	defaultsStop := []string{"default"}
	model, err := newOllamaMetadataModel(server.URL, "model", 4096, boolPointer(true), 0.7, nil, OllamaMetadataConfig{
		Defaults: OllamaGenerationSettings{MaxTokens: intPointer(120), TopP: floatPointer(0.9), Seed: intPointer(9), Stop: &defaultsStop},
	})
	require.NoError(t, err)

	_, err = model.Call(context.Background(), "defaults")
	require.NoError(t, err)
	// Explicit call options win over the configured defaults, and an explicit
	// zero (or empty list) must not be mistaken for "not set" and dropped.
	_, err = model.Call(context.Background(), "override", llms.WithTemperature(0), llms.WithTopP(0), llms.WithSeed(0), llms.WithStopWords([]string{}), llms.WithMaxTokens(0))
	require.NoError(t, err)

	require.Len(t, requests, 2)
	assert.Equal(t, map[string]any{"temperature": 0.7, "num_ctx": 4096.0, "num_predict": 120.0, "top_p": 0.9, "seed": 9.0, "stop": []any{"default"}}, requests[0]["options"])
	assert.Equal(t, map[string]any{"temperature": 0.0, "num_ctx": 4096.0, "top_p": 0.0, "seed": 0.0, "stop": []any{}}, requests[1]["options"])
}

// Concurrent calls must not see each other's per-call options: the model is a
// single shared instance, so a settings overlay that mutated shared state would
// corrupt whichever request happened to interleave.
func TestOllamaMetadataCallOptionsDoNotLeakAcrossConcurrentCalls(t *testing.T) {
	requests := make(map[string]float64)
	var lock sync.Mutex
	server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
			Options map[string]any `json:"options"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		lock.Lock()
		requests[request.Messages[0].Content] = request.Options["temperature"].(float64)
		lock.Unlock()
		_, _ = io.WriteString(w, `{"message":{"content":"answer"},"done":true}`+"\n")
	})
	model, err := newOllamaMetadataModel(server.URL, "model", 0, nil, 0.1, nil, OllamaMetadataConfig{})
	require.NoError(t, err)
	var group sync.WaitGroup
	for prompt, temperature := range map[string]float64{"one": 0.2, "two": 0.8} {
		group.Add(1)
		go func(prompt string, temperature float64) {
			defer group.Done()
			_, callErr := model.Call(context.Background(), prompt, llms.WithTemperature(temperature))
			require.NoError(t, callErr)
		}(prompt, temperature)
	}
	group.Wait()
	assert.Equal(t, map[string]float64{"one": 0.2, "two": 0.8}, requests)
}

func TestOllamaMetadataKeepAliveThinkAndFormatWireValues(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		settings   OllamaGenerationSettings
		wantKeep   any
		wantThink  any
		wantFormat any
	}{
		{name: "zero keep alive and false think", settings: OllamaGenerationSettings{KeepAlive: stringPointer("0"), Think: json.RawMessage("false")}, wantKeep: "0s", wantThink: false},
		{name: "indefinite keep alive and high thinking", settings: OllamaGenerationSettings{KeepAlive: stringPointer("-1"), Think: json.RawMessage(`"high"`)}, wantKeep: -1.0, wantThink: "high"},
		{name: "medium thinking", settings: OllamaGenerationSettings{Think: json.RawMessage(`"medium"`)}, wantThink: "medium"},
		{name: "low thinking", settings: OllamaGenerationSettings{Think: json.RawMessage(`"low"`)}, wantThink: "low"},
		{name: "json format", settings: OllamaGenerationSettings{Format: json.RawMessage(`"json"`)}, wantFormat: "json"},
		{name: "schema format", settings: OllamaGenerationSettings{Format: json.RawMessage(`{"type":"object"}`)}, wantFormat: map[string]any{"type": "object"}},
		{name: "omitted think", settings: OllamaGenerationSettings{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var request map[string]any
			server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				content := "answer"
				if testCase.wantFormat != nil {
					content = `{}`
				}
				_, _ = io.WriteString(w, `{"message":{"content":`+mustJSON(t, content)+`},"done":true}`+"\n")
			})
			model, err := newOllamaMetadataModel(server.URL, "model", 0, nil, 0, nil, OllamaMetadataConfig{Defaults: testCase.settings})
			require.NoError(t, err)
			_, err = model.Call(context.Background(), "prompt")
			require.NoError(t, err)
			if testCase.wantKeep == nil {
				assert.NotContains(t, request, "keep_alive")
			} else {
				assert.Equal(t, testCase.wantKeep, request["keep_alive"])
			}
			if testCase.wantThink == nil {
				assert.NotContains(t, request, "think")
			} else {
				assert.Equal(t, testCase.wantThink, request["think"])
			}
			if testCase.wantFormat == nil {
				assert.NotContains(t, request, "format")
			} else {
				assert.Equal(t, testCase.wantFormat, request["format"])
			}
		})
	}
}

func TestOllamaMetadataRejectsInvalidSettingsAndStructuredResponses(t *testing.T) {
	for _, settings := range []OllamaGenerationSettings{
		{MaxTokens: intPointer(0)}, {ContextLength: intPointer(0)}, {TopP: floatPointer(1.1)}, {RepeatLastN: intPointer(-2)},
		{KeepAlive: stringPointer("forever")}, {Think: json.RawMessage(`"max"`)}, {Format: json.RawMessage(`{}`)},
	} {
		_, err := newOllamaMetadataModel("http://127.0.0.1:11434", "model", 0, nil, 0, nil, OllamaMetadataConfig{Defaults: settings})
		require.Error(t, err)
	}
	for _, content := range []string{"not-json", `{"truncated":`} {
		server := testOllamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"message":{"content":`+mustJSON(t, content)+`},"done":true}`+"\n")
		})
		model, err := newOllamaMetadataModel(server.URL, "model", 0, nil, 0, nil, OllamaMetadataConfig{Defaults: OllamaGenerationSettings{Format: json.RawMessage(`"json"`)}})
		require.NoError(t, err)
		_, err = model.Call(context.Background(), "prompt")
		assert.ErrorContains(t, err, "structured response was not valid JSON")
	}
}

func TestOllamaMetadataCallOptionsRunOnceAndValidateFinalSettings(t *testing.T) {
	server := testOllamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"message":{"content":"answer"},"done":true}`+"\n")
	})
	model, err := newOllamaMetadataModel(server.URL, "model", 0, nil, 0, nil, OllamaMetadataConfig{Defaults: OllamaGenerationSettings{Temperature: floatPointer(0.5)}})
	require.NoError(t, err)
	called := 0
	_, err = model.Call(context.Background(), "prompt", func(options *llms.CallOptions) { called++; options.Temperature = 0 })
	require.NoError(t, err)
	assert.Equal(t, 1, called)
	_, err = model.Call(context.Background(), "prompt", llms.WithTemperature(-1))
	assert.ErrorContains(t, err, "temperature")
	_, err = model.Call(context.Background(), "prompt", llms.WithTemperature(math.Inf(1)))
	assert.ErrorContains(t, err, "finite")
	_, err = model.Call(context.Background(), "prompt", llms.WithTemperature(math.NaN()))
	assert.ErrorContains(t, err, "finite")
}

func floatPointer(value float64) *float64 { return &value }
func intPointer(value int) *int           { return &value }
func stringPointer(value string) *string  { return &value }

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}
