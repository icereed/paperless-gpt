package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	configDir    = "config"
	settingsFile = "settings.json"
)

// effectiveOCRDefaults resolves the Run Options used when a run doesn't
// specify its own: settings-persisted values (editable from the UI) override
// the env-derived ones. Auto-OCR always runs with these defaults.
func (app *App) effectiveOCRDefaults() OCROptions {
	settingsMutex.RLock()
	defer settingsMutex.RUnlock()

	opts := OCROptions{
		UploadPDF:       app.pdfUpload,
		ReplaceOriginal: app.pdfReplace,
		CopyMetadata:    app.pdfCopyMetadata,
		LimitPages:      limitOcrPages,
		ProcessMode:     app.ocrProcessMode,
	}
	o := settings.OCR
	if o.LimitPages != nil {
		opts.LimitPages = *o.LimitPages
	}
	if o.ProcessMode != nil && *o.ProcessMode != "" {
		opts.ProcessMode = *o.ProcessMode
	}
	if o.UploadPDF != nil {
		opts.UploadPDF = *o.UploadPDF
	}
	if o.ReplaceOriginal != nil {
		opts.ReplaceOriginal = *o.ReplaceOriginal
	}
	if o.CopyMetadata != nil {
		opts.CopyMetadata = *o.CopyMetadata
	}
	// Replace without upload is invalid; never let persisted defaults create it.
	if !opts.UploadPDF {
		opts.ReplaceOriginal = false
	}
	return opts
}

// updateOCRDefaults merges new OCR defaults into settings and persists them.
func updateOCRDefaults(defaults OCRDefaults) error {
	settingsMutex.Lock()
	defer settingsMutex.Unlock()
	settings.OCR = defaults
	return saveSettingsLocked()
}

// ocrDefaultsSources reports, per run option, whether the effective value
// comes from the environment or from UI-saved settings. Saved values shadow
// env values; the shadowing must always be visible (UI, API, startup log) so
// the environment never silently lies to the operator.
func ocrDefaultsSources() map[string]string {
	settingsMutex.RLock()
	defer settingsMutex.RUnlock()

	source := func(saved bool) string {
		if saved {
			return "saved"
		}
		return "env"
	}
	o := settings.OCR
	return map[string]string{
		"limit_pages":      source(o.LimitPages != nil),
		"process_mode":     source(o.ProcessMode != nil && *o.ProcessMode != ""),
		"upload_pdf":       source(o.UploadPDF != nil),
		"replace_original": source(o.ReplaceOriginal != nil),
		"copy_metadata":    source(o.CopyMetadata != nil),
	}
}

// logOCRSettingsOverrides makes shadowed env values visible at startup.
func logOCRSettingsOverrides() {
	var overridden []string
	for field, src := range ocrDefaultsSources() {
		if src == "saved" {
			overridden = append(overridden, field)
		}
	}
	if len(overridden) > 0 {
		sort.Strings(overridden)
		log.Infof("OCR defaults from config/settings.json override env values for: %s (reset via the OCR page or DELETE /api/ocr/defaults)", strings.Join(overridden, ", "))
	}
}

// saveSettings saves the current settings to the settings.json file.
func saveSettings() error {
	settingsMutex.Lock()
	defer settingsMutex.Unlock()
	return saveSettingsLocked()
}

// saveSettingsLocked performs the actual saving without locking the mutex.
// This is to be called from functions that already hold the lock.
func saveSettingsLocked() error {
	// Ensure the config directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	// Marshal the settings struct to JSON
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	// Write the file
	return os.WriteFile(filepath.Join(configDir, settingsFile), data, 0644)
}

// loadSettings loads the settings from settings.json, creating it with defaults if it doesn't exist or is corrupt.
func loadSettings() {
	settingsMutex.Lock()
	defer settingsMutex.Unlock()

	settingsPath := filepath.Join(configDir, settingsFile)
	data, err := os.ReadFile(settingsPath)

	// Define default settings
	loadDefaultSettings := func() {
		settings = Settings{
			CustomFieldsEnable:      false,
			CustomFieldsSelectedIDs: []int{},
			CustomFieldsWriteMode:   "append",
		}
	}

	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, create it with defaults
			log.Infof("Settings file not found at %s, creating with default values.", settingsPath)
			loadDefaultSettings()
			if err := saveSettingsLocked(); err != nil {
				log.Fatalf("Failed to create default settings file: %v", err)
			}
		} else {
			// Another error occurred while reading
			log.Warnf("Failed to read settings file: %v. Loading default settings.", err)
			loadDefaultSettings()
		}
		return
	}

	// File exists, so unmarshal it
	if err := json.Unmarshal(data, &settings); err != nil {
		log.Warnf("Failed to parse settings file, please check its format. Loading default settings. Error: %v", err)
		loadDefaultSettings()
		return
	}

	log.Info("Successfully loaded settings from settings.json")
}
