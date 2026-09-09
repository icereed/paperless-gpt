package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

var ollamaMetadataPromptNames = map[string]bool{
	"adhoc-analysis_prompt.tmpl": true,
	"correspondent_prompt.tmpl":  true,
	"created_date_prompt.tmpl":   true,
	"custom_field_prompt.tmpl":   true,
	"document_type_prompt.tmpl":  true,
	"tag_prompt.tmpl":            true,
	"title_prompt.tmpl":          true,
}

var ollamaStructuredOutputPromptNames = map[string]bool{
	"adhoc-analysis_prompt.tmpl": true,
	"custom_field_prompt.tmpl":   true,
}

// loadOllamaMetadataConfig loads the optional, startup-only metadata settings
// file. An explicitly configured file is intentionally strict so typos do not
// silently change model behavior.
func loadOllamaMetadataConfig() (OllamaMetadataConfig, error) {
	path := os.Getenv("OLLAMA_SETTINGS_FILE")
	if path == "" {
		return OllamaMetadataConfig{}, nil
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return OllamaMetadataConfig{}, fmt.Errorf("read OLLAMA_SETTINGS_FILE: %w", err)
	}
	if trimmed := strings.TrimSpace(string(contents)); len(trimmed) == 0 || trimmed[0] != '{' {
		return OllamaMetadataConfig{}, fmt.Errorf("decode OLLAMA_SETTINGS_FILE: expected a JSON object")
	}
	if err := validateOllamaMetadataConfigFileShape(contents); err != nil {
		return OllamaMetadataConfig{}, err
	}

	var config OllamaMetadataConfig
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return OllamaMetadataConfig{}, fmt.Errorf("decode OLLAMA_SETTINGS_FILE: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return OllamaMetadataConfig{}, fmt.Errorf("decode OLLAMA_SETTINGS_FILE: multiple JSON values")
		}
		return OllamaMetadataConfig{}, fmt.Errorf("decode OLLAMA_SETTINGS_FILE: %w", err)
	}

	if err := validateOllamaMetadataConfig(config); err != nil {
		return OllamaMetadataConfig{}, err
	}
	if config.Defaults.Format != nil {
		return OllamaMetadataConfig{}, fmt.Errorf("OLLAMA_SETTINGS_FILE: format is not allowed in defaults")
	}
	for filename, settings := range config.Prompts {
		if !ollamaMetadataPromptNames[filename] {
			return OllamaMetadataConfig{}, fmt.Errorf("OLLAMA_SETTINGS_FILE: unsupported prompt %q", filename)
		}
		if settings.Format != nil && !ollamaStructuredOutputPromptNames[filename] {
			return OllamaMetadataConfig{}, fmt.Errorf("OLLAMA_SETTINGS_FILE: format is only allowed for custom_field_prompt.tmpl and adhoc-analysis_prompt.tmpl")
		}
		if filename == "custom_field_prompt.tmpl" && settings.Format != nil {
			if err := validateCustomFieldOllamaFormat(settings.Format); err != nil {
				return OllamaMetadataConfig{}, err
			}
		}
	}

	return config, nil
}

// validateOllamaMetadataConfigFileShape rejects explicit null values in the
// external settings file. Omission is the only inheritance marker in this
// format. JSON schemas stored in format are deliberately opaque here, because
// null is valid inside a schema (for example, a const value).
func validateOllamaMetadataConfigFileShape(contents []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(contents, &root); err != nil {
		return fmt.Errorf("decode OLLAMA_SETTINGS_FILE: %w", err)
	}
	if defaults, ok := root["defaults"]; ok {
		if err := validateOllamaSettingsObject(defaults, "defaults"); err != nil {
			return err
		}
	}
	if prompts, ok := root["prompts"]; ok {
		if isJSONNull(prompts) {
			return fmt.Errorf("OLLAMA_SETTINGS_FILE: prompts must be a JSON object, not null")
		}
		var promptSettings map[string]json.RawMessage
		if err := json.Unmarshal(prompts, &promptSettings); err != nil || promptSettings == nil {
			return fmt.Errorf("OLLAMA_SETTINGS_FILE: prompts must be a JSON object")
		}
		for filename, settings := range promptSettings {
			if err := validateOllamaSettingsObject(settings, fmt.Sprintf("prompt %q", filename)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOllamaSettingsObject(raw json.RawMessage, name string) error {
	if isJSONNull(raw) {
		return fmt.Errorf("OLLAMA_SETTINGS_FILE: %s must be a JSON object, not null", name)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(raw, &settings); err != nil || settings == nil {
		return fmt.Errorf("OLLAMA_SETTINGS_FILE: %s must be a JSON object", name)
	}
	for setting, value := range settings {
		if isJSONNull(value) {
			return fmt.Errorf("OLLAMA_SETTINGS_FILE: %s setting %q must be omitted instead of null", name, setting)
		}
	}
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func validateCustomFieldOllamaFormat(raw json.RawMessage) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("OLLAMA_SETTINGS_FILE: invalid custom field format: %w", err)
	}
	if _, ok := value.(string); ok {
		return nil // "json" is valid and retains the existing parser contract.
	}
	schema, ok := value.(map[string]any)
	if !ok || schema["type"] != "array" {
		return fmt.Errorf("OLLAMA_SETTINGS_FILE: custom_field_prompt.tmpl schema must have top-level type \"array\"")
	}
	return nil
}

// applyOllamaMetadataEnvironment overlays valid global metadata settings on
// file defaults. Prompt-specific settings are left intact for the adapter to
// apply later, after these global settings.
func applyOllamaMetadataEnvironment(config *OllamaMetadataConfig) {
	if value := os.Getenv("LLM_TEMPERATURE"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			log.Warnf("Invalid LLM_TEMPERATURE value %q, ignoring (must be a non-negative finite number)", value)
		} else {
			config.Defaults.Temperature = &parsed
		}
	}

	if value := os.Getenv("LLM_MAX_TOKENS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || (parsed <= 0 && parsed != -1) {
			log.Warnf("Invalid LLM_MAX_TOKENS value %q, ignoring (must be positive or -1)", value)
		} else {
			config.Defaults.MaxTokens = &parsed
		}
	}

	if value := os.Getenv("OLLAMA_CONTEXT_LENGTH"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			log.Warnf("Invalid OLLAMA_CONTEXT_LENGTH value %q, ignoring (must be positive)", value)
		} else if parsed > 0 {
			config.Defaults.ContextLength = &parsed
		}
	}

	if value := os.Getenv("OLLAMA_KEEP_ALIVE"); value != "" {
		if _, err := parseOllamaKeepAlive(value); err != nil {
			log.Warnf("Invalid OLLAMA_KEEP_ALIVE value %q, ignoring", value)
		} else {
			config.Defaults.KeepAlive = &value
		}
	}

	if value := os.Getenv("OLLAMA_THINK"); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			config.Defaults.Think = json.RawMessage(strconv.FormatBool(parsed))
		} else if value == "low" || value == "medium" || value == "high" {
			encoded, _ := json.Marshal(value)
			config.Defaults.Think = encoded
		} else {
			log.Warnf("Invalid OLLAMA_THINK value %q, ignoring (must be true, false, low, medium, or high)", value)
		}
	}
}
