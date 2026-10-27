package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

// Use the real pinned SDK: this reproduces its default temperature=0 for
// both metadata requests and the identical client used by vision OCR.
func TestGPT6OpenAIClientTemperature(t *testing.T) {
	for _, tc := range []struct {
		model           string
		wantTemperature bool
	}{
		{"gpt-6.1-terra", false},
		{"gpt-6.1-terra-2026-10-01", false},
		{"gpt-6-luna", false},
		{"openai/gpt-6.1-terra", false},
		{"gpt-5.6-luna", false},
		{"gpt-4.1-mini", true},
	} {
		t.Run(tc.model, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				_, hasTemperature := body["temperature"]
				assert.Equal(t, tc.wantTemperature, hasTemperature)
				assert.Equal(t, `"`+tc.model+`"`, string(body["model"]))
				assert.Equal(t, "paperless-gpt", r.Header.Get("X-Title"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()
			model, err := openai.New(openai.WithModel(tc.model), openai.WithToken("test-key"),
				openai.WithBaseURL(server.URL), openai.WithHTTPClient(OpenAIHTTPClient(tc.model)))
			require.NoError(t, err)
			result, err := model.GenerateContent(context.Background(), []llms.MessageContent{
				llms.TextParts(llms.ChatMessageTypeHuman, "Transcribe the supplied document."),
			})
			require.NoError(t, err)
			require.Equal(t, "ok", result.Choices[0].Content)
		})
	}
}

// TestGPT6TransportPreservesPayloadAndReplay verifies rewriting and retry bodies.
func TestGPT6TransportPreservesPayloadAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
	}{
		{"reasoning", "POST", "/v1/chat/completions", `{"model":"gpt-6.1-terra","temperature":0,"messages":[{"role":"user","content":"private text"}],"max_completion_tokens":4096}`},
		{"none", "POST", "/v1/chat/completions", `{"model":"gpt-6.1-terra","reasoning_effort":"none","temperature":0}`},
		{"other model", "POST", "/v1/chat/completions", `{"model":"gpt-4.1-mini","temperature":0}`},
		{"other endpoint", "POST", "/v1/responses", `{"model":"gpt-6.1-terra","temperature":0}`},
		{"invalid JSON", "POST", "/v1/chat/completions", `{bad json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &gpt6Transport{base: gpt6RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
				got, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, int64(len(got)), r.ContentLength)
				replay, err := r.GetBody()
				require.NoError(t, err)
				defer replay.Close()
				replayed, err := io.ReadAll(replay)
				require.NoError(t, err)
				require.Equal(t, got, replayed)
				if tc.name != "reasoning" {
					require.Equal(t, tc.body, string(got), "unaffected requests must remain byte-identical")
				} else {
					var before, after map[string]json.RawMessage
					require.NoError(t, json.Unmarshal([]byte(tc.body), &before))
					require.NoError(t, json.Unmarshal(got, &after))
					delete(before, "temperature")
					require.Equal(t, before, after)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
			})}
			req, err := http.NewRequest(tc.method, "http://example.invalid"+tc.path, bytes.NewBufferString(tc.body))
			require.NoError(t, err)
			resp, err := transport.RoundTrip(req)
			require.NoError(t, err)
			resp.Body.Close()
		})
	}
}

type gpt6RoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip delegates test requests to the supplied assertion function.
func (f gpt6RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestOpenAIHTTPClientSkipsUnrelatedBodies checks that other models keep streaming.
func TestOpenAIHTTPClientSkipsUnrelatedBodies(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	for _, model := range []string{"gpt-4.1-mini", "openai/gpt-4.1-mini", "gpt-60", ""} {
		t.Run(model, func(t *testing.T) {
			payload := `{"model":"` + model + `","temperature":0}`
			buffer := bytes.NewBufferString(payload)
			body := &struct{ io.ReadCloser }{io.NopCloser(buffer)}
			http.DefaultTransport = gpt6RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
				require.Same(t, body, r.Body, "unrelated model body must reach the base transport untouched")
				require.Equal(t, len(payload), buffer.Len(), "body must not be read before delegation")
				return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
			})
			req, err := http.NewRequest("POST", "http://example.invalid/v1/chat/completions", body)
			require.NoError(t, err)
			resp, err := OpenAIHTTPClient(model).Do(req)
			require.NoError(t, err)
			resp.Body.Close()
		})
	}
}
