package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findEntry(entries []ConfigEntry, name string) *ConfigEntry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}

func TestBuildConfigEntriesNeverExposesSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-super-secret-value")

	entries := buildConfigEntries()
	e := findEntry(entries, "OPENAI_API_KEY")
	require.NotNil(t, e)
	assert.True(t, e.Secret)
	assert.True(t, e.IsSet, "a set secret reports is_set=true")
	assert.Empty(t, e.Value, "a secret value must never be emitted")
	assert.Equal(t, "env", e.Source)
}

func TestBuildConfigEntriesNeverExposesOllamaHeaders(t *testing.T) {
	t.Setenv("OLLAMA_HEADERS", "Authorization=Bearer private-token,X-API-Key=private-key")

	e := findEntry(buildConfigEntries(), "OLLAMA_HEADERS")
	require.NotNil(t, e)
	assert.True(t, e.Secret)
	assert.True(t, e.IsSet)
	assert.Empty(t, e.Value, "credential-bearing headers must not appear in configuration responses")
	assert.Equal(t, "env", e.Source)
}

func TestBuildConfigEntriesScrubsURLUserinfo(t *testing.T) {
	t.Setenv("PAPERLESS_BASE_URL", "https://user:hunter2@paperless.example.com:8000")
	t.Setenv("MINICPM_OCR_BASE_URL", "https://user:s3cret@ocr.example/v1")

	e := findEntry(buildConfigEntries(), "PAPERLESS_BASE_URL")
	require.NotNil(t, e)
	assert.NotContains(t, e.Value, "hunter2", "embedded credentials must be scrubbed")
	assert.Contains(t, e.Value, "paperless.example.com")

	mini := findEntry(buildConfigEntries(), "MINICPM_OCR_BASE_URL")
	require.NotNil(t, mini)
	assert.NotContains(t, mini.Value, "s3cret")
	assert.Contains(t, mini.Value, "ocr.example")
}

func TestBuildConfigEntriesReportsSavedOverride(t *testing.T) {
	settingsMutex.Lock()
	orig := settings
	limit := 11
	settings.OCR = OCRDefaults{LimitPages: &limit}
	settingsMutex.Unlock()
	defer func() {
		settingsMutex.Lock()
		settings = orig
		settingsMutex.Unlock()
	}()

	e := findEntry(buildConfigEntries(), "OCR_LIMIT_PAGES")
	require.NotNil(t, e)
	assert.Equal(t, "saved", e.Source)
	assert.Equal(t, "11", e.Value)
	assert.Equal(t, "/ocr", e.EditableAt)
}

func TestBuildConfigEntriesUnsetIsDefault(t *testing.T) {
	// PAPERLESS_PUBLIC_URL is unlikely to be set in the test environment.
	if _, set := os.LookupEnv("PAPERLESS_PUBLIC_URL"); set {
		t.Skip("PAPERLESS_PUBLIC_URL is set in this environment")
	}
	e := findEntry(buildConfigEntries(), "PAPERLESS_PUBLIC_URL")
	require.NotNil(t, e)
	assert.Equal(t, "default", e.Source)
	assert.False(t, e.IsSet)
	assert.Empty(t, e.Value)
}

func TestScrubURLUserinfo(t *testing.T) {
	assert.Equal(t, "http://host:8000", scrubURLUserinfo("http://host:8000"))
	assert.NotContains(t, scrubURLUserinfo("http://u:p@host:8000"), "p@")
	assert.Contains(t, scrubURLUserinfo("http://u:p@host:8000"), "host:8000")
	// A value that is not an http(s) URL is masked rather than echoed.
	assert.Equal(t, redactedConfigURL, scrubURLUserinfo("not a url"))
}

func TestMalformedURLCredentialsAreMaskedWhenAnotherProviderIsActive(t *testing.T) {
	// The config view lists every URL setting, including a MiniCPM URL left
	// over while a different OCR provider is selected.
	t.Setenv("OCR_PROVIDER", "llm")

	cases := []struct {
		name   string
		value  string
		secret string
	}{
		{name: "space in host", value: "http://user:pass@host:port with space", secret: "user:pass@"},
		{name: "missing scheme", value: "://user:secret@x", secret: "user:secret@"},
		{name: "bad percent-escape", value: "http://user:p%zz@ocr.example/v1", secret: "p%zz"},
		{name: "opaque userinfo", value: "user:password@ocr.example/v1", secret: "user:password@"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MINICPM_OCR_BASE_URL", tc.value)
			t.Setenv("OPENAI_BASE_URL", tc.value)

			entries := buildConfigEntries()
			encoded, err := json.Marshal(entries)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), tc.secret)

			mini := findEntry(entries, "MINICPM_OCR_BASE_URL")
			require.NotNil(t, mini)
			assert.Equal(t, redactedConfigURL, mini.Value)
			assert.Equal(t, "env", mini.Source)

			openai := findEntry(entries, "OPENAI_BASE_URL")
			require.NotNil(t, openai)
			assert.Equal(t, redactedConfigURL, openai.Value)
			assert.NotContains(t, openai.Value, tc.secret)
		})
	}
}

func TestMiniCPMURLQueryCredentialsAreHidden(t *testing.T) {
	t.Setenv("MINICPM_OCR_BASE_URL", "https://user:password@ocr.example/v1?token=secret")
	entry := findEntry(buildConfigEntries(), "MINICPM_OCR_BASE_URL")
	require.NotNil(t, entry)
	assert.NotContains(t, entry.Value, "password")
	assert.NotContains(t, entry.Value, "secret")
	assert.Contains(t, entry.Value, "ocr.example/v1")
}
