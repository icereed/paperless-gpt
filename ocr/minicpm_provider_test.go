package ocr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// miniCPMTestServer spins up a fake OpenAI-compatible chat endpoint and
// returns it together with the last decoded request body.
func miniCPMTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *atomic.Value) {
	t.Helper()
	var lastBody atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := json.NewDecoder(r.Body)
		var raw map[string]interface{}
		if err := body.Decode(&raw); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
			return
		}
		lastBody.Store(raw)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server, &lastBody
}

func miniCPMResponse(w http.ResponseWriter, finishReason, content string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"choices": []map[string]interface{}{{
			"finish_reason": finishReason,
			"message":       map[string]interface{}{"content": content},
		}},
	})
}

func TestMiniCPMProviderSendsOfficialRequestShape(t *testing.T) {
	server, lastBody := miniCPMTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		miniCPMResponse(w, "stop", "recognized text")
	})
	provider, err := newMiniCPMProvider(Config{
		Provider:         "minicpm",
		MiniCPMEndpoint:  server.URL + "/v1/chat/completions",
		MiniCPMAPIKey:    "test-key",
		MiniCPMModel:     "minicpm-ocr",
		MiniCPMPrompt:    "Text Recognition:",
		MiniCPMMaxTokens: 4096,
	})
	require.NoError(t, err)

	// 1x1 transparent PNG
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x62, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}

	result, err := provider.ProcessImage(context.Background(), png, 1)
	require.NoError(t, err)
	assert.Equal(t, "recognized text", result.Text)
	assert.False(t, result.OcrLimitHit)
	assert.Equal(t, "minicpm", result.Metadata["provider"])

	reqBody := lastBody.Load().(map[string]interface{})
	assert.Equal(t, "minicpm-ocr", reqBody["model"])
	assert.Equal(t, float64(4096), reqBody["max_tokens"])
	// Official validated decoding parameters (vLLM deployment guide).
	assert.Equal(t, float64(0.01), reqBody["temperature"])
	assert.Equal(t, float64(0.001), reqBody["top_p"])
	assert.Equal(t, float64(1), reqBody["top_k"])
	assert.Equal(t, float64(1.0), reqBody["repetition_penalty"])
	assert.Equal(t, float64(100), reqBody["no_repeat_ngram_size"])
	assert.Equal(t, map[string]interface{}{"enable_thinking": false}, reqBody["chat_template_kwargs"])
	assert.Equal(t, map[string]interface{}{"downsample_mode": "4x", "max_slice_nums": float64(9)}, reqBody["mm_processor_kwargs"])

	messages := reqBody["messages"].([]interface{})
	require.Len(t, messages, 1)
	content := messages[0].(map[string]interface{})["content"].([]interface{})
	require.Len(t, content, 2)
	// Image must precede the text part.
	imagePart := content[0].(map[string]interface{})
	textPart := content[1].(map[string]interface{})
	assert.Equal(t, "image_url", imagePart["type"])
	imageURL := imagePart["image_url"].(map[string]interface{})["url"].(string)
	assert.Contains(t, imageURL, "data:image/png;base64,")
	assert.Equal(t, "Text Recognition:", textPart["text"])
}

func TestMiniCPMProviderRequiresEndpoint(t *testing.T) {
	_, err := newMiniCPMProvider(Config{Provider: "minicpm"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint")
}

func TestMiniCPMProviderMarksLengthAsTruncated(t *testing.T) {
	server, _ := miniCPMTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		miniCPMResponse(w, "length", "partial text")
	})
	provider, err := newMiniCPMProvider(Config{MiniCPMEndpoint: server.URL})
	require.NoError(t, err)

	result, err := provider.ProcessImage(context.Background(), tinyPNG(t), 1)
	require.NoError(t, err)
	assert.Equal(t, "partial text", result.Text)
	assert.True(t, result.OcrLimitHit)
}

func TestMiniCPMProviderAllowsBlankStopPage(t *testing.T) {
	server, _ := miniCPMTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		miniCPMResponse(w, "stop", "")
	})
	provider, err := newMiniCPMProvider(Config{MiniCPMEndpoint: server.URL})
	require.NoError(t, err)

	result, err := provider.ProcessImage(context.Background(), tinyPNG(t), 1)
	require.NoError(t, err)
	assert.Empty(t, result.Text)
	assert.False(t, result.OcrLimitHit)
}

func TestMiniCPMProviderRejectsOtherFinishReasons(t *testing.T) {
	server, _ := miniCPMTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		miniCPMResponse(w, "content_filter", "partial")
	})
	provider, err := newMiniCPMProvider(Config{MiniCPMEndpoint: server.URL})
	require.NoError(t, err)

	_, err = provider.ProcessImage(context.Background(), tinyPNG(t), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content_filter")
}

func TestMiniCPMProviderRejectsNonImageInput(t *testing.T) {
	server, _ := miniCPMTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		miniCPMResponse(w, "stop", "text")
	})
	provider, err := newMiniCPMProvider(Config{MiniCPMEndpoint: server.URL})
	require.NoError(t, err)

	_, err = provider.ProcessImage(context.Background(), []byte("%PDF-1.7 fake"), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected an image")
}

func TestMiniCPMProviderRetriesTransientErrors(t *testing.T) {
	var calls int32
	server, _ := miniCPMTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, `{"error": "overloaded"}`, http.StatusBadGateway)
			return
		}
		miniCPMResponse(w, "stop", "second try")
	})
	provider, err := newMiniCPMProvider(Config{MiniCPMEndpoint: server.URL})
	require.NoError(t, err)

	result, err := provider.ProcessImage(context.Background(), tinyPNG(t), 1)
	require.NoError(t, err)
	assert.Equal(t, "second try", result.Text)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestMiniCPMProviderFailsHardOnPermanentError(t *testing.T) {
	server, _ := miniCPMTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error": "bad request"}`, http.StatusBadRequest)
	})
	provider, err := newMiniCPMProvider(Config{MiniCPMEndpoint: server.URL})
	require.NoError(t, err)

	_, err = provider.ProcessImage(context.Background(), tinyPNG(t), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 400")
}

func TestMiniCPMProviderSendsAuthorizationOnlyWhenConfigured(t *testing.T) {
	var authHeader atomic.Value
	server, _ := miniCPMTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		authHeader.Store(r.Header.Get("Authorization"))
		miniCPMResponse(w, "stop", "text")
	})

	noAuth, err := newMiniCPMProvider(Config{MiniCPMEndpoint: server.URL})
	require.NoError(t, err)
	_, err = noAuth.ProcessImage(context.Background(), tinyPNG(t), 1)
	require.NoError(t, err)
	assert.Empty(t, authHeader.Load().(string), "vLLM without --api-key must not receive a bearer token")

	withAuth, err := newMiniCPMProvider(Config{MiniCPMEndpoint: server.URL, MiniCPMAPIKey: "secret"})
	require.NoError(t, err)
	_, err = withAuth.ProcessImage(context.Background(), tinyPNG(t), 1)
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret", authHeader.Load().(string))
}

func TestDecodeMiniCPMContentAcceptsPartsArray(t *testing.T) {
	assert.Equal(t, "plain", decodeMiniCPMContent(json.RawMessage(`"plain"`)))
	assert.Equal(t, "a b", decodeMiniCPMContent(json.RawMessage(`[{"type":"text","text":"a"},{"type":"text","text":" b"}]`)))
	assert.Equal(t, "", decodeMiniCPMContent(json.RawMessage(`[]`)))
	assert.Equal(t, "", decodeMiniCPMContent(nil))
}

// tinyPNG returns a minimal valid 1x1 PNG.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	return []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x62, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
}
