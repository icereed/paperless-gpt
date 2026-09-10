package main

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
)

// applyOllamaMetadataEnvironment layers valid environment values onto the
// metadata generation defaults. Invalid values are warned about and ignored
// rather than failing startup, matching how the other numeric env vars behave.
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
