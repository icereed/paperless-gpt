package main

import (
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

func TestBuildConfigEntriesScrubsURLUserinfo(t *testing.T) {
	t.Setenv("PAPERLESS_BASE_URL", "https://user:hunter2@paperless.example.com:8000")

	e := findEntry(buildConfigEntries(), "PAPERLESS_BASE_URL")
	require.NotNil(t, e)
	assert.NotContains(t, e.Value, "hunter2", "embedded credentials must be scrubbed")
	assert.Contains(t, e.Value, "paperless.example.com")
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
	// non-URL input is returned unchanged
	assert.Equal(t, "not a url", scrubURLUserinfo("not a url"))
}
