package openrouterzdr

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnforced(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{"unset", "", false},
		{"true lowercase", "true", true},
		{"true uppercase", "TRUE", true},
		{"true mixed case", "True", true},
		{"false", "false", false},
		{"arbitrary value", "1", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("OPENROUTER_ENFORCE_ZDR", c.env)
			assert.Equal(t, c.want, Enforced())
		})
	}
}

func TestInjectZDRPreference(t *testing.T) {
	t.Run("adds provider block when absent", func(t *testing.T) {
		body := []byte(`{"model":"anthropic/claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`)
		out, err := injectZDRPreference(body)
		require.NoError(t, err)

		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(out, &got))

		provider, ok := got["provider"].(map[string]interface{})
		require.True(t, ok, "expected provider object")
		assert.Equal(t, "deny", provider["data_collection"])
		assert.Equal(t, true, provider["zdr"])
		assert.Equal(t, "anthropic/claude-sonnet-4-5", got["model"])
	})

	t.Run("preserves existing provider fields", func(t *testing.T) {
		body := []byte(`{"model":"m","provider":{"order":["openai"],"allow_fallbacks":false}}`)
		out, err := injectZDRPreference(body)
		require.NoError(t, err)

		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(out, &got))

		provider, ok := got["provider"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "deny", provider["data_collection"])
		assert.Equal(t, true, provider["zdr"])
		assert.Equal(t, false, provider["allow_fallbacks"])
		order, ok := provider["order"].([]interface{})
		require.True(t, ok)
		assert.Equal(t, []interface{}{"openai"}, order)
	})

	t.Run("errors on malformed JSON", func(t *testing.T) {
		_, err := injectZDRPreference([]byte(`not json`))
		assert.Error(t, err)
	})
}

func TestIsOpenRouterHost(t *testing.T) {
	cases := []struct {
		name string
		host string
		want bool
	}{
		{"exact match", "openrouter.ai", true},
		{"uppercase", "OpenRouter.AI", true},
		{"mixed case", "OpenRouter.ai", true},
		{"subdomain", "eu.openrouter.ai", true},
		{"subdomain mixed case", "EU.OpenRouter.AI", true},
		{"lookalike suffix domain", "openrouter.ai.example.com", false},
		{"lookalike prefix domain", "myopenrouter.ai", false},
		{"unrelated host", "example.com", false},
		{"empty", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, isOpenRouterHost(c.host))
		})
	}
}

func TestTransport(t *testing.T) {
	newRecordingServer := func(t *testing.T) (*httptest.Server, *[]byte) {
		t.Helper()
		var captured []byte
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			captured = body
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)
		return server, &captured
	}

	t.Run("disabled by default: body passes through unmodified", func(t *testing.T) {
		server, captured := newRecordingServer(t)
		// OPENROUTER_ENFORCE_ZDR intentionally left unset.
		client := &http.Client{Transport: NewTransport(http.DefaultTransport)}

		reqBody := `{"model":"m"}`
		resp, err := client.Post(server.URL, "application/json", bytes.NewBufferString(reqBody))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.JSONEq(t, reqBody, string(*captured))
	})

	t.Run("enabled: injects provider preference for openrouter.ai host only", func(t *testing.T) {
		t.Setenv("OPENROUTER_ENFORCE_ZDR", "true")

		// Host matching is done via req.URL.Hostname(), so a local test
		// server (127.0.0.1:PORT) never matches "openrouter.ai" — verify the
		// injection logic directly against a request whose Host we control.
		captured := map[string]interface{}{}
		next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, &captured))
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
		})

		transport := NewTransport(next)
		req, err := http.NewRequest(http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewBufferString(`{"model":"m"}`))
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		provider, ok := captured["provider"].(map[string]interface{})
		require.True(t, ok, "expected provider object in request sent upstream")
		assert.Equal(t, "deny", provider["data_collection"])
		assert.Equal(t, true, provider["zdr"])
	})

	t.Run("enabled: mixed-case openrouter.ai host still matches", func(t *testing.T) {
		t.Setenv("OPENROUTER_ENFORCE_ZDR", "true")

		captured := map[string]interface{}{}
		next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, &captured))
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
		})

		transport := NewTransport(next)
		req, err := http.NewRequest(http.MethodPost, "https://OpenRouter.AI/api/v1/chat/completions", bytes.NewBufferString(`{"model":"m"}`))
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		provider, ok := captured["provider"].(map[string]interface{})
		require.True(t, ok, "expected provider object in request sent upstream")
		assert.Equal(t, "deny", provider["data_collection"])
	})

	t.Run("enabled but lookalike host: passes through unmodified", func(t *testing.T) {
		t.Setenv("OPENROUTER_ENFORCE_ZDR", "true")

		var captured []byte
		next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			captured = body
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
		})

		transport := NewTransport(next)
		reqBody := `{"model":"m"}`
		req, err := http.NewRequest(http.MethodPost, "https://openrouter.ai.attacker.example/api/v1/chat/completions", bytes.NewBufferString(reqBody))
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.JSONEq(t, reqBody, string(captured))
	})

	t.Run("enabled but non-OpenRouter host: passes through unmodified", func(t *testing.T) {
		t.Setenv("OPENROUTER_ENFORCE_ZDR", "true")
		server, captured := newRecordingServer(t)
		client := &http.Client{Transport: NewTransport(http.DefaultTransport)}

		reqBody := `{"model":"m"}`
		resp, err := client.Post(server.URL, "application/json", bytes.NewBufferString(reqBody))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.JSONEq(t, reqBody, string(*captured))
	})
}

// roundTripFunc adapts a function to the http.RoundTripper interface for
// tests that need to assert on the outgoing request without a real network
// call to a host matching "openrouter.ai" (which httptest.Server cannot
// provide, since it always listens on 127.0.0.1).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Guard against t.Setenv leaking into unrelated parallel tests in this
// package — none of the tests above run t.Parallel(), so this is currently
// unnecessary, but os.Unsetenv here documents the intent explicitly for
// anyone adding a parallel test later.
func TestMain_openrouterZDREnvIsolation(t *testing.T) {
	t.Cleanup(func() { _ = os.Unsetenv("OPENROUTER_ENFORCE_ZDR") })
}
