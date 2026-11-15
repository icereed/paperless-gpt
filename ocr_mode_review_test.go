package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOCRMyPDFManualModesRejectedBeforeQueueing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("OCR_PROVIDER", "ocrmypdf")
	oldQueue := jobQueue
	jobQueue = make(chan *Job, 2)
	t.Cleanup(func() { jobQueue = oldQueue })
	app := &App{Client: &mockPaperlessClient{}, Database: newOCRRunTestDB(t), ocrProcessMode: "whole_pdf"}
	router := gin.New()
	router.POST("/api/documents/:id/ocr", app.submitOCRJobHandler)

	for _, mode := range []string{"image", "pdf"} {
		t.Run(mode, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/documents/12/ocr", bytes.NewBufferString(`{"process_mode":"`+mode+`"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code)
			var runCount int64
			require.NoError(t, app.Database.Model(&OCRRun{}).Count(&runCount).Error)
			assert.Zero(t, runCount, "invalid mode must not persist a run")
			select {
			case job := <-jobQueue:
				t.Errorf("incompatible OCR job was queued: %+v", job)
			default:
			}
		})
	}
}

func TestOCRMyPDFIncompatibleSavedModesRejected(t *testing.T) {
	t.Chdir(t.TempDir())
	gin.SetMode(gin.TestMode)
	settingsMutex.Lock()
	originalSettings := settings
	settings = Settings{}
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = originalSettings
		settingsMutex.Unlock()
	})
	t.Setenv("OCR_PROVIDER", "ocrmypdf")
	app := &App{ocrProcessMode: "whole_pdf"}
	router := gin.New()
	router.PUT("/api/ocr/defaults", app.updateOCRDefaultsHandler)

	for _, mode := range []string{"image", "pdf"} {
		t.Run(mode, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/ocr/defaults", bytes.NewBufferString(`{"process_mode":"`+mode+`"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
	req := httptest.NewRequest(http.MethodPut, "/api/ocr/defaults", bytes.NewBufferString(`{"process_mode":"whole_pdf"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	settingsMutex.RLock()
	savedMode := settings.OCR.ProcessMode
	settingsMutex.RUnlock()
	require.NotNil(t, savedMode)
	assert.Equal(t, "whole_pdf", *savedMode)
}

func TestProviderModeCompatibilityPreservesExistingProviders(t *testing.T) {
	for _, tc := range []struct {
		provider string
		mode     string
	}{
		{provider: "llm", mode: "image"},
		{provider: "google_docai", mode: "image"},
		{provider: "ocrmypdf", mode: "whole_pdf"},
	} {
		t.Run(tc.provider+"/"+tc.mode, func(t *testing.T) {
			require.NoError(t, validateOCRProviderModeCompatibility(tc.provider, tc.mode, ""))
		})
	}
}

func TestIncompatibleSavedOCRModeRejectedWithoutMutation(t *testing.T) {
	t.Chdir(t.TempDir())
	app := &App{ocrProcessMode: "whole_pdf"}
	settingsMutex.Lock()
	original := settings
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = original
		settingsMutex.Unlock()
	})

	savedMode := "image"
	settingsMutex.Lock()
	settings.OCR.ProcessMode = &savedMode
	settingsMutex.Unlock()
	require.ErrorContains(t, validateSavedOCRProcessMode("ocrmypdf", ""), "remove the saved override")

	settingsMutex.RLock()
	retainedMode := settings.OCR.ProcessMode
	settingsMutex.RUnlock()
	require.NotNil(t, retainedMode)
	assert.Equal(t, "image", *retainedMode, "startup validation must not rewrite operator settings")
	assert.Equal(t, "image", app.effectiveOCRDefaults().ProcessMode)
}

func TestCompatibleSavedOCRModeRemainsForOtherProvider(t *testing.T) {
	t.Chdir(t.TempDir())
	settingsMutex.Lock()
	original := settings
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = original
		settingsMutex.Unlock()
	})

	savedMode := "image"
	settingsMutex.Lock()
	settings.OCR.ProcessMode = &savedMode
	settingsMutex.Unlock()
	require.NoError(t, validateSavedOCRProcessMode("google_docai", ""))

	assert.Equal(t, "image", *settings.OCR.ProcessMode)
}

func TestGeneralSettingsRejectsIncompatibleModeWithoutMutation(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("OCR_PROVIDER", "ocrmypdf")
	settingsMutex.Lock()
	previous := settings
	settings = Settings{}
	validMode := "whole_pdf"
	settings.OCR.ProcessMode = &validMode
	err := saveSettingsLocked()
	settingsMutex.Unlock()
	require.NoError(t, err)
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = previous
		settingsMutex.Unlock()
	})
	before, err := os.ReadFile("config/settings.json")
	require.NoError(t, err)
	router := gin.New()
	router.POST("/api/settings", (&App{}).updateSettingsHandler)
	for _, mode := range []string{"image", "pdf"} {
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBufferString(`{"ocr":{"process_mode":"`+mode+`"}}`))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		assert.Equal(t, http.StatusBadRequest, response.Code)
		settingsMutex.RLock()
		actual := *settings.OCR.ProcessMode
		settingsMutex.RUnlock()
		assert.Equal(t, validMode, actual)
		after, err := os.ReadFile("config/settings.json")
		require.NoError(t, err)
		assert.Equal(t, before, after, "rejected mode must not alter saved settings")
	}
}

func TestUnsetProviderDefaultsToLLMModeRules(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("OCR_PROVIDER", "")
	settingsMutex.Lock()
	previousSettings := settings
	settings = Settings{}
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = previousSettings
		settingsMutex.Unlock()
	})
	previousVision := visionLlmProvider
	visionLlmProvider = "openai"
	t.Cleanup(func() { visionLlmProvider = previousVision })
	router := gin.New()
	router.PUT("/api/ocr/defaults", (&App{ocrProcessMode: "image"}).updateOCRDefaultsHandler)
	req := httptest.NewRequest(http.MethodPut, "/api/ocr/defaults", bytes.NewBufferString(`{"process_mode":"whole_pdf"}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	assert.Equal(t, http.StatusBadRequest, response.Code, "unset provider follows startup's llm default")
}
