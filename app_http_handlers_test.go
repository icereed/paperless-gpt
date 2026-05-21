package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupTestRouter creates a gin router for testing and sets up necessary directories and files.
func setupTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.Default()

	// Isolate to a temp working directory
	tmp := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// Create test directories
	require.NoError(t, os.MkdirAll("prompts", os.ModePerm))
	require.NoError(t, os.MkdirAll("default_prompts", os.ModePerm))

	// Create dummy default prompt files for loadTemplates to find
	promptFiles := []string{
		"title_prompt.tmpl",
		"tag_prompt.tmpl",
		"correspondent_prompt.tmpl",
		"document_type_prompt.tmpl",
		"created_date_prompt.tmpl",
		"custom_field_prompt.tmpl",
		"ocr_prompt.tmpl",
	}
	for _, file := range promptFiles {
		require.NoError(
			t,
			os.WriteFile(
				filepath.Join("default_prompts", file),
				[]byte("default content"),
				0644,
			),
		)
	}

	return router
}

func TestGetPromptsHandler(t *testing.T) {
	router := setupTestRouter(t)

	// Create a dummy prompt file
	promptContent := "Hello {{.Name}}"
	os.WriteFile(filepath.Join("prompts", "test_prompt.tmpl"), []byte(promptContent), 0644)

	router.GET("/api/prompts", getPromptsHandler)

	req, _ := http.NewRequest("GET", "/api/prompts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "test_prompt.tmpl")
	assert.Equal(t, promptContent, response["test_prompt.tmpl"])
}

func TestUpdatePromptsHandler(t *testing.T) {
	router := setupTestRouter(t)

	// Create a dummy prompt file to be updated
	os.WriteFile(filepath.Join("prompts", "update_prompt.tmpl"), []byte("Initial content"), 0644)
	// The setup function already creates the default prompts, so we just need the one we are updating
	os.WriteFile(filepath.Join("default_prompts", "update_prompt.tmpl"), []byte("Default content"), 0644)

	router.POST("/api/prompts", updatePromptsHandler)

	t.Run("Successful update", func(t *testing.T) {
		newContent := "Updated content with {{.Value}}"
		payload := gin.H{
			"filename": "update_prompt.tmpl",
			"content":  newContent,
		}
		jsonPayload, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", "/api/prompts", bytes.NewBuffer(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Verify file content
		fileContent, err := os.ReadFile(filepath.Join("prompts", "update_prompt.tmpl"))
		assert.NoError(t, err)
		assert.Equal(t, newContent, string(fileContent))
		info, err := os.Stat(filepath.Join("prompts", "update_prompt.tmpl"))
		assert.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	})

	t.Run("Invalid template content", func(t *testing.T) {
		invalidContent := "Invalid {{.Value"
		payload := gin.H{
			"filename": "update_prompt.tmpl",
			"content":  invalidContent,
		}
		jsonPayload, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", "/api/prompts", bytes.NewBuffer(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("New file created when it does not exist", func(t *testing.T) {
		// The handler creates the file if it doesn't exist yet (os.WriteFile with any name
		// inside the prompts dir succeeds as long as the dir exists).
		payload := gin.H{
			"filename": "non_existent_prompt.tmpl",
			"content":  "Some content",
		}
		jsonPayload, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", "/api/prompts", bytes.NewBuffer(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Path traversal attempt with ..", func(t *testing.T) {
		payload := gin.H{
			"filename": "../evil.tmpl",
			"content":  "irrelevant",
		}
		jsonPayload, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", "/api/prompts", bytes.NewBuffer(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Absolute path traversal attempt", func(t *testing.T) {
		// filepath.Join("prompts", "/etc/passwd.tmpl") resolves to /etc/passwd.tmpl
		// on Unix — this must be rejected.
		payload := gin.H{
			"filename": "/etc/passwd.tmpl",
			"content":  "irrelevant",
		}
		jsonPayload, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", "/api/prompts", bytes.NewBuffer(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Embedded slash traversal attempt", func(t *testing.T) {
		payload := gin.H{
			"filename": "subdir/evil.tmpl",
			"content":  "irrelevant",
		}
		jsonPayload, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", "/api/prompts", bytes.NewBuffer(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestMergeSettingsPatchValidation(t *testing.T) {
	current := defaultSettings()

	_, err := mergeSettingsPatch(current, map[string]interface{}{"unknown": true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown settings field")

	_, err = mergeSettingsPatch(current, map[string]interface{}{"custom_fields_write_mode": "invalid"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom_fields_write_mode")

	_, err = mergeSettingsPatch(current, map[string]interface{}{"jobber_job_id_field_id": -1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jobber_job_id_field_id")

	_, err = mergeSettingsPatch(current, map[string]interface{}{"integration_public_url": "not a url"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "integration_public_url")

	merged, err := mergeSettingsPatch(current, map[string]interface{}{
		"custom_fields_write_mode": "replace",
		"integration_public_url":   "https://paperless-gpt.example.com",
	})
	require.NoError(t, err)
	assert.Equal(t, "replace", merged.CustomFieldsWriteMode)
	assert.Equal(t, "https://paperless-gpt.example.com", merged.IntegrationPublicURL)
}

func TestUpdateDocumentsApplyJobberFalseSkipsJobberActions(t *testing.T) {
	db, err := InitializeTestDB()
	require.NoError(t, err)
	settingsMutex.Lock()
	previousSettings := settings
	settings = defaultSettings()
	settings.JobberEnabled = true
	settings.JobberExpenseEnabled = true
	settings.JobberJobIDFieldID = 10
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = previousSettings
		settingsMutex.Unlock()
	})

	client := &updateDocumentsMockClient{}
	app := &App{
		Client:       client,
		Database:     db,
		Integrations: NewIntegrationsService(db),
	}
	router := gin.New()
	router.PATCH("/api/update-documents", app.updateDocumentsHandler)

	payload := []DocumentSuggestion{{
		ID: 1,
		JobberCandidates: []JobberMatchCandidate{{
			ID:        "job-1",
			JobNumber: "1001",
		}},
		SelectedJobberMatchID: "job-1",
		ApplyJobber:           false,
		CreateJobberExpense:   true,
	}}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPatch, "/api/update-documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.False(t, client.upsertCalled)
}

func TestUpdateDocumentsApplyJobberTrueWritesFields(t *testing.T) {
	db, err := InitializeTestDB()
	require.NoError(t, err)
	settingsMutex.Lock()
	previousSettings := settings
	settings = defaultSettings()
	settings.JobberEnabled = true
	settings.JobberJobIDFieldID = 10
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = previousSettings
		settingsMutex.Unlock()
	})

	client := &updateDocumentsMockClient{}
	app := &App{
		Client:       client,
		Database:     db,
		Integrations: NewIntegrationsService(db),
	}
	router := gin.New()
	router.PATCH("/api/update-documents", app.updateDocumentsHandler)

	payload := []DocumentSuggestion{{
		ID: 1,
		JobberCandidates: []JobberMatchCandidate{{
			ID:        "job-1",
			JobNumber: "1001",
		}},
		SelectedJobberMatchID: "job-1",
		ApplyJobber:           true,
	}}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPatch, "/api/update-documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, client.upsertCalled)
}

type updateDocumentsMockClient struct {
	upsertCalled        bool
	paperlessCalled     bool
	failOnPaperlessCall bool
	documentsByTag      []Document
	document            Document
	lastDocumentID      int
	lastTag             string
	lastPageSize        int
}

func (m *updateDocumentsMockClient) GetDocumentsByTag(ctx context.Context, tag string, pageSize int) ([]Document, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	m.lastTag = tag
	m.lastPageSize = pageSize
	return m.documentsByTag, nil
}
func (m *updateDocumentsMockClient) GetDocumentCountByTag(ctx context.Context, tag string) (int, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return 0, nil
}
func (m *updateDocumentsMockClient) UpdateDocuments(ctx context.Context, documents []DocumentSuggestion, db *gorm.DB, isUndo bool, batchID ...uint) error {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return nil
}
func (m *updateDocumentsMockClient) GetDocument(ctx context.Context, documentID int) (Document, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	m.lastDocumentID = documentID
	if m.document.ID != 0 {
		return m.document, nil
	}
	return Document{ID: documentID}, nil
}
func (m *updateDocumentsMockClient) GetAllTags(ctx context.Context) (map[string]int, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return nil, nil
}
func (m *updateDocumentsMockClient) GetAllCorrespondents(ctx context.Context) (map[string]int, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return nil, nil
}
func (m *updateDocumentsMockClient) GetAllDocumentTypes(ctx context.Context) ([]DocumentType, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return nil, nil
}
func (m *updateDocumentsMockClient) GetCustomFields(ctx context.Context) ([]CustomField, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return nil, nil
}
func (m *updateDocumentsMockClient) CreateTag(ctx context.Context, tagName string) (int, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return 0, nil
}
func (m *updateDocumentsMockClient) DownloadPDF(ctx context.Context, document Document) ([]byte, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return nil, nil
}
func (m *updateDocumentsMockClient) DownloadDocumentAsImages(ctx context.Context, documentID int, pageLimit int) ([]string, int, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return nil, 0, nil
}
func (m *updateDocumentsMockClient) DownloadDocumentAsPDF(ctx context.Context, documentID int, limitPages int, split bool) ([]string, []byte, int, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return nil, nil, 0, nil
}
func (m *updateDocumentsMockClient) UploadDocument(ctx context.Context, data []byte, filename string, metadata map[string]interface{}) (string, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return "", nil
}
func (m *updateDocumentsMockClient) UpsertDocumentCustomFields(ctx context.Context, documentID int, fieldValues map[int]interface{}, db *gorm.DB) error {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	m.upsertCalled = true
	return nil
}
func (m *updateDocumentsMockClient) UpsertDocumentCustomFieldsWithBatch(ctx context.Context, documentID int, fieldValues map[int]interface{}, db *gorm.DB, batchID *uint) error {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	m.upsertCalled = true
	return nil
}

func (m *updateDocumentsMockClient) resetCalls() {
	m.paperlessCalled = false
	m.lastDocumentID = 0
	m.lastTag = ""
	m.lastPageSize = 0
}

func (m *updateDocumentsMockClient) GetTaskStatus(ctx context.Context, taskID string) (map[string]interface{}, error) {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return nil, nil
}
func (m *updateDocumentsMockClient) DeleteDocument(ctx context.Context, documentID int) error {
	m.paperlessCalled = true
	if m.failOnPaperlessCall {
		panic("unexpected paperless-ngx client call")
	}
	return nil
}

func TestGetVersionHandler(t *testing.T) {
	router := setupTestRouter(t)
	router.GET("/api/version", getVersionHandler)

	req, _ := http.NewRequest("GET", "/api/version", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	// Check that the response contains the expected fields
	assert.Contains(t, response, "version")
	assert.Contains(t, response, "commit")
	assert.Contains(t, response, "buildDate")

	// Verify the values are the default development values
	assert.Equal(t, "devVersion", response["version"])
	assert.Equal(t, "devCommit", response["commit"])
	assert.Equal(t, "devBuildDate", response["buildDate"])
}

func TestBricoproHQAPIRequiresAPIKey(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", "test-secret")
	db, err := InitializeTestDB()
	require.NoError(t, err)
	client := &updateDocumentsMockClient{failOnPaperlessCall: true}
	app := &App{Client: client, Database: db}
	router := gin.New()
	api := router.Group(bricoproHQAPIPrefix)
	api.Use(app.bricoproHQAPIKeyMiddleware())
	app.registerBricoproHQAPIRoutes(api)
	require.NoError(t, app.upsertBricoproHQAPIKey(context.Background(), mustEncryptForTest(t, "secret-key")))

	wMissing := httptest.NewRecorder()
	router.ServeHTTP(wMissing, httptest.NewRequest(http.MethodGet, bricoproHQAPIPrefix+"/health", nil))
	require.Equal(t, http.StatusUnauthorized, wMissing.Code)

	req := httptest.NewRequest(http.MethodGet, bricoproHQAPIPrefix+"/health", nil)
	req.Header.Set("X-API-Key", "secret-key")
	wOK := httptest.NewRecorder()
	router.ServeHTTP(wOK, req)
	require.Equal(t, http.StatusOK, wOK.Code)
}

func TestBricoproHQAPIStatsUsesLocalPaperlessGPTData(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", "test-secret")

	db, err := InitializeTestDB()
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, db.Create(&SuggestionJob{DocumentID: 1, Status: suggestionJobStatusPending, NextAttemptAt: now}).Error)
	require.NoError(t, db.Create(&SuggestionJob{DocumentID: 2, Status: suggestionJobStatusRunning, NextAttemptAt: now}).Error)
	require.NoError(t, db.Create(&SuggestionJob{DocumentID: 3, Status: suggestionJobStatusFailed, NextAttemptAt: now}).Error)
	require.NoError(t, db.Create(&SuggestionJob{DocumentID: 4, Status: suggestionJobStatusSucceeded, NextAttemptAt: now}).Error)

	recentSuggestion := DocumentSuggestion{
		ID:            12,
		SuggestedTags: []string{"materials", "urgent", "materials"},
		SuggestedCustomFields: []CustomFieldSuggestion{
			{ID: 9, Name: "Total", Value: "123.45"},
		},
	}
	olderSuggestion := DocumentSuggestion{
		ID:            13,
		SuggestedTags: []string{"old-tag"},
		SuggestedCustomFields: []CustomFieldSuggestion{
			{ID: 9, Name: "Total", Value: "9999.99"},
		},
	}
	highestSuggestion := DocumentSuggestion{
		ID:            14,
		SuggestedTags: []string{"urgent"},
		SuggestedCustomFields: []CustomFieldSuggestion{
			{ID: 10, Name: "Amount", Value: 500.25},
		},
	}
	recentJSON, err := json.Marshal(recentSuggestion)
	require.NoError(t, err)
	olderJSON, err := json.Marshal(olderSuggestion)
	require.NoError(t, err)
	highestJSON, err := json.Marshal(highestSuggestion)
	require.NoError(t, err)
	require.NoError(t, db.Create(&DocumentSuggestionCache{
		DocumentID:      12,
		GeneratedAt:     now.AddDate(0, 0, -5),
		SourceHash:      "recent-12",
		SuggestionsJSON: string(recentJSON),
	}).Error)
	require.NoError(t, db.Create(&DocumentSuggestionCache{
		DocumentID:      12,
		GeneratedAt:     now.AddDate(0, 0, -4),
		SourceHash:      "recent-12b",
		SuggestionsJSON: string(recentJSON),
	}).Error)
	require.NoError(t, db.Create(&DocumentSuggestionCache{
		DocumentID:      13,
		GeneratedAt:     now.AddDate(0, 0, -31),
		SourceHash:      "old-13",
		SuggestionsJSON: string(olderJSON),
	}).Error)
	require.NoError(t, db.Create(&DocumentSuggestionCache{
		DocumentID:      14,
		GeneratedAt:     now.AddDate(0, 0, -2),
		SourceHash:      "recent-14",
		SuggestionsJSON: string(highestJSON),
	}).Error)

	router := gin.New()
	client := &updateDocumentsMockClient{}
	app := &App{Client: client, Database: db}
	require.NoError(t, app.upsertBricoproHQAPIKey(context.Background(), mustEncryptForTest(t, "secret-key")))
	api := router.Group(bricoproHQAPIPrefix)
	api.Use(app.bricoproHQAPIKeyMiddleware())
	app.registerBricoproHQAPIRoutes(api)

	req := httptest.NewRequest(http.MethodGet, bricoproHQAPIPrefix+"/stats", nil)
	req.Header.Set("X-API-Key", "secret-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.False(t, client.paperlessCalled)

	var response bricoproHQStatsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, bricoproHQAPIVersion, response.APIVersion)
	assert.Equal(t, 30, response.WindowDays)
	assert.Equal(t, int64(1), response.Queue.Pending)
	assert.Equal(t, int64(1), response.Queue.Running)
	assert.Equal(t, int64(1), response.Queue.Failed)
	assert.Equal(t, int64(3), response.Queue.Total)
	assert.Equal(t, int64(2), response.ProcessedDocumentsLast30Days)
	assert.Equal(t, []bricoproHQTagStat{
		{Tag: "materials", Count: 4},
		{Tag: "urgent", Count: 3},
	}, response.MostUsedTagsLast30Days)
	require.NotNil(t, response.HighestCustomFieldAmountSuggestionLast30Days)
	assert.Equal(t, 10, response.HighestCustomFieldAmountSuggestionLast30Days.FieldID)
	assert.Equal(t, "Amount", response.HighestCustomFieldAmountSuggestionLast30Days.FieldName)
	assert.Equal(t, 500.25, response.HighestCustomFieldAmountSuggestionLast30Days.Amount)
	assert.Empty(t, client.lastTag)
	assert.Zero(t, client.lastDocumentID)
}

func TestBricoproHQAPIDoesNotExposeDocuments(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", "test-secret")
	db, err := InitializeTestDB()
	require.NoError(t, err)
	app := &App{Database: db}
	require.NoError(t, app.upsertBricoproHQAPIKey(context.Background(), mustEncryptForTest(t, "secret-key")))
	router := gin.New()
	api := router.Group(bricoproHQAPIPrefix)
	api.Use(app.bricoproHQAPIKeyMiddleware())
	app.registerBricoproHQAPIRoutes(api)

	req := httptest.NewRequest(http.MethodGet, bricoproHQAPIPrefix+"/documents", nil)
	req.Header.Set("X-API-Key", "secret-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestBricoproHQAPIKeyGenerationEnablesConnectorAPI(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", "test-secret")

	db, err := InitializeTestDB()
	require.NoError(t, err)
	app := &App{Database: db}
	router := gin.New()
	api := router.Group("/api")
	app.registerBricoproHQConnectorSettingsRoutes(api)
	connector := router.Group(bricoproHQAPIPrefix)
	connector.Use(app.bricoproHQAPIKeyMiddleware())
	app.registerBricoproHQAPIRoutes(connector)

	wGenerate := httptest.NewRecorder()
	router.ServeHTTP(wGenerate, httptest.NewRequest(http.MethodPost, "/api/bricoprohq-connector/key", nil))
	require.Equal(t, http.StatusCreated, wGenerate.Code)

	var generated bricoproHQConnectorStatusResponse
	require.NoError(t, json.Unmarshal(wGenerate.Body.Bytes(), &generated))
	require.True(t, generated.Configured)
	require.NotEmpty(t, generated.GeneratedKey)

	req := httptest.NewRequest(http.MethodGet, bricoproHQAPIPrefix+"/stats", nil)
	req.Header.Set("X-API-Key", generated.GeneratedKey)
	wStats := httptest.NewRecorder()
	router.ServeHTTP(wStats, req)
	require.Equal(t, http.StatusOK, wStats.Code)
}

func TestBricoproHQAPIKeyGenerationAndRevoke(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", "test-secret-2")
	db, err := InitializeTestDB()
	require.NoError(t, err)
	app := &App{Database: db}
	router := gin.New()
	api := router.Group("/api")
	app.registerBricoproHQConnectorSettingsRoutes(api)

	generateReq := httptest.NewRequest(http.MethodPost, "/api/bricoprohq-connector/key", nil)
	generateReq.Host = "paperless-gpt.local:8080"
	wGenerate := httptest.NewRecorder()
	router.ServeHTTP(wGenerate, generateReq)
	require.Equal(t, http.StatusCreated, wGenerate.Code)

	var generated bricoproHQConnectorStatusResponse
	require.NoError(t, json.Unmarshal(wGenerate.Body.Bytes(), &generated))
	require.True(t, generated.Configured)
	require.NotEmpty(t, generated.GeneratedKey)
	require.Equal(t, "http://paperless-gpt.local:8080", generated.BaseURL)
	require.Empty(t, generated.LocalBaseURL)

	localReq := httptest.NewRequest(http.MethodGet, "/api/bricoprohq-connector", nil)
	localReq.Host = "paperless-gpt.local:8080"
	localReq.Header.Set("Origin", "http://192.168.1.25:3000")
	wLocal := httptest.NewRecorder()
	router.ServeHTTP(wLocal, localReq)
	require.Equal(t, http.StatusOK, wLocal.Code)

	var localStatus bricoproHQConnectorStatusResponse
	require.NoError(t, json.Unmarshal(wLocal.Body.Bytes(), &localStatus))
	require.Equal(t, "http://192.168.1.25:8080", localStatus.LocalBaseURL)
	require.Equal(t, "http://192.168.1.25:8080/api/bricoprohq/v1/health", localStatus.HealthURL)

	storedKey, err := app.bricoproHQAPIKey(context.Background())
	require.NoError(t, err)
	require.Equal(t, generated.GeneratedKey, storedKey)

	wRevoke := httptest.NewRecorder()
	router.ServeHTTP(wRevoke, httptest.NewRequest(http.MethodDelete, "/api/bricoprohq-connector/key", nil))
	require.Equal(t, http.StatusOK, wRevoke.Code)

	storedKey, err = app.bricoproHQAPIKey(context.Background())
	require.NoError(t, err)
	require.Empty(t, storedKey)
}

func mustEncryptForTest(t *testing.T, value string) string {
	t.Helper()
	encrypted, err := EncryptSecret(value)
	require.NoError(t, err)
	return encrypted
}
