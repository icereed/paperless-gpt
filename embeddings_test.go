package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIEmbeddingsURL(t *testing.T) {
	got, err := openAIEmbeddingsURL("", "text-embedding-3-small", false)
	require.NoError(t, err)
	assert.Equal(t, "https://api.openai.com/v1/embeddings", got)

	got, err = openAIEmbeddingsURL("https://example.test/v1/", "text-embedding-3-small", false)
	require.NoError(t, err)
	assert.Equal(t, "https://example.test/v1/embeddings", got)

	_, err = openAIEmbeddingsURL("", "deploy", true)
	assert.Error(t, err)

	got, err = openAIEmbeddingsURL("https://example.openai.azure.com", "my deploy", true)
	require.NoError(t, err)
	assert.Equal(t, "https://example.openai.azure.com/openai/deployments/my%20deploy/embeddings?api-version=2024-10-21", got)
}

func TestOpenAIEmbedder(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/embeddings", r.URL.Path)
		gotAuth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		// Return out of order to confirm index sorting.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 1, "embedding": []float32{0, 1}},
				{"index": 0, "embedding": []float32{1, 0}},
			},
		})
	}))
	defer server.Close()

	embedder := &openAIEmbedder{
		client:   server.Client(),
		endpoint: server.URL + "/embeddings",
		apiKey:   "secret",
		model:    "text-embedding-3-small",
	}
	vectors, err := embedder.Embed(context.Background(), []string{"one", "two"})
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret", gotAuth)
	assert.Equal(t, "text-embedding-3-small", gotBody["model"])
	assert.Equal(t, []any{"one", "two"}, gotBody["input"])
	require.Len(t, vectors, 2)
	assert.Equal(t, []float32{1, 0}, vectors[0])
	assert.Equal(t, []float32{0, 1}, vectors[1])
}

func TestOllamaEmbedder(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/embed", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotBody))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.5, 0.5}},
		})
	}))
	defer server.Close()

	embedder := &ollamaEmbedder{
		client:   server.Client(),
		endpoint: server.URL + "/api/embed",
		model:    "nomic-embed-text",
	}
	vectors, err := embedder.Embed(context.Background(), []string{"invoice"})
	require.NoError(t, err)
	assert.Equal(t, "nomic-embed-text", gotBody["model"])
	require.Len(t, vectors, 1)
	assert.Equal(t, []float32{0.5, 0.5}, vectors[0])
}

func TestOpenAIEmbedderRejectsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()
	embedder := &openAIEmbedder{client: server.Client(), endpoint: server.URL, apiKey: "k", model: "m"}
	_, err := embedder.Embed(context.Background(), []string{"x"})
	assert.Error(t, err)
}
