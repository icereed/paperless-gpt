package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"paperless-gpt/ocr"
)

const (
	defaultSimilarLimit = 5
	maxSimilarLimit     = 20
	openAIEmbeddingsAPI = "https://api.openai.com/v1"
	azureEmbeddingsAPI  = "2024-10-21"
)

// Embedder turns text into vectors. A nil embedder means similar-document
// context is off.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
}

type embeddingSettings struct {
	Provider     string
	Model        string
	SimilarLimit int
}

// loadEmbeddingConfig reads the embedding environment. An empty provider
// disables the feature. A provider without a model, or an unknown provider,
// is a configuration error.
func loadEmbeddingConfig() (embeddingSettings, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("EMBEDDING_PROVIDER")))
	model := strings.TrimSpace(os.Getenv("EMBEDDING_MODEL"))
	limit := defaultSimilarLimit
	if raw := os.Getenv("EMBEDDING_SIMILAR_LIMIT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			log.Warnf("Invalid EMBEDDING_SIMILAR_LIMIT %q, using %d", raw, defaultSimilarLimit)
		} else {
			if n > maxSimilarLimit {
				log.Warnf("EMBEDDING_SIMILAR_LIMIT %d is above %d; capping", n, maxSimilarLimit)
				n = maxSimilarLimit
			}
			limit = n
		}
	}
	switch provider {
	case "", "openai", "ollama":
	default:
		return embeddingSettings{}, fmt.Errorf("EMBEDDING_PROVIDER must be openai or ollama, got %q", provider)
	}
	if provider != "" && model == "" {
		return embeddingSettings{}, fmt.Errorf("EMBEDDING_MODEL is required when EMBEDDING_PROVIDER is set")
	}
	return embeddingSettings{Provider: provider, Model: model, SimilarLimit: limit}, nil
}

func newEmbedder(cfg embeddingSettings) (Embedder, error) {
	switch cfg.Provider {
	case "openai":
		return newOpenAIEmbedder(cfg.Model)
	case "ollama":
		return newOllamaEmbedder(cfg.Model)
	default:
		return nil, nil
	}
}

type openAIEmbedder struct {
	client   *http.Client
	endpoint string
	apiKey   string
	model    string
	azure    bool
}

func newOpenAIEmbedder(model string) (*openAIEmbedder, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required when EMBEDDING_PROVIDER=openai")
	}
	azure := strings.ToLower(os.Getenv("OPENAI_API_TYPE")) == "azure"
	endpoint, err := openAIEmbeddingsURL(os.Getenv("OPENAI_BASE_URL"), model, azure)
	if err != nil {
		return nil, err
	}
	client := ocr.OpenAIHTTPClient()
	if client.Timeout == 0 {
		client.Timeout = 60 * time.Second
	}
	return &openAIEmbedder{
		client:   client,
		endpoint: endpoint,
		apiKey:   apiKey,
		model:    model,
		azure:    azure,
	}, nil
}

func openAIEmbeddingsURL(base, model string, azure bool) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if azure {
		if base == "" {
			return "", fmt.Errorf("OPENAI_BASE_URL is required for Azure OpenAI embeddings")
		}
		return fmt.Sprintf("%s/openai/deployments/%s/embeddings?api-version=%s", base, url.PathEscape(model), azureEmbeddingsAPI), nil
	}
	if base == "" {
		base = openAIEmbeddingsAPI
	}
	return base + "/embeddings", nil
}

func (e *openAIEmbedder) Model() string { return e.model }

func (e *openAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body := map[string]any{"input": texts}
	if !e.azure {
		body["model"] = e.model
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.azure {
		req.Header.Set("api-key", e.apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embeddings: status %d: %s", resp.StatusCode, truncateForLog(string(data), 200))
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("openai embeddings: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("openai embeddings: got %d vectors for %d texts", len(parsed.Data), len(texts))
	}
	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].Index < parsed.Data[j].Index })
	out := make([][]float32, len(parsed.Data))
	for i, item := range parsed.Data {
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("openai embeddings: empty vector at index %d", item.Index)
		}
		out[i] = item.Embedding
	}
	return out, nil
}

type ollamaEmbedder struct {
	client   *http.Client
	endpoint string
	model    string
}

func newOllamaEmbedder(model string) (*ollamaEmbedder, error) {
	host := strings.TrimRight(strings.TrimSpace(os.Getenv("OLLAMA_HOST")), "/")
	if host == "" {
		host = "http://127.0.0.1:11434"
	}
	client := ollamaHTTPClientWithTimeout()
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &ollamaEmbedder{
		client:   client,
		endpoint: host + "/api/embed",
		model:    model,
	}, nil
}

func (e *ollamaEmbedder) Model() string { return e.model }

func (e *ollamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(map[string]any{
		"model": e.model,
		"input": texts,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embeddings: status %d: %s", resp.StatusCode, truncateForLog(string(data), 200))
	}
	var parsed struct {
		Embeddings [][]float32 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("ollama embeddings: %w", err)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("ollama embeddings: %s", parsed.Error)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embeddings: got %d vectors for %d texts", len(parsed.Embeddings), len(texts))
	}
	for i, vector := range parsed.Embeddings {
		if len(vector) == 0 {
			return nil, fmt.Errorf("ollama embeddings: empty vector at index %d", i)
		}
	}
	return parsed.Embeddings, nil
}
