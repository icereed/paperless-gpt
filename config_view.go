package main

import (
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ConfigEntry is the diagnostic view of one environment variable: its
// documentation plus the *effective* runtime state, with secrets never
// exposed as values.
type ConfigEntry struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Secret      bool   `json:"secret"`
	Default     string `json:"default"`
	// Source of the effective value: "env" (set in the environment),
	// "saved" (a UI-saved setting shadows the env value) or "default".
	Source string `json:"source"`
	// IsSet reports whether the environment variable is present, regardless of
	// source. For secrets this is the only signal exposed.
	IsSet bool `json:"is_set"`
	// Value is the effective, non-secret value (env value or saved override),
	// with any URL userinfo scrubbed. Empty for secrets and for unset vars.
	Value string `json:"value,omitempty"`
	// EditableAt names an in-app route where this value can be changed, when
	// one exists (e.g. the OCR run-option defaults).
	EditableAt string `json:"editable_at,omitempty"`
}

// urlEnvVars hold URLs whose embedded credentials (user:pass@host) must be
// scrubbed before display even though the variable itself is not a secret.
var urlEnvVars = map[string]bool{
	"PAPERLESS_BASE_URL":   true,
	"PAPERLESS_PUBLIC_URL": true,
	"OPENAI_BASE_URL":      true,
	"OLLAMA_HOST":          true,
	"DOCLING_URL":          true,
}

// scrubURLUserinfo removes any user:password@ component from a URL so
// credentials embedded in a connection string never reach the UI, replacing
// it with a plain "***@" marker (url.UserPassword would percent-encode it).
func scrubURLUserinfo(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return strings.Replace(u.String(), "://", "://***@", 1)
}

// ocrOverrideValue returns the UI-saved override for the env vars that the OCR
// defaults can shadow, or ("", false) when no override applies.
func ocrOverrideValue(name string) (string, bool) {
	settingsMutex.RLock()
	defer settingsMutex.RUnlock()
	o := settings.OCR
	switch name {
	case "OCR_LIMIT_PAGES":
		if o.LimitPages != nil {
			return strconv.Itoa(*o.LimitPages), true
		}
	case "OCR_PROCESS_MODE":
		if o.ProcessMode != nil && *o.ProcessMode != "" {
			return *o.ProcessMode, true
		}
	case "PDF_UPLOAD":
		if o.UploadPDF != nil {
			return strconv.FormatBool(*o.UploadPDF), true
		}
	case "PDF_REPLACE":
		if o.ReplaceOriginal != nil {
			return strconv.FormatBool(*o.ReplaceOriginal), true
		}
	case "PDF_COPY_METADATA":
		if o.CopyMetadata != nil {
			return strconv.FormatBool(*o.CopyMetadata), true
		}
	}
	return "", false
}

// buildConfigEntries assembles the effective configuration view from the
// registry and the current environment/settings.
func buildConfigEntries() []ConfigEntry {
	entries := make([]ConfigEntry, 0, len(envRegistry))
	for _, e := range envRegistry {
		envVal, isSet := os.LookupEnv(e.Name)

		entry := ConfigEntry{
			Name:        e.Name,
			Category:    e.Category,
			Description: e.Description,
			Secret:      e.Secret,
			Default:     e.Default,
			IsSet:       isSet,
		}

		if saved, ok := ocrOverrideValue(e.Name); ok {
			// A UI-saved OCR default shadows the environment. This must be
			// visible so the environment never silently lies.
			entry.Source = "saved"
			entry.EditableAt = "/ocr"
			if !e.Secret {
				entry.Value = saved
			}
		} else if isSet {
			entry.Source = "env"
			if !e.Secret {
				if urlEnvVars[e.Name] {
					entry.Value = scrubURLUserinfo(envVal)
				} else {
					entry.Value = envVal
				}
			}
			if e.Name == "OCR_LIMIT_PAGES" || e.Name == "OCR_PROCESS_MODE" ||
				e.Name == "PDF_UPLOAD" || e.Name == "PDF_REPLACE" || e.Name == "PDF_COPY_METADATA" {
				entry.EditableAt = "/ocr"
			}
		} else {
			entry.Source = "default"
		}

		entries = append(entries, entry)
	}
	return entries
}

// getConfigHandler serves the read-only configuration diagnostics view:
// every known setting with its effective value, source, and meaning. Secret
// values are never emitted — only whether they are set.
func (app *App) getConfigHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"categories": envCategoryOrder,
		"entries":    buildConfigEntries(),
	})
}
