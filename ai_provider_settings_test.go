package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAIProviderSettingsSaveLoadDoesNotExposeKey(t *testing.T) {
	db, err := InitializeTestDB()
	require.NoError(t, err)
	app := &App{Database: db, llmCache: &sync.Map{}}
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", "test-secret")

	resp, err := app.saveAIProviderSettings(t.Context(), AIProviderSettingsRequest{
		Provider:     AIProviderOpenAI,
		Enabled:      true,
		DefaultModel: "gpt-4o-mini",
		APIKey:       "sk-secret",
		TaskModels:   map[string]string{TaskTitle: "gpt-4o"},
	})
	require.NoError(t, err)
	require.True(t, resp.APIKeyConfigured)

	loaded, err := app.getAIProviderSettings(t.Context())
	require.NoError(t, err)
	require.True(t, loaded.APIKeyConfigured)
	require.Equal(t, "gpt-4o-mini", loaded.DefaultModel)

	var setting AIProviderSetting
	require.NoError(t, db.First(&setting, "provider = ?", AIProviderOpenAI).Error)
	require.True(t, IsEncryptedSecret(setting.EncryptedAPIKey))
	require.NotContains(t, setting.EncryptedAPIKey, "sk-secret")
}

func TestResolveAIProviderConfigDBOverridesEnv(t *testing.T) {
	db, err := InitializeTestDB()
	require.NoError(t, err)
	app := &App{Database: db, llmCache: &sync.Map{}}
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", "test-secret")
	oldProvider, oldModel := llmProvider, llmModel
	llmProvider = AIProviderOllama
	llmModel = "env-model"
	t.Cleanup(func() {
		llmProvider = oldProvider
		llmModel = oldModel
	})

	_, err = app.saveAIProviderSettings(t.Context(), AIProviderSettingsRequest{
		Provider:     AIProviderOpenRouter,
		Enabled:      true,
		DefaultModel: "openai/gpt-4o-mini",
		APIKey:       "or-secret",
	})
	require.NoError(t, err)

	cfg, err := app.ResolveAIProviderConfig(t.Context())
	require.NoError(t, err)
	require.Equal(t, AIProviderOpenRouter, cfg.Provider)
	require.Equal(t, "openai/gpt-4o-mini", cfg.ModelForTask(TaskTags))
	require.Equal(t, "or-secret", cfg.APIKey)
}

func TestResolveAIProviderConfigEnvFallback(t *testing.T) {
	app := &App{}
	oldProvider, oldModel, oldKey := llmProvider, llmModel, openaiAPIKey
	llmProvider = AIProviderOpenAI
	llmModel = "env-model"
	openaiAPIKey = "env-key"
	t.Cleanup(func() {
		llmProvider = oldProvider
		llmModel = oldModel
		openaiAPIKey = oldKey
	})

	cfg, err := app.ResolveAIProviderConfig(t.Context())
	require.NoError(t, err)
	require.Equal(t, AIProviderOpenAI, cfg.Provider)
	require.Equal(t, "env-model", cfg.ModelForTask(TaskTitle))
	require.Equal(t, "env-key", cfg.APIKey)
}

func TestUnknownProviderRejected(t *testing.T) {
	db, err := InitializeTestDB()
	require.NoError(t, err)
	app := &App{Database: db, llmCache: &sync.Map{}}

	_, err = app.saveAIProviderSettings(t.Context(), AIProviderSettingsRequest{
		Provider: "azure",
		Enabled:  true,
	})
	require.Error(t, err)
}

func TestTaskOverrideChangesSuggestionSourceHash(t *testing.T) {
	doc := Document{ID: 1, Title: "Invoice", Content: "Body"}
	req := GenerateSuggestionsRequest{Documents: []Document{doc}, GenerateTitles: true}
	base := suggestionCacheMetadata{
		Provider:       AIProviderOpenAI,
		Model:          "gpt-4o-mini",
		TaskModels:     map[string]string{TaskTitle: "gpt-4o-mini"},
		PromptVersions: "prompts",
	}
	changed := base
	changed.TaskModels = map[string]string{TaskTitle: "gpt-4o"}

	assert.Equal(t, suggestionSourceHash(doc, req, base), suggestionSourceHash(doc, req, base))
	assert.NotEqual(t, suggestionSourceHash(doc, req, base), suggestionSourceHash(doc, req, changed))
}

func TestOpenRouterHeaders(t *testing.T) {
	appPublicURL = "https://paperless.example"
	t.Cleanup(func() { appPublicURL = "" })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "https://paperless.example", r.Header.Get("HTTP-Referer"))
		require.Equal(t, "paperless-gpt", r.Header.Get("X-Title"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := createOpenRouterHTTPClient().Get(server.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}
