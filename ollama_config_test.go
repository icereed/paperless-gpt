package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

func TestLoadOllamaMetadataConfig(t *testing.T) {
	t.Run("unset file is optional", func(t *testing.T) {
		t.Setenv("OLLAMA_SETTINGS_FILE", "")
		config, err := loadOllamaMetadataConfig()
		require.NoError(t, err)
		assert.Nil(t, config.Prompts)
		assert.Nil(t, config.Defaults.Temperature)
	})

	t.Run("missing explicit file fails", func(t *testing.T) {
		t.Setenv("OLLAMA_SETTINGS_FILE", filepath.Join(t.TempDir(), "missing.json"))
		_, err := loadOllamaMetadataConfig()
		require.Error(t, err)
	})

	for _, testCase := range []struct {
		name     string
		contents string
	}{
		{name: "malformed JSON", contents: `{"defaults":`},
		{name: "non object root", contents: `null`},
		{name: "null defaults", contents: `{"defaults":null}`},
		{name: "null prompts", contents: `{"prompts":null}`},
		{name: "null prompt settings", contents: `{"prompts":{"title_prompt.tmpl":null}}`},
		{name: "null setting", contents: `{"defaults":{"max_tokens":null}}`},
		{name: "unknown top-level field", contents: `{"unknown": true}`},
		{name: "unknown setting", contents: `{"defaults":{"not_a_setting":1}}`},
		{name: "unknown prompt", contents: `{"prompts":{"unknown_prompt.tmpl":{"temperature":0}}}`},
		{name: "OCR prompt is excluded", contents: `{"prompts":{"ocr_prompt.tmpl":{"temperature":0}}}`},
		{name: "format in defaults", contents: `{"defaults":{"format":"json"}}`},
		{name: "format on plain text prompt", contents: `{"prompts":{"title_prompt.tmpl":{"format":"json"}}}`},
		{name: "custom field schema must be an array", contents: `{"prompts":{"custom_field_prompt.tmpl":{"format":{"type":"object"}}}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("OLLAMA_SETTINGS_FILE", writeOllamaSettingsFile(t, testCase.contents))
			_, err := loadOllamaMetadataConfig()
			require.Error(t, err)
		})
	}

	t.Run("keeps explicit zero and false values", func(t *testing.T) {
		t.Setenv("OLLAMA_SETTINGS_FILE", writeOllamaSettingsFile(t, `{
			"defaults": {"temperature": 0, "seed": 0, "think": false, "keep_alive": "0"},
			"prompts": {
				"custom_field_prompt.tmpl": {"max_tokens": -1, "format": {"type": "array"}},
				"adhoc-analysis_prompt.tmpl": {"format": "json"}
			}
		}`))

		config, err := loadOllamaMetadataConfig()
		require.NoError(t, err)
		require.NotNil(t, config.Defaults.Temperature)
		assert.Equal(t, 0.0, *config.Defaults.Temperature)
		require.NotNil(t, config.Defaults.Seed)
		assert.Equal(t, 0, *config.Defaults.Seed)
		assert.JSONEq(t, "false", string(config.Defaults.Think))
		require.NotNil(t, config.Defaults.KeepAlive)
		assert.Equal(t, "0", *config.Defaults.KeepAlive)
		require.NotNil(t, config.Prompts["custom_field_prompt.tmpl"].MaxTokens)
		assert.Equal(t, -1, *config.Prompts["custom_field_prompt.tmpl"].MaxTokens)
		assert.JSONEq(t, `{"type":"array"}`, string(config.Prompts["custom_field_prompt.tmpl"].Format))
	})

	t.Run("permits null inside an adhoc JSON schema", func(t *testing.T) {
		t.Setenv("OLLAMA_SETTINGS_FILE", writeOllamaSettingsFile(t, `{
			"prompts":{"adhoc-analysis_prompt.tmpl":{"format":{"const":null}}}
		}`))
		_, err := loadOllamaMetadataConfig()
		require.NoError(t, err)
	})
}

func TestApplyOllamaMetadataEnvironmentPrecedence(t *testing.T) {
	fileTemperature := 0.1
	fileMaxTokens := 20
	fileContextLength := 512
	fileKeepAlive := "5m"
	config := OllamaMetadataConfig{
		Defaults: OllamaGenerationSettings{
			Temperature:   &fileTemperature,
			MaxTokens:     &fileMaxTokens,
			ContextLength: &fileContextLength,
			KeepAlive:     &fileKeepAlive,
			Think:         json.RawMessage("true"),
		},
		Prompts: map[string]OllamaGenerationSettings{
			"title_prompt.tmpl": {Temperature: float64Pointer(0.2)},
		},
	}

	t.Run("invalid values preserve file defaults", func(t *testing.T) {
		t.Setenv("LLM_TEMPERATURE", "NaN")
		t.Setenv("LLM_MAX_TOKENS", "0")
		t.Setenv("OLLAMA_CONTEXT_LENGTH", "-1")
		t.Setenv("OLLAMA_KEEP_ALIVE", "yesterday")
		t.Setenv("OLLAMA_THINK", "maximum")

		current := config.clone()
		applyOllamaMetadataEnvironment(&current)
		assert.Equal(t, 0.1, *current.Defaults.Temperature)
		assert.Equal(t, 20, *current.Defaults.MaxTokens)
		assert.Equal(t, 512, *current.Defaults.ContextLength)
		assert.Equal(t, "5m", *current.Defaults.KeepAlive)
		assert.JSONEq(t, "true", string(current.Defaults.Think))
		assert.Equal(t, 0.2, *current.Prompts["title_prompt.tmpl"].Temperature)
	})

	t.Run("valid globals overlay defaults but leave prompt values", func(t *testing.T) {
		t.Setenv("LLM_TEMPERATURE", "0")
		t.Setenv("LLM_MAX_TOKENS", "-1")
		t.Setenv("OLLAMA_CONTEXT_LENGTH", "4096")
		t.Setenv("OLLAMA_KEEP_ALIVE", "0")
		t.Setenv("OLLAMA_THINK", "high")

		current := config.clone()
		applyOllamaMetadataEnvironment(&current)
		assert.Equal(t, 0.0, *current.Defaults.Temperature)
		assert.Equal(t, -1, *current.Defaults.MaxTokens)
		assert.Equal(t, 4096, *current.Defaults.ContextLength)
		assert.Equal(t, "0", *current.Defaults.KeepAlive)
		assert.JSONEq(t, `"high"`, string(current.Defaults.Think))
		assert.Equal(t, 0.2, *current.Prompts["title_prompt.tmpl"].Temperature)
	})
}

func TestCreateLLMOllamaAppliesSettingsFileEnvironmentAndCallOptions(t *testing.T) {
	var requests []map[string]any
	server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		requests = append(requests, request)
		_, _ = io.WriteString(w, `{"message":{"content":"title"},"done":true}`+"\n")
	})
	t.Setenv("OLLAMA_HOST", server.URL)
	t.Setenv("OLLAMA_SETTINGS_FILE", writeOllamaSettingsFile(t, `{
		"defaults": {"temperature": 0.1, "max_tokens": 10, "num_ctx": 2048, "keep_alive": "1m", "think": false},
		"prompts": {"title_prompt.tmpl": {"temperature": 0.2, "max_tokens": 20}}
	}`))
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
		withOllamaPrompt(context.Background(), "title_prompt.tmpl"),
		[]llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "Return a title")},
		llms.WithTemperature(0),
	)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, map[string]any{
		"temperature": 0.0,
		"num_predict": 20.0,
		"num_ctx":     4096.0,
	}, requests[0]["options"])
	assert.Equal(t, "0s", requests[0]["keep_alive"])
	assert.Equal(t, "high", requests[0]["think"])
}

func TestCreateLLMOllamaKeepsFileTemperatureWhenEnvironmentIsAbsent(t *testing.T) {
	var request map[string]any
	server := testOllamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		_, _ = io.WriteString(w, `{"message":{"content":"title"},"done":true}`+"\n")
	})
	t.Setenv("OLLAMA_HOST", server.URL)
	t.Setenv("OLLAMA_SETTINGS_FILE", writeOllamaSettingsFile(t, `{"defaults":{"temperature":0.4}}`))
	t.Setenv("LLM_TEMPERATURE", "")
	t.Setenv("LLM_MAX_TOKENS", "")
	t.Setenv("OLLAMA_CONTEXT_LENGTH", "")
	t.Setenv("OLLAMA_KEEP_ALIVE", "")
	t.Setenv("OLLAMA_THINK", "")
	previousProvider, previousModel := llmProvider, llmModel
	llmProvider, llmModel = "ollama", "test-model"
	t.Cleanup(func() { llmProvider, llmModel = previousProvider, previousModel })

	model, err := createLLM()
	require.NoError(t, err)
	_, err = model.Call(context.Background(), "Return a title")
	require.NoError(t, err)
	assert.Equal(t, 0.4, request["options"].(map[string]any)["temperature"])
}

func TestOllamaSettingsFileDoesNotAffectOtherMetadataProviders(t *testing.T) {
	server := testOllamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"title"}}]}`)
	})
	t.Setenv("OLLAMA_SETTINGS_FILE", filepath.Join(t.TempDir(), "does-not-exist.json"))
	t.Setenv("OPENAI_BASE_URL", server.URL)
	previousProvider, previousModel, previousKey := llmProvider, llmModel, openaiAPIKey
	llmProvider, llmModel, openaiAPIKey = "openai", "test-model", "test-key"
	t.Cleanup(func() { llmProvider, llmModel, openaiAPIKey = previousProvider, previousModel, previousKey })

	_, err := createLLM()
	require.NoError(t, err)
}

func writeOllamaSettingsFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ollama-settings.json")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func float64Pointer(value float64) *float64 {
	return &value
}
