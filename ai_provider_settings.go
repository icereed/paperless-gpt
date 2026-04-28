package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"gorm.io/gorm"
)

const (
	AIProviderOpenAI     = "openai"
	AIProviderOpenRouter = "openrouter"
	AIProviderOllama     = "ollama"
	AIProviderGoogleAI   = "googleai"

	TaskTitle         = "title"
	TaskTags          = "tags"
	TaskCorrespondent = "correspondent"
	TaskDocumentType  = "document_type"
	TaskCreatedDate   = "created_date"
	TaskCustomFields  = "custom_fields"
)

var (
	allowedUIAIProviders = map[string]struct{}{
		AIProviderOpenAI:     {},
		AIProviderOpenRouter: {},
		AIProviderOllama:     {},
		AIProviderGoogleAI:   {},
	}
	allowedAITaskModels = map[string]struct{}{
		TaskTitle:         {},
		TaskTags:          {},
		TaskCorrespondent: {},
		TaskDocumentType:  {},
		TaskCreatedDate:   {},
		TaskCustomFields:  {},
	}
)

type AIProviderConfig struct {
	Provider     string            `json:"provider"`
	BaseURL      string            `json:"base_url,omitempty"`
	APIKey       string            `json:"-"`
	DefaultModel string            `json:"default_model"`
	TaskModels   map[string]string `json:"task_models"`
	Source       string            `json:"source,omitempty"`
}

func (cfg *AIProviderConfig) ModelForTask(task string) string {
	if cfg == nil {
		return ""
	}
	key := strings.ToLower(strings.TrimSpace(task))
	if key != "" {
		if model := strings.TrimSpace(cfg.TaskModels[key]); model != "" {
			return model
		}
	}
	return strings.TrimSpace(cfg.DefaultModel)
}

type AIProviderSettingsResponse struct {
	Provider         string            `json:"provider"`
	Enabled          bool              `json:"enabled"`
	BaseURL          string            `json:"base_url"`
	DefaultModel     string            `json:"default_model"`
	APIKeyConfigured bool              `json:"api_key_configured"`
	TaskModels       map[string]string `json:"task_models"`
	Source           string            `json:"source,omitempty"`
}

type AIProviderSettingsRequest struct {
	Provider     string            `json:"provider"`
	Enabled      bool              `json:"enabled"`
	BaseURL      string            `json:"base_url"`
	DefaultModel string            `json:"default_model"`
	APIKey       string            `json:"api_key"`
	TaskModels   map[string]string `json:"task_models"`
}

type llmCacheEntry struct {
	llm llms.Model
}

func normalizeUIProvider(provider string) (string, error) {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "google" || p == "google_ai" {
		p = AIProviderGoogleAI
	}
	if _, ok := allowedUIAIProviders[p]; !ok {
		return "", fmt.Errorf("unsupported AI provider %q", provider)
	}
	return p, nil
}

func normalizeTaskModels(in map[string]string) (map[string]string, error) {
	result := map[string]string{}
	for key, value := range in {
		task := strings.ToLower(strings.TrimSpace(key))
		if _, ok := allowedAITaskModels[task]; !ok {
			return nil, fmt.Errorf("unsupported task model override %q", key)
		}
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result[task] = trimmed
		}
	}
	return result, nil
}

func defaultBaseURLForProvider(provider string) string {
	switch provider {
	case AIProviderOpenRouter:
		return "https://openrouter.ai/api/v1"
	case AIProviderOllama:
		if host := strings.TrimSpace(os.Getenv("OLLAMA_HOST")); host != "" {
			return host
		}
		return "http://127.0.0.1:11434"
	default:
		return ""
	}
}

func (app *App) getEnabledUIProviderSetting(ctx context.Context) (*AIProviderSetting, error) {
	if app == nil || app.Database == nil {
		return nil, nil
	}
	var settings []AIProviderSetting
	err := app.Database.WithContext(ctx).
		Where("enabled = ?", true).
		Order("updated_at DESC, id DESC").
		Find(&settings).Error
	if err != nil {
		return nil, err
	}
	for _, setting := range settings {
		provider, err := normalizeUIProvider(setting.Provider)
		if err != nil {
			continue
		}
		setting.Provider = provider
		if setting.DefaultModel != "" || setting.BaseURL != "" || setting.EncryptedAPIKey != "" {
			return &setting, nil
		}
	}
	return nil, nil
}

func (app *App) ResolveAIProviderConfig(ctx context.Context) (*AIProviderConfig, error) {
	if setting, err := app.getEnabledUIProviderSetting(ctx); err != nil {
		return nil, err
	} else if setting != nil {
		taskModels := map[string]string{}
		if strings.TrimSpace(setting.TaskModelsJSON) != "" {
			if err := json.Unmarshal([]byte(setting.TaskModelsJSON), &taskModels); err != nil {
				return nil, fmt.Errorf("invalid task model settings: %w", err)
			}
			taskModels, err = normalizeTaskModels(taskModels)
			if err != nil {
				return nil, err
			}
		}
		apiKey := ""
		if setting.EncryptedAPIKey != "" {
			decrypted, err := DecryptSecret(setting.EncryptedAPIKey)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt AI provider API key: %w", err)
			}
			apiKey = decrypted
		}
		baseURL := strings.TrimSpace(setting.BaseURL)
		if baseURL == "" {
			baseURL = defaultBaseURLForProvider(setting.Provider)
		}
		return &AIProviderConfig{
			Provider:     setting.Provider,
			BaseURL:      baseURL,
			APIKey:       apiKey,
			DefaultModel: strings.TrimSpace(setting.DefaultModel),
			TaskModels:   taskModels,
			Source:       "ui",
		}, nil
	}

	return envAIProviderConfig(), nil
}

func envAIProviderConfig() *AIProviderConfig {
	provider := strings.ToLower(strings.TrimSpace(llmProvider))
	cfg := &AIProviderConfig{
		Provider:     provider,
		DefaultModel: strings.TrimSpace(llmModel),
		TaskModels:   map[string]string{},
		Source:       "env",
	}
	switch provider {
	case AIProviderOpenAI:
		cfg.APIKey = openaiAPIKey
		cfg.BaseURL = strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	case AIProviderOllama:
		cfg.BaseURL = defaultBaseURLForProvider(AIProviderOllama)
	case AIProviderGoogleAI:
		cfg.APIKey = os.Getenv("GOOGLEAI_API_KEY")
	case AIProviderOpenRouter:
		cfg.APIKey = os.Getenv("OPENROUTER_API_KEY")
		cfg.BaseURL = defaultBaseURLForProvider(AIProviderOpenRouter)
	default:
		// Preserve env-only support for existing providers outside the new UI scope.
	}
	return cfg
}

func (app *App) currentSuggestionCacheMetadata(ctx context.Context) suggestionCacheMetadata {
	cfg, err := app.ResolveAIProviderConfig(ctx)
	if err != nil || cfg == nil {
		if err != nil {
			log.WithError(err).Warn("Falling back to env AI cache metadata")
		}
		return suggestionCacheMetadata{
			Provider:       strings.ToLower(strings.TrimSpace(llmProvider)),
			Model:          strings.TrimSpace(llmModel),
			PromptVersions: currentPromptVersionsHash(),
		}
	}
	return suggestionCacheMetadata{
		Provider:       cfg.Provider,
		Model:          cfg.DefaultModel,
		TaskModels:     effectiveTaskModelsForCache(cfg),
		PromptVersions: currentPromptVersionsHash(),
	}
}

func effectiveTaskModelsForCache(cfg *AIProviderConfig) map[string]string {
	result := map[string]string{}
	for task := range allowedAITaskModels {
		result[task] = cfg.ModelForTask(task)
	}
	return result
}

func (app *App) getLLMForTask(ctx context.Context, task string) (llms.Model, string, error) {
	cfg, err := app.ResolveAIProviderConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	if cfg == nil || cfg.Provider == "" {
		if app.LLM != nil {
			return app.LLM, "", nil
		}
		return nil, "", errors.New("no AI provider configured")
	}
	model := cfg.ModelForTask(task)
	if model == "" {
		if app.LLM != nil && cfg.Source == "env" {
			return app.LLM, "", nil
		}
		return nil, "", errors.New("AI provider model is not configured")
	}
	if cfg.Source == "env" && app.LLM != nil && cfg.Provider == strings.ToLower(strings.TrimSpace(llmProvider)) && model == strings.TrimSpace(llmModel) {
		return app.LLM, model, nil
	}
	llm, err := app.getOrCreateConfiguredLLM(ctx, cfg, model)
	return llm, model, err
}

func (app *App) getOrCreateConfiguredLLM(ctx context.Context, cfg *AIProviderConfig, model string) (llms.Model, error) {
	cacheKey := llmCacheKey(cfg, model)
	if app.llmCache != nil {
		if cached, ok := app.llmCache.Load(cacheKey); ok {
			return cached.(llmCacheEntry).llm, nil
		}
	}
	llm, err := buildLLMFromConfig(ctx, cfg, model)
	if err != nil {
		return nil, err
	}
	if app.llmCache != nil {
		app.llmCache.Store(cacheKey, llmCacheEntry{llm: llm})
	}
	return llm, nil
}

func llmCacheKey(cfg *AIProviderConfig, model string) string {
	sum := sha256.Sum256([]byte(cfg.APIKey))
	return stableHash(map[string]string{
		"provider": cfg.Provider,
		"base_url": cfg.BaseURL,
		"model":    model,
		"key_hash": hex.EncodeToString(sum[:]),
	})
}

func buildLLMFromConfig(ctx context.Context, cfg *AIProviderConfig, model string) (llms.Model, error) {
	if cfg.Provider == AIProviderOpenAI && strings.ToLower(os.Getenv("OPENAI_API_TYPE")) == "azure" && cfg.Source == "env" {
		return createLLM()
	}
	switch cfg.Provider {
	case AIProviderOpenAI:
		if cfg.APIKey == "" {
			return nil, errors.New("OpenAI API key is not set")
		}
		options := []openai.Option{
			openai.WithModel(model),
			openai.WithToken(cfg.APIKey),
			openai.WithHTTPClient(createCustomHTTPClient()),
		}
		if cfg.BaseURL != "" {
			options = append(options, openai.WithBaseURL(cfg.BaseURL))
		}
		llm, err := openai.New(options...)
		if err != nil {
			return nil, err
		}
		return NewRateLimitedLLM(llm, getRateLimitConfig(false)), nil
	case AIProviderOpenRouter:
		if cfg.APIKey == "" {
			return nil, errors.New("OpenRouter API key is not set")
		}
		baseURL := strings.TrimSpace(cfg.BaseURL)
		if baseURL == "" {
			baseURL = defaultBaseURLForProvider(AIProviderOpenRouter)
		}
		llm, err := openai.New(
			openai.WithModel(model),
			openai.WithToken(cfg.APIKey),
			openai.WithBaseURL(baseURL),
			openai.WithHTTPClient(createOpenRouterHTTPClient()),
		)
		if err != nil {
			return nil, err
		}
		return NewRateLimitedLLM(llm, getRateLimitConfig(false)), nil
	case AIProviderOllama:
		baseURL := strings.TrimSpace(cfg.BaseURL)
		if baseURL == "" {
			baseURL = defaultBaseURLForProvider(AIProviderOllama)
		}
		opts := []ollama.Option{
			ollama.WithModel(model),
			ollama.WithServerURL(baseURL),
		}
		if ctxLenStr := os.Getenv("OLLAMA_CONTEXT_LENGTH"); ctxLenStr != "" {
			if parsed, err := strconv.Atoi(ctxLenStr); err == nil && parsed > 0 {
				opts = append(opts, ollama.WithRunnerNumCtx(parsed))
			} else if err != nil {
				log.Warnf("Invalid OLLAMA_CONTEXT_LENGTH value: %v, ignoring", err)
			}
		}
		llm, err := ollama.New(opts...)
		if err != nil {
			return nil, err
		}
		return NewRateLimitedLLM(llm, getRateLimitConfig(false)), nil
	case AIProviderGoogleAI:
		provider, err := NewGoogleAIProvider(ctx, model, cfg.APIKey, googleThinkingBudget)
		if err != nil {
			return nil, fmt.Errorf("failed to create GoogleAI provider: %w", err)
		}
		return provider, nil
	default:
		if cfg.Source == "env" {
			return createLLM()
		}
		return nil, fmt.Errorf("unsupported AI provider: %s", cfg.Provider)
	}
}

func createOpenRouterHTTPClient() *http.Client {
	referer := appPublicURL
	if referer == "" {
		referer = "https://paperless-gpt.local"
	}
	return createHeaderHTTPClient(map[string]string{
		"HTTP-Referer": referer,
		"X-Title":      "paperless-gpt",
	})
}

func createHeaderHTTPClient(headers map[string]string) *http.Client {
	return &http.Client{Transport: &headerTransport{
		transport: http.DefaultTransport,
		headers:   headers,
	}}
}

func taskModelsJSON(taskModels map[string]string) (string, error) {
	normalized, err := normalizeTaskModels(taskModels)
	if err != nil {
		return "", err
	}
	if len(normalized) == 0 {
		return "", nil
	}
	keys := make([]string, 0, len(normalized))
	for key := range normalized {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := map[string]string{}
	for _, key := range keys {
		ordered[key] = normalized[key]
	}
	payload, err := json.Marshal(ordered)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func (app *App) aiProviderSettingResponse(ctx context.Context, setting AIProviderSetting) (AIProviderSettingsResponse, error) {
	provider, err := normalizeUIProvider(setting.Provider)
	if err != nil {
		return AIProviderSettingsResponse{}, err
	}
	taskModels := map[string]string{}
	if setting.TaskModelsJSON != "" {
		if err := json.Unmarshal([]byte(setting.TaskModelsJSON), &taskModels); err != nil {
			return AIProviderSettingsResponse{}, err
		}
	}
	return AIProviderSettingsResponse{
		Provider:         provider,
		Enabled:          setting.Enabled,
		BaseURL:          setting.BaseURL,
		DefaultModel:     setting.DefaultModel,
		APIKeyConfigured: setting.EncryptedAPIKey != "",
		TaskModels:       taskModels,
		Source:           "ui",
	}, nil
}

func (app *App) getAIProviderSettings(ctx context.Context) (AIProviderSettingsResponse, error) {
	var setting AIProviderSetting
	err := app.Database.WithContext(ctx).Where("enabled = ?", true).Order("updated_at DESC, id DESC").First(&setting).Error
	if err == nil {
		return app.aiProviderSettingResponse(ctx, setting)
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return AIProviderSettingsResponse{}, err
	}
	cfg := envAIProviderConfig()
	provider := cfg.Provider
	if _, ok := allowedUIAIProviders[provider]; !ok {
		provider = AIProviderOpenAI
	}
	return AIProviderSettingsResponse{
		Provider:         provider,
		Enabled:          false,
		BaseURL:          cfg.BaseURL,
		DefaultModel:     cfg.DefaultModel,
		APIKeyConfigured: cfg.APIKey != "",
		TaskModels:       map[string]string{},
		Source:           "env",
	}, nil
}

func (app *App) saveAIProviderSettings(ctx context.Context, req AIProviderSettingsRequest) (AIProviderSettingsResponse, error) {
	provider, err := normalizeUIProvider(req.Provider)
	if err != nil {
		return AIProviderSettingsResponse{}, err
	}
	taskJSON, err := taskModelsJSON(req.TaskModels)
	if err != nil {
		return AIProviderSettingsResponse{}, err
	}
	var setting AIProviderSetting
	db := app.Database.WithContext(ctx)
	err = db.Where("provider = ?", provider).First(&setting).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return AIProviderSettingsResponse{}, err
	}
	if err == gorm.ErrRecordNotFound {
		setting = AIProviderSetting{Provider: provider}
	}
	setting.Enabled = req.Enabled
	setting.BaseURL = strings.TrimSpace(req.BaseURL)
	setting.DefaultModel = strings.TrimSpace(req.DefaultModel)
	setting.TaskModelsJSON = taskJSON
	if strings.TrimSpace(req.APIKey) != "" {
		encrypted, err := EncryptSecret(strings.TrimSpace(req.APIKey))
		if err != nil {
			return AIProviderSettingsResponse{}, err
		}
		setting.EncryptedAPIKey = encrypted
	}
	if setting.Enabled {
		if err := db.Model(&AIProviderSetting{}).Where("provider <> ?", provider).Update("enabled", false).Error; err != nil {
			return AIProviderSettingsResponse{}, err
		}
	}
	if setting.ID == 0 {
		err = db.Create(&setting).Error
	} else {
		err = db.Save(&setting).Error
	}
	if err != nil {
		return AIProviderSettingsResponse{}, err
	}
	if app.llmCache != nil {
		app.llmCache = &sync.Map{}
	}
	return app.aiProviderSettingResponse(ctx, setting)
}

func (app *App) testAIProviderSettings(ctx context.Context, req AIProviderSettingsRequest) error {
	provider, err := normalizeUIProvider(req.Provider)
	if err != nil {
		return err
	}
	taskModels, err := normalizeTaskModels(req.TaskModels)
	if err != nil {
		return err
	}
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" && app != nil && app.Database != nil {
		var existing AIProviderSetting
		if err := app.Database.WithContext(ctx).Where("provider = ?", provider).First(&existing).Error; err == nil && existing.EncryptedAPIKey != "" {
			apiKey, err = DecryptSecret(existing.EncryptedAPIKey)
			if err != nil {
				return fmt.Errorf("failed to decrypt saved API key: %w", err)
			}
		} else if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
	}
	if apiKey == "" {
		switch provider {
		case AIProviderOpenAI:
			apiKey = openaiAPIKey
		case AIProviderOpenRouter:
			apiKey = os.Getenv("OPENROUTER_API_KEY")
		case AIProviderGoogleAI:
			apiKey = os.Getenv("GOOGLEAI_API_KEY")
		}
	}
	cfg := &AIProviderConfig{
		Provider:     provider,
		BaseURL:      strings.TrimSpace(req.BaseURL),
		APIKey:       apiKey,
		DefaultModel: strings.TrimSpace(req.DefaultModel),
		TaskModels:   taskModels,
		Source:       "test",
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURLForProvider(provider)
	}
	model := cfg.ModelForTask(TaskTitle)
	if model == "" {
		return errors.New("default model is required")
	}
	if provider == AIProviderOpenRouter {
		return testOpenRouterSettings(ctx, cfg, model)
	}
	llm, err := buildLLMFromConfig(ctx, cfg, model)
	if err != nil {
		return err
	}
	resp, err := llm.GenerateContent(ctx, []llms.MessageContent{{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Reply with OK."}},
	}})
	if err != nil {
		return err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return errors.New("AI provider returned no response")
	}
	return nil
}

type openRouterAPIError struct {
	Code     int                    `json:"code"`
	Message  string                 `json:"message"`
	Metadata map[string]interface{} `json:"metadata"`
}

type openRouterErrorPayload struct {
	Error openRouterAPIError `json:"error"`
}

type openRouterChatPayload struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *openRouterAPIError `json:"error,omitempty"`
}

func testOpenRouterSettings(ctx context.Context, cfg *AIProviderConfig, model string) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return errors.New("OpenRouter API key is not set")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURLForProvider(AIProviderOpenRouter)
	}
	requestBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Reply with OK."},
		},
		"max_tokens": 8,
	}
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := createOpenRouterHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("OpenRouter connection failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed reading OpenRouter response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return formatOpenRouterError(resp.StatusCode, respBytes)
	}

	var payload openRouterChatPayload
	if err := json.Unmarshal(respBytes, &payload); err != nil {
		return fmt.Errorf("OpenRouter returned an invalid response: %w", err)
	}
	if payload.Error != nil {
		return formatOpenRouterAPIError(payload.Error.Code, *payload.Error)
	}
	if len(payload.Choices) == 0 {
		return errors.New("OpenRouter returned no response choices")
	}
	return nil
}

func formatOpenRouterError(statusCode int, body []byte) error {
	var payload openRouterErrorPayload
	if err := json.Unmarshal(body, &payload); err != nil || strings.TrimSpace(payload.Error.Message) == "" {
		return fmt.Errorf("OpenRouter returned unexpected status code %d: %s", statusCode, strings.TrimSpace(string(body)))
	}

	return formatOpenRouterAPIError(statusCode, payload.Error)
}

func formatOpenRouterAPIError(statusCode int, apiError openRouterAPIError) error {
	message := fmt.Sprintf("OpenRouter error %d: %s", statusCode, strings.TrimSpace(apiError.Message))
	if providerName, ok := apiError.Metadata["provider_name"].(string); ok && strings.TrimSpace(providerName) != "" {
		message += fmt.Sprintf(" from %s", strings.TrimSpace(providerName))
	}
	if raw, ok := apiError.Metadata["raw"]; ok {
		if rawMessage := stringifyOpenRouterMetadata(raw); rawMessage != "" {
			message += fmt.Sprintf(" - %s", rawMessage)
		}
	}

	switch statusCode {
	case http.StatusTooManyRequests:
		message += ". You are being rate limited by OpenRouter or the upstream model provider. Try again later, choose a less rate-limited model, or configure provider keys in OpenRouter."
	case http.StatusPaymentRequired:
		message += ". Your OpenRouter account or API key has insufficient credits."
	case http.StatusUnauthorized:
		message += ". Check that your OpenRouter API key is valid and enabled."
	case http.StatusServiceUnavailable, http.StatusBadGateway:
		message += ". The selected model provider is temporarily unavailable; try another model or retry later."
	}
	return errors.New(message)
}

func stringifyOpenRouterMetadata(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}
