package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func defaultSettings() Settings {
	return Settings{
		CustomFieldsEnable:               false,
		CustomFieldsSelectedIDs:          []int{},
		CustomFieldsWriteMode:            "append",
		RestrictTagsToExisting:           false,
		RestrictCorrespondentsToExisting: false,
		RestrictDocumentTypesToExisting:  false,
		JobberEnabled:                    false,
		JobberExpenseEnabled:             false,
		GoogleDriveEnabled:               false,
		QuickBooksEnabled:                false,
	}
}

const (
	configDir    = "config"
	settingsFile = "settings.json"
)

// saveSettings saves the current settings to the settings.json file.
func saveSettings() error {
	settingsMutex.Lock()
	defer settingsMutex.Unlock()
	return saveSettingsLocked()
}

// saveSettingsLocked performs the actual saving without locking the mutex.
// This is to be called from functions that already hold the lock.
func saveSettingsLocked() error {
	// Ensure the config directory exists (owner-only to protect secrets inside)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}

	// Webhook secrets are persisted in the DB, not settings.json.
	settingsForFile := settings
	settingsForFile.PaperlessWebhookSecret = ""

	// Marshal the settings struct to JSON
	data, err := json.MarshalIndent(settingsForFile, "", "  ")
	if err != nil {
		return err
	}

	// Write the file with owner-read-write only (contains API tokens / field IDs)
	return os.WriteFile(filepath.Join(configDir, settingsFile), data, 0600)
}

// loadSettings loads the settings from settings.json, creating it with defaults if it doesn't exist or is corrupt.
func loadSettings() {
	settingsMutex.Lock()
	defer settingsMutex.Unlock()

	settingsPath := filepath.Join(configDir, settingsFile)
	data, err := os.ReadFile(settingsPath)

	// Define default settings
	loadDefaultSettings := func() {
		settings = defaultSettings()
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

	// Normalize zero-value slices / defaults for fields that may not exist in older settings files.
	defaults := defaultSettings()
	if settings.CustomFieldsSelectedIDs == nil {
		settings.CustomFieldsSelectedIDs = defaults.CustomFieldsSelectedIDs
	}
	if settings.CustomFieldsWriteMode == "" {
		settings.CustomFieldsWriteMode = defaults.CustomFieldsWriteMode
	}
	if settings.JobberExpenseTitleFieldRef == "" && settings.JobberExpenseTitleFieldID > 0 {
		settings.JobberExpenseTitleFieldRef = customFieldReference(settings.JobberExpenseTitleFieldID)
	}
	if settings.JobberExpenseDescriptionFieldRef == "" && settings.JobberExpenseDescriptionFieldID > 0 {
		settings.JobberExpenseDescriptionFieldRef = customFieldReference(settings.JobberExpenseDescriptionFieldID)
	}
	if settings.JobberExpenseDateFieldRef == "" && settings.JobberExpenseDateFieldID > 0 {
		settings.JobberExpenseDateFieldRef = customFieldReference(settings.JobberExpenseDateFieldID)
	}
	if settings.JobberExpenseTotalFieldRef == "" && settings.JobberExpenseTotalFieldID > 0 {
		settings.JobberExpenseTotalFieldRef = customFieldReference(settings.JobberExpenseTotalFieldID)
	}

	log.Info("Successfully loaded settings from settings.json")
}
