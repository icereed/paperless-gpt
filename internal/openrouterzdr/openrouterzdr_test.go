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

	t.Run("preserves large integer precision in untouched fields", func(t *testing.T) {
		// 9007199254740993 is 2^53 + 1, the smallest positive integer that
		// cannot be represented exactly as a float64. A naive
		// json.Unmarshal into map[string]interface{} decodes all numbers as
		// float64 and would silently round this down to 9007199254740992
		// on re-marshal.
		body := []byte(`{"model":"m","seed":9007199254740993}`)
		out, err := injectZDRPreference(body)
		require.NoError(t, err)
		assert.Contains(t, string(out), `"seed":9007199254740993`)
	})

	t.Run("errors on trailing data after the top-level JSON value", func(t *testing.T) {
		// json.Decoder.Decode reads exactly one JSON value and stops,
		// unlike json.Unmarshal, which rejects any input with data left
		// over after the value. Without an explicit trailing-data check,
		// a body like this would "succeed", silently keeping only the
		// first object and dropping the second one on re-marshal.
		body := []byte(`{"model":"m"}{"messages":[]}`)
		_, err := injectZDRPreference(body)
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

	t.Run("enabled: GetBody is updated so retries also carry the injected preference", func(t *testing.T) {
		t.Setenv("OPENROUTER_ENFORCE_ZDR", "true")

		// http.Transport rewinds a request for a retry via GetBody rather
		// than reusing req.Body directly. Simulate that by building a
		// request the same way http.NewRequest does for a bytes.Buffer
		// body (which populates GetBody), running it through the
		// transport once, and then confirming a *second* read via
		// GetBody() also observes the injected provider preference.
		reqBody := `{"model":"m"}`
		req, err := http.NewRequest(http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewBufferString(reqBody))
		require.NoError(t, err)
		require.NotNil(t, req.GetBody, "http.NewRequest with a *bytes.Buffer body should populate GetBody")

		next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			// Drain but discard; this test only cares about GetBody below.
			_, _ = io.ReadAll(req.Body)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
		})

		transport := NewTransport(next)
		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.NotNil(t, req.GetBody, "GetBody should still be set after RoundTrip")
		retryBody, err := req.GetBody()
		require.NoError(t, err)
		retryBytes, err := io.ReadAll(retryBody)
		require.NoError(t, err)

		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(retryBytes, &got))
		provider, ok := got["provider"].(map[string]interface{})
		require.True(t, ok, "expected provider object in the body a retry would read via GetBody")
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

// A body of just `null` is not an object; it must be refused, not panic.
func TestInjectZDRPreferenceRejectsNull(t *testing.T) {
	if _, err := injectZDRPreference([]byte(`null`)); err == nil {
		t.Fatal("expected an error for a null body")
	}
}

func TestProviderOnly(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want []string
	}{
		{"unset", "", nil},
		{"single slug", "azure/eu", []string{"azure/eu"}},
		{"multiple slugs", "azure/eu,openai", []string{"azure/eu", "openai"}},
		{"whitespace is trimmed", "  azure/eu , openai  ", []string{"azure/eu", "openai"}},
		{"empty entries are dropped", ",azure/eu,, ,openai,", []string{"azure/eu", "openai"}},
		{"only whitespace and commas", " , ,", nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("OPENROUTER_PROVIDER_ONLY", c.env)
			assert.Equal(t, c.want, ProviderOnly())
		})
	}
}

func TestInjectProviderPreferences(t *testing.T) {
	decodeProvider := func(t *testing.T, out []byte) map[string]interface{} {
		t.Helper()
		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(out, &got))
		provider, ok := got["provider"].(map[string]interface{})
		require.True(t, ok, "expected provider object")
		return provider
	}

	t.Run("only: adds provider.only without ZDR fields", func(t *testing.T) {
		out, err := injectProviderPreferences([]byte(`{"model":"m"}`), false, []string{"azure/eu"})
		require.NoError(t, err)

		provider := decodeProvider(t, out)
		assert.Equal(t, []interface{}{"azure/eu"}, provider["only"])
		assert.NotContains(t, provider, "zdr")
		assert.NotContains(t, provider, "data_collection")
	})

	t.Run("only and ZDR: both land in one provider object", func(t *testing.T) {
		out, err := injectProviderPreferences([]byte(`{"model":"m"}`), true, []string{"azure/eu", "openai"})
		require.NoError(t, err)

		provider := decodeProvider(t, out)
		assert.Equal(t, []interface{}{"azure/eu", "openai"}, provider["only"])
		assert.Equal(t, "deny", provider["data_collection"])
		assert.Equal(t, true, provider["zdr"])
	})

	t.Run("only: preserves existing provider fields and untouched body fields", func(t *testing.T) {
		body := []byte(`{"model":"m","seed":9007199254740993,"provider":{"allow_fallbacks":false,"order":["openai"]}}`)
		out, err := injectProviderPreferences(body, false, []string{"azure/eu"})
		require.NoError(t, err)

		provider := decodeProvider(t, out)
		assert.Equal(t, []interface{}{"azure/eu"}, provider["only"])
		assert.Equal(t, false, provider["allow_fallbacks"])
		assert.Equal(t, []interface{}{"openai"}, provider["order"])
		assert.Contains(t, string(out), `"seed":9007199254740993`)
	})

	t.Run("only: replaces an existing only list", func(t *testing.T) {
		out, err := injectProviderPreferences([]byte(`{"provider":{"only":["openai"]}}`), false, []string{"azure/eu"})
		require.NoError(t, err)
		assert.Equal(t, []interface{}{"azure/eu"}, decodeProvider(t, out)["only"])
	})

	t.Run("ZDR only: injectZDRPreference behaves as before", func(t *testing.T) {
		out, err := injectZDRPreference([]byte(`{"model":"m"}`))
		require.NoError(t, err)

		provider := decodeProvider(t, out)
		assert.Equal(t, "deny", provider["data_collection"])
		assert.Equal(t, true, provider["zdr"])
		assert.NotContains(t, provider, "only")
	})

	t.Run("errors on malformed JSON", func(t *testing.T) {
		_, err := injectProviderPreferences([]byte(`not json`), false, []string{"azure/eu"})
		assert.Error(t, err)
	})
}

func TestTransportProviderOnly(t *testing.T) {
	// do sends body through the transport to url and returns the JSON the
	// upstream RoundTripper received.
	do := func(t *testing.T, url, body string) map[string]interface{} {
		t.Helper()
		var captured map[string]interface{}
		next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			b, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(b, &captured))
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
		})
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
		require.NoError(t, err)
		resp, err := NewTransport(next).RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		return captured
	}

	const orURL = "https://openrouter.ai/api/v1/chat/completions"

	t.Run("injects provider.only without ZDR enforcement", func(t *testing.T) {
		t.Setenv("OPENROUTER_PROVIDER_ONLY", "azure/eu")

		provider, ok := do(t, orURL, `{"model":"m"}`)["provider"].(map[string]interface{})
		require.True(t, ok, "expected provider object in request sent upstream")
		assert.Equal(t, []interface{}{"azure/eu"}, provider["only"])
		assert.NotContains(t, provider, "zdr")
	})

	t.Run("combines with ZDR enforcement", func(t *testing.T) {
		t.Setenv("OPENROUTER_ENFORCE_ZDR", "true")
		t.Setenv("OPENROUTER_PROVIDER_ONLY", " azure/eu , ")

		provider, ok := do(t, orURL, `{"model":"m"}`)["provider"].(map[string]interface{})
		require.True(t, ok, "expected provider object in request sent upstream")
		assert.Equal(t, []interface{}{"azure/eu"}, provider["only"])
		assert.Equal(t, "deny", provider["data_collection"])
		assert.Equal(t, true, provider["zdr"])
	})

	t.Run("blank value is ignored: body passes through unmodified", func(t *testing.T) {
		t.Setenv("OPENROUTER_PROVIDER_ONLY", " , ")

		got := do(t, orURL, `{"model":"m"}`)
		assert.NotContains(t, got, "provider")
	})

	t.Run("regional subdomain host still matches", func(t *testing.T) {
		t.Setenv("OPENROUTER_PROVIDER_ONLY", "azure/eu")

		provider, ok := do(t, "https://eu.openrouter.ai/api/v1/chat/completions", `{"model":"m"}`)["provider"].(map[string]interface{})
		require.True(t, ok, "expected provider object in request sent upstream")
		assert.Equal(t, []interface{}{"azure/eu"}, provider["only"])
	})

	t.Run("non-OpenRouter host: passes through unmodified", func(t *testing.T) {
		t.Setenv("OPENROUTER_PROVIDER_ONLY", "azure/eu")

		got := do(t, "https://openrouter.ai.attacker.example/api/v1/chat/completions", `{"model":"m"}`)
		assert.NotContains(t, got, "provider")
	})

	t.Run("GetBody is updated so retries also carry provider.only", func(t *testing.T) {
		t.Setenv("OPENROUTER_PROVIDER_ONLY", "azure/eu")

		req, err := http.NewRequest(http.MethodPost, orURL, bytes.NewBufferString(`{"model":"m"}`))
		require.NoError(t, err)
		require.NotNil(t, req.GetBody)

		next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			_, _ = io.ReadAll(req.Body)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
		})
		resp, err := NewTransport(next).RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		retryBody, err := req.GetBody()
		require.NoError(t, err)
		retryBytes, err := io.ReadAll(retryBody)
		require.NoError(t, err)
		assert.Contains(t, string(retryBytes), `"only":["azure/eu"]`)
	})
}
