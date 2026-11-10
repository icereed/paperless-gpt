package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiniCPMChatCompletionsURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "server root", in: "http://localhost:8000", want: "http://localhost:8000/v1/chat/completions"},
		{name: "trailing slash", in: "http://localhost:8000/", want: "http://localhost:8000/v1/chat/completions"},
		{name: "openai base", in: "http://localhost:8000/v1", want: "http://localhost:8000/v1/chat/completions"},
		{name: "openai base trailing slash", in: "https://ocr.example/v1/", want: "https://ocr.example/v1/chat/completions"},
		{name: "full endpoint", in: "http://localhost:8000/v1/chat/completions", want: "http://localhost:8000/v1/chat/completions"},
		{name: "full endpoint trailing slash", in: "http://localhost:8000/v1/chat/completions/", want: "http://localhost:8000/v1/chat/completions"},
		{name: "whitespace", in: "  http://localhost:8000/v1  ", want: "http://localhost:8000/v1/chat/completions"},
		{name: "query on v1 base", in: "https://ocr.example/v1?token=secret", want: "https://ocr.example/v1/chat/completions?token=secret"},
		{name: "query on server root", in: "https://ocr.example?token=secret", want: "https://ocr.example/v1/chat/completions?token=secret"},
		{name: "userinfo preserved", in: "https://user:secret@ocr.example/v1", want: "https://user:secret@ocr.example/v1/chat/completions"},
		{name: "missing", in: "  ", wantErr: "MINICPM_OCR_BASE_URL"},
		{name: "not http", in: "localhost:8000", wantErr: "http(s)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := miniCPMChatCompletionsURL(tt.in)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewMiniCPMOCRProvider(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		provider, err := newMiniCPMOCRProvider(Config{MiniCPMBaseURL: "http://127.0.0.1:9"})
		require.NoError(t, err)
		assert.Equal(t, "http://127.0.0.1:9/v1/chat/completions", provider.chatURL)
		assert.Equal(t, miniCPMDefaultModel, provider.model)
		assert.Equal(t, miniCPMDefaultMaxTokens, provider.maxTokens)
		assert.Equal(t, time.Duration(miniCPMDefaultTimeoutSeconds)*time.Second, provider.httpClient.Timeout)
		assert.Empty(t, provider.apiKey)
	})

	t.Run("custom config", func(t *testing.T) {
		provider, err := newMiniCPMOCRProvider(Config{
			MiniCPMBaseURL:   "http://ocr.internal/v1/",
			MiniCPMAPIKey:    "  secret  ",
			MiniCPMModel:     " custom-ocr ",
			MiniCPMTimeout:   15,
			MiniCPMMaxTokens: 1024,
		})
		require.NoError(t, err)
		assert.Equal(t, "http://ocr.internal/v1/chat/completions", provider.chatURL)
		assert.Equal(t, "secret", provider.apiKey)
		assert.Equal(t, "custom-ocr", provider.model)
		assert.Equal(t, 1024, provider.maxTokens)
		assert.Equal(t, 15*time.Second, provider.httpClient.Timeout)
	})

	t.Run("missing base url", func(t *testing.T) {
		provider, err := newMiniCPMOCRProvider(Config{})
		require.Error(t, err)
		assert.Nil(t, provider)
		assert.Contains(t, err.Error(), "MINICPM_OCR_BASE_URL")
	})
}

func TestNewProvider_MiniCPM(t *testing.T) {
	provider, err := NewProvider(Config{
		Provider:       "minicpm_ocr",
		MiniCPMBaseURL: "http://127.0.0.1:9",
		MiniCPMModel:   "minicpm-ocr",
	})
	require.NoError(t, err)
	require.IsType(t, &MiniCPMOCRProvider{}, provider)

	_, err = NewProvider(Config{Provider: "minicpm_ocr"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MINICPM_OCR_BASE_URL")
}

func TestMiniCPMOCRProvider_ProcessImage(t *testing.T) {
	image := []byte("page-image-bytes")

	t.Run("request shape and auth", func(t *testing.T) {
		var got miniCPMChatRequest
		var rawBody []byte
		var auth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/v1/chat/completions", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "application/json", r.Header.Get("Accept"))
			auth = r.Header.Get("Authorization")

			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			rawBody = body
			assert.NoError(t, json.Unmarshal(body, &got))

			writeMiniCPMChatResponse(t, w, "Invoice total 42", "stop", 11, 4, 15)
		}))
		defer server.Close()

		provider := testMiniCPMProvider(t, server.URL, "test-key")
		result, err := provider.ProcessImage(context.Background(), image, 3)
		require.NoError(t, err)

		assert.Equal(t, "Bearer test-key", auth)
		assert.Equal(t, "minicpm-ocr", got.Model)
		require.Len(t, got.Messages, 1)
		assert.Equal(t, "user", got.Messages[0].Role)
		require.Len(t, got.Messages[0].Content, 2)
		assert.Equal(t, "image_url", got.Messages[0].Content[0].Type)
		require.NotNil(t, got.Messages[0].Content[0].ImageURL)
		assert.Equal(t, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(image), got.Messages[0].Content[0].ImageURL.URL)
		assert.Equal(t, "text", got.Messages[0].Content[1].Type)
		assert.Equal(t, miniCPMPagePrompt, got.Messages[0].Content[1].Text)
		require.NotNil(t, got.MMProcessorKwargs)
		assert.Equal(t, miniCPMDefaultDownsample, got.MMProcessorKwargs.DownsampleMode)
		assert.Nil(t, got.MMProcessorKwargs.MaxSliceNums)
		assert.Contains(t, string(rawBody), `"mm_processor_kwargs":{"downsample_mode":"4x"}`)
		assert.NotContains(t, string(rawBody), "max_slice_nums")
		assert.Equal(t, miniCPMDefaultMaxTokens, got.MaxTokens)
		assert.InDelta(t, miniCPMTemperature, got.Temperature, 1e-9)
		assert.InDelta(t, miniCPMTopP, got.TopP, 1e-9)
		assert.Equal(t, miniCPMTopK, got.TopK)
		assert.InDelta(t, miniCPMRepetitionPenalty, got.RepetitionPenalty, 1e-9)
		assert.Equal(t, miniCPMNoRepeatNgramSize, got.NoRepeatNgramSize)
		assert.False(t, got.Stream)
		assert.False(t, got.ChatTemplateKwargs.EnableThinking)
		assert.Contains(t, string(rawBody), `"stream":false`)
		assert.Contains(t, string(rawBody), `"enable_thinking":false`)
		assert.Contains(t, string(rawBody), `"temperature":0.01`)
		assert.Contains(t, string(rawBody), `"top_p":0.001`)
		assert.Contains(t, string(rawBody), `"top_k":1`)

		assert.Equal(t, "Invoice total 42", result.Text)
		assert.Equal(t, miniCPMProviderName, result.Metadata["provider"])
		assert.Equal(t, "minicpm-ocr", result.Metadata["model"])
		assert.Equal(t, "image/jpeg", result.Metadata["mime_type"])
		assert.Equal(t, "3", result.Metadata["page"])
		assert.Equal(t, "stop", result.Metadata["finish_reason"])
		assert.False(t, result.OcrLimitHit)
		assert.Equal(t, "stop", result.GenerationInfo["finish_reason"])
		assert.Equal(t, 11, result.GenerationInfo["prompt_tokens"])
		assert.Equal(t, 4, result.GenerationInfo["completion_tokens"])
		assert.Equal(t, 15, result.GenerationInfo["total_tokens"])
	})

	t.Run("omits auth header when key is empty", func(t *testing.T) {
		var auth string
		var hasAuth bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("Authorization")
			_, hasAuth = r.Header["Authorization"]
			writeMiniCPMChatResponse(t, w, "plain text", "stop", 0, 0, 0)
		}))
		defer server.Close()

		provider := testMiniCPMProvider(t, server.URL, "")
		result, err := provider.ProcessImage(context.Background(), image, 1)
		require.NoError(t, err)
		assert.Empty(t, auth)
		assert.False(t, hasAuth)
		assert.Equal(t, "plain text", result.Text)
		_, hasUsage := result.GenerationInfo["total_tokens"]
		assert.False(t, hasUsage)
	})

	t.Run("length finish reason marks the limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeMiniCPMChatResponse(t, w, "partial page", "length", 0, 0, 0)
		}))
		defer server.Close()

		provider := testMiniCPMProvider(t, server.URL, "")
		result, err := provider.ProcessImage(context.Background(), image, 1)
		require.NoError(t, err)
		assert.Equal(t, "partial page", result.Text)
		assert.True(t, result.OcrLimitHit)
		assert.Equal(t, "length", result.Metadata["finish_reason"])
	})

	t.Run("content parts", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := w.Write([]byte(`{"choices":[{"message":{"content":[{"type":"text","text":"line one"},{"type":"text","text":"line two"}]},"finish_reason":"stop"}]}`))
			assert.NoError(t, err)
		}))
		defer server.Close()

		provider := testMiniCPMProvider(t, server.URL, "")
		result, err := provider.ProcessImage(context.Background(), image, 1)
		require.NoError(t, err)
		assert.Equal(t, "line one\nline two", result.Text)
	})

	t.Run("png data url", func(t *testing.T) {
		png := []byte("\x89PNG\r\n\x1a\n")
		var got miniCPMChatRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.NoError(t, json.Unmarshal(body, &got))
			writeMiniCPMChatResponse(t, w, "png", "stop", 0, 0, 0)
		}))
		defer server.Close()

		provider := testMiniCPMProvider(t, server.URL, "")
		result, err := provider.ProcessImage(context.Background(), png, 1)
		require.NoError(t, err)
		assert.Equal(t, "image/png", result.Metadata["mime_type"])
		assert.True(t, strings.HasPrefix(got.Messages[0].Content[0].ImageURL.URL, "data:image/png;base64,"))
	})

	t.Run("http error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
		}))
		defer server.Close()

		provider := testMiniCPMProvider(t, server.URL, "bad")
		result, err := provider.ProcessImage(context.Background(), image, 1)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "401")
		assert.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("error field", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"error":{"message":"model not found"}}`))
		}))
		defer server.Close()

		provider := testMiniCPMProvider(t, server.URL, "")
		_, err := provider.ProcessImage(context.Background(), image, 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model not found")
	})

	t.Run("no choices", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[]}`))
		}))
		defer server.Close()

		provider := testMiniCPMProvider(t, server.URL, "")
		_, err := provider.ProcessImage(context.Background(), image, 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no choices")
	})

	t.Run("invalid json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not-json`))
		}))
		defer server.Close()

		provider := testMiniCPMProvider(t, server.URL, "")
		_, err := provider.ProcessImage(context.Background(), image, 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse")
	})

	t.Run("empty content is a blank page", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeMiniCPMChatResponse(t, w, "   ", "stop", 0, 0, 0)
		}))
		defer server.Close()

		provider := testMiniCPMProvider(t, server.URL, "")
		result, err := provider.ProcessImage(context.Background(), image, 2)
		require.NoError(t, err)
		assert.Empty(t, result.Text)
		assert.False(t, result.OcrLimitHit)
	})

	t.Run("pdf is rejected", func(t *testing.T) {
		provider := testMiniCPMProvider(t, "http://127.0.0.1:9", "")
		result, err := provider.ProcessImage(context.Background(), []byte("%PDF-1.4\n"), 1)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "OCR_PROCESS_MODE=image")
	})

	t.Run("empty image", func(t *testing.T) {
		provider := testMiniCPMProvider(t, "http://127.0.0.1:9", "")
		_, err := provider.ProcessImage(context.Background(), nil, 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty image")
	})

	t.Run("configured max tokens", func(t *testing.T) {
		var got miniCPMChatRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.NoError(t, json.Unmarshal(body, &got))
			writeMiniCPMChatResponse(t, w, "short", "stop", 0, 0, 0)
		}))
		defer server.Close()

		provider, err := newMiniCPMOCRProvider(Config{
			MiniCPMBaseURL:   server.URL,
			MiniCPMTimeout:   5,
			MiniCPMMaxTokens: 1024,
		})
		require.NoError(t, err)
		_, err = provider.ProcessImage(context.Background(), image, 1)
		require.NoError(t, err)
		assert.Equal(t, 1024, got.MaxTokens)
	})

	t.Run("unsupported content parts", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := w.Write([]byte(`{"choices":[{"message":{"content":[{"type":"refusal","refusal":"Cannot process this image"}]},"finish_reason":"stop"}]}`))
			assert.NoError(t, err)
		}))
		defer server.Close()

		provider := testMiniCPMProvider(t, server.URL, "")
		result, err := provider.ProcessImage(context.Background(), image, 1)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "unsupported content parts")
	})

	t.Run("canceled context", func(t *testing.T) {
		provider := testMiniCPMProvider(t, "http://127.0.0.1:9", "")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := provider.ProcessImage(ctx, image, 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func testMiniCPMProvider(t *testing.T, serverURL, apiKey string) *MiniCPMOCRProvider {
	t.Helper()
	provider, err := newMiniCPMOCRProvider(Config{
		MiniCPMBaseURL: serverURL,
		MiniCPMAPIKey:  apiKey,
		MiniCPMTimeout: 5,
	})
	require.NoError(t, err)
	return provider
}

func writeMiniCPMChatResponse(t *testing.T, w http.ResponseWriter, content, finish string, promptTokens, completionTokens, totalTokens int) {
	t.Helper()
	payload := map[string]interface{}{
		"model": "minicpm-ocr",
		"choices": []map[string]interface{}{
			{
				"message":       map[string]interface{}{"role": "assistant", "content": content},
				"finish_reason": finish,
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      totalTokens,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(payload))
}

func TestParseMiniCPMChatResponse_UnsupportedContent(t *testing.T) {
	_, _, _, err := parseMiniCPMChatResponse([]byte(`{"choices":[{"message":{"content":{"unexpected":true}},"finish_reason":"stop"}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported content")
}

func TestMiniCPMRefusal(t *testing.T) {
	for _, body := range []string{`{"choices":[{"message":{"refusal":"no","content":null}}]}`, `{"choices":[{"message":{"content":[{"type":"refusal","refusal":"no"}]}}]}`} {
		_, _, _, err := parseMiniCPMChatResponse([]byte(body))
		require.Error(t, err)
	}
}

func TestMiniCPMOCRProvider_PromptAndMMKwargs(t *testing.T) {
	image := []byte("page-image-bytes")

	t.Run("custom prompt and slice count", func(t *testing.T) {
		var got miniCPMChatRequest
		var raw string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			raw = string(body)
			assert.NoError(t, json.Unmarshal(body, &got))
			writeMiniCPMChatResponse(t, w, "table", "stop", 0, 0, 0)
		}))
		defer server.Close()

		slices := 9
		provider, err := newMiniCPMOCRProvider(Config{
			MiniCPMBaseURL:        server.URL,
			MiniCPMTimeout:        5,
			MiniCPMPrompt:         "  Table Recognition:  ",
			MiniCPMDownsampleMode: "2x",
			MiniCPMMaxSliceNums:   &slices,
		})
		require.NoError(t, err)
		assert.Equal(t, "Table Recognition:", provider.prompt)
		_, err = provider.ProcessImage(context.Background(), image, 1)
		require.NoError(t, err)
		assert.Equal(t, "Table Recognition:", got.Messages[0].Content[1].Text)
		require.NotNil(t, got.MMProcessorKwargs)
		assert.Equal(t, "2x", got.MMProcessorKwargs.DownsampleMode)
		require.NotNil(t, got.MMProcessorKwargs.MaxSliceNums)
		assert.Equal(t, 9, *got.MMProcessorKwargs.MaxSliceNums)
		assert.Contains(t, raw, `"downsample_mode":"2x"`)
		assert.Contains(t, raw, `"max_slice_nums":9`)
	})

	t.Run("off omits mm processor kwargs", func(t *testing.T) {
		var raw string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			raw = string(body)
			writeMiniCPMChatResponse(t, w, "text", "stop", 0, 0, 0)
		}))
		defer server.Close()

		slices := 3
		provider, err := newMiniCPMOCRProvider(Config{
			MiniCPMBaseURL:        server.URL,
			MiniCPMTimeout:        5,
			MiniCPMDownsampleMode: "off",
			MiniCPMMaxSliceNums:   &slices,
			MiniCPMPrompt:         "   ",
		})
		require.NoError(t, err)
		assert.Equal(t, miniCPMPagePrompt, provider.prompt)
		assert.False(t, provider.sendMMKwargs)
		_, err = provider.ProcessImage(context.Background(), image, 1)
		require.NoError(t, err)
		assert.NotContains(t, raw, "mm_processor_kwargs")
		assert.NotContains(t, raw, "max_slice_nums")
	})
}

func TestMiniCPMOCRProvider_Retries(t *testing.T) {
	image := []byte("page-image-bytes")
	t.Setenv("VISION_LLM_BACKOFF_MAX_WAIT", "1ms")

	t.Run("503 then 200", func(t *testing.T) {
		t.Setenv("VISION_LLM_MAX_RETRIES", "8")
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			writeMiniCPMChatResponse(t, w, "recovered", "stop", 0, 0, 0)
		}))
		defer server.Close()

		result, err := testMiniCPMProvider(t, server.URL, "").ProcessImage(context.Background(), image, 1)
		require.NoError(t, err)
		assert.Equal(t, "recovered", result.Text)
		assert.Equal(t, 2, calls)
	})

	t.Run("429 honors retry-after", func(t *testing.T) {
		t.Setenv("VISION_LLM_MAX_RETRIES", "8")
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "slow down", http.StatusTooManyRequests)
				return
			}
			writeMiniCPMChatResponse(t, w, "after wait", "stop", 0, 0, 0)
		}))
		defer server.Close()

		start := time.Now()
		result, err := testMiniCPMProvider(t, server.URL, "").ProcessImage(context.Background(), image, 1)
		elapsed := time.Since(start)
		require.NoError(t, err)
		assert.Equal(t, "after wait", result.Text)
		assert.Equal(t, 2, calls)
		assert.GreaterOrEqual(t, elapsed, 900*time.Millisecond)
	})

	t.Run("400 is not retried", func(t *testing.T) {
		t.Setenv("VISION_LLM_MAX_RETRIES", "8")
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			http.Error(w, "bad request", http.StatusBadRequest)
		}))
		defer server.Close()

		_, err := testMiniCPMProvider(t, server.URL, "").ProcessImage(context.Background(), image, 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "400")
		assert.Equal(t, 1, calls)
	})

	t.Run("retries exhausted", func(t *testing.T) {
		t.Setenv("VISION_LLM_MAX_RETRIES", "2")
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		defer server.Close()

		_, err := testMiniCPMProvider(t, server.URL, "").ProcessImage(context.Background(), image, 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "503")
		assert.Equal(t, 3, calls)
	})

	t.Run("canceled during retry-after", func(t *testing.T) {
		t.Setenv("VISION_LLM_MAX_RETRIES", "8")
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Retry-After", "30")
			http.Error(w, "slow down", http.StatusTooManyRequests)
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			time.Sleep(150 * time.Millisecond)
			cancel()
		}()
		_, err := testMiniCPMProvider(t, server.URL, "").ProcessImage(ctx, image, 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 1, calls)
	})

	t.Run("network error is retried", func(t *testing.T) {
		t.Setenv("VISION_LLM_MAX_RETRIES", "1")
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				hj, ok := w.(http.Hijacker)
				if !assert.True(t, ok) {
					return
				}
				conn, _, err := hj.Hijack()
				assert.NoError(t, err)
				_ = conn.Close()
				return
			}
			writeMiniCPMChatResponse(t, w, "after reset", "stop", 0, 0, 0)
		}))
		defer server.Close()

		result, err := testMiniCPMProvider(t, server.URL, "").ProcessImage(context.Background(), image, 1)
		require.NoError(t, err)
		assert.Equal(t, "after reset", result.Text)
		assert.Equal(t, 2, calls)
	})
}

func TestMiniCPMDoesNotExposeURLSecrets(t *testing.T) {
	t.Setenv("VISION_LLM_MAX_RETRIES", "0")
	var logs bytes.Buffer
	previous := log.Out
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	provider, err := NewProvider(Config{Provider: "minicpm_ocr", MiniCPMBaseURL: "http://user:password@127.0.0.1:1/v1?token=secret"})
	require.NoError(t, err)
	_, err = provider.ProcessImage(context.Background(), []byte("page"), 1)
	require.Error(t, err)
	for _, secret := range []string{"password", "secret"} {
		assert.NotContains(t, logs.String(), secret)
		assert.NotContains(t, err.Error(), secret)
	}
}
