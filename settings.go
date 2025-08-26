package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

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
