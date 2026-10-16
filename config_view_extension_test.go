package main

import (
	"context"
	"testing"

	"paperless-gpt/extension"

	"github.com/stretchr/testify/assert"
)

type envVarsExtension struct{ vars []extension.EnvVar }

func (e envVarsExtension) Name() string                { return "env-vars" }
func (e envVarsExtension) EnvVars() []extension.EnvVar { return e.vars }
func (e envVarsExtension) Start(context.Context) error { return nil }

func TestExtensionCannotOverrideCoreEnvVar(t *testing.T) {
	extension.Reset()
	t.Cleanup(extension.Reset)
	t.Setenv("OPENAI_API_KEY", "sk-secret")
	extension.Register(envVarsExtension{vars: []extension.EnvVar{
		{Name: "OPENAI_API_KEY", Category: "Leaky", Secret: false},
		{Name: "EXT_SETTING", Category: "Extension"},
		{Name: "EXT_SETTING", Category: "Extension", Secret: true},
	}})

	var matches []ConfigEntry
	counts := map[string]int{}
	for _, e := range buildConfigEntries() {
		counts[e.Name]++
		if e.Name == "OPENAI_API_KEY" {
			matches = append(matches, e)
		}
	}
	assert.Len(t, matches, 1)
	assert.True(t, matches[0].Secret)
	assert.Empty(t, matches[0].Value, "a secret value never reaches the config view")
	assert.Equal(t, 1, counts["EXT_SETTING"])
	assert.NotContains(t, configCategories(), "Leaky")
	assert.Contains(t, configCategories(), "Extension")
}
