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
	upsertCalled   bool
	documentsByTag []Document
	lastTag        string
	lastPageSize   int
}

func (m *updateDocumentsMockClient) GetDocumentsByTag(ctx context.Context, tag string, pageSize int) ([]Document, error) {
	m.lastTag = tag
	m.lastPageSize = pageSize
	return m.documentsByTag, nil
}
func (m *updateDocumentsMockClient) GetDocumentCountByTag(ctx context.Context, tag string) (int, error) {
	return 0, nil
}
func (m *updateDocumentsMockClient) UpdateDocuments(ctx context.Context, documents []DocumentSuggestion, db *gorm.DB, isUndo bool, batchID ...uint) error {
	return nil
}
func (m *updateDocumentsMockClient) GetDocument(ctx context.Context, documentID int) (Document, error) {
	return Document{}, nil
}
func (m *updateDocumentsMockClient) GetAllTags(ctx context.Context) (map[string]int, error) {
	return nil, nil
}
func (m *updateDocumentsMockClient) GetAllCorrespondents(ctx context.Context) (map[string]int, error) {
	return nil, nil
}
func (m *updateDocumentsMockClient) GetAllDocumentTypes(ctx context.Context) ([]DocumentType, error) {
	return nil, nil
}
func (m *updateDocumentsMockClient) GetCustomFields(ctx context.Context) ([]CustomField, error) {
	return nil, nil
}
func (m *updateDocumentsMockClient) CreateTag(ctx context.Context, tagName string) (int, error) {
	return 0, nil
}
func (m *updateDocumentsMockClient) DownloadPDF(ctx context.Context, document Document) ([]byte, error) {
	return nil, nil
}
func (m *updateDocumentsMockClient) DownloadDocumentAsImages(ctx context.Context, documentID int, pageLimit int) ([]string, int, error) {
	return nil, 0, nil
}
func (m *updateDocumentsMockClient) DownloadDocumentAsPDF(ctx context.Context, documentID int, limitPages int, split bool) ([]string, []byte, int, error) {
	return nil, nil, 0, nil
}
func (m *updateDocumentsMockClient) UploadDocument(ctx context.Context, data []byte, filename string, metadata map[string]interface{}) (string, error) {
	return "", nil
}
func (m *updateDocumentsMockClient) UpsertDocumentCustomFields(ctx context.Context, documentID int, fieldValues map[int]interface{}, db *gorm.DB) error {
	m.upsertCalled = true
	return nil
}
func (m *updateDocumentsMockClient) UpsertDocumentCustomFieldsWithBatch(ctx context.Context, documentID int, fieldValues map[int]interface{}, db *gorm.DB, batchID *uint) error {
	m.upsertCalled = true
	return nil
}
func (m *updateDocumentsMockClient) GetTaskStatus(ctx context.Context, taskID string) (map[string]interface{}, error) {
	return nil, nil
}
func (m *updateDocumentsMockClient) DeleteDocument(ctx context.Context, documentID int) error {
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

func TestExternalAPIRequiresAPIKey(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_API_KEY", "secret-key")
	router := gin.New()
	app := &App{Client: &updateDocumentsMockClient{}}
	api := router.Group("/api/external/v1")
	api.Use(app.externalAPIMiddleware())
	app.registerExternalAPIRoutes(api)

	wMissing := httptest.NewRecorder()
	router.ServeHTTP(wMissing, httptest.NewRequest(http.MethodGet, "/api/external/v1/health", nil))
	require.Equal(t, http.StatusUnauthorized, wMissing.Code)

	req := httptest.NewRequest(http.MethodGet, "/api/external/v1/health", nil)
	req.Header.Set("X-API-Key", "secret-key")
	wOK := httptest.NewRecorder()
	router.ServeHTTP(wOK, req)
	require.Equal(t, http.StatusOK, wOK.Code)
}

func TestExternalAPIPendingDocuments(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_API_KEY", "secret-key")
	previousManualTag := manualTag
	manualTag = "review-me"
	t.Cleanup(func() { manualTag = previousManualTag })

	client := &updateDocumentsMockClient{
		documentsByTag: []Document{
			{ID: 12, Title: "Invoice"},
		},
	}
	router := gin.New()
	app := &App{Client: client}
	api := router.Group("/api/external/v1")
	api.Use(app.externalAPIMiddleware())
	app.registerExternalAPIRoutes(api)

	req := httptest.NewRequest(http.MethodGet, "/api/external/v1/documents/pending?page_size=250", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "review-me", client.lastTag)
	assert.Equal(t, 100, client.lastPageSize)

	var response externalDocumentListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, 1, response.Count)
	assert.Equal(t, 100, response.PageSize)
	require.Len(t, response.Documents, 1)
	assert.Equal(t, 12, response.Documents[0].ID)
}

func TestExternalAPIKeyGenerationEnablesExternalAPI(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_API_KEY", "")
	t.Setenv("EXTERNAL_API_KEY", "")
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", "test-secret")

	db, err := InitializeTestDB()
	require.NoError(t, err)
	client := &updateDocumentsMockClient{
		documentsByTag: []Document{{ID: 44, Title: "Receipt"}},
	}
	app := &App{Client: client, Database: db}
	router := gin.New()
	api := router.Group("/api")
	app.registerExternalAPIKeySettingsRoutes(api)
	external := router.Group("/api/external/v1")
	external.Use(app.externalAPIMiddleware())
	app.registerExternalAPIRoutes(external)

	wGenerate := httptest.NewRecorder()
	router.ServeHTTP(wGenerate, httptest.NewRequest(http.MethodPost, "/api/external-api-key/generate", nil))
	require.Equal(t, http.StatusCreated, wGenerate.Code)

	var generated externalAPIKeyStatusResponse
	require.NoError(t, json.Unmarshal(wGenerate.Body.Bytes(), &generated))
	require.True(t, generated.Configured)
	require.Equal(t, "settings", generated.Source)
	require.NotEmpty(t, generated.GeneratedKey)

	req := httptest.NewRequest(http.MethodGet, "/api/external/v1/documents/pending", nil)
	req.Header.Set("X-API-Key", generated.GeneratedKey)
	wPending := httptest.NewRecorder()
	router.ServeHTTP(wPending, req)
	require.Equal(t, http.StatusOK, wPending.Code)
}

func TestExternalAPIKeyGenerationAndRevoke(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_API_KEY", "")
	t.Setenv("EXTERNAL_API_KEY", "")
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", "test-secret-2")
	db, err := InitializeTestDB()
	require.NoError(t, err)
	app := &App{Database: db}
	router := gin.New()
	api := router.Group("/api")
	app.registerExternalAPIKeySettingsRoutes(api)

	generateReq := httptest.NewRequest(http.MethodPost, "/api/external-api-key/generate", nil)
	generateReq.Host = "paperless-gpt.local:8080"
	wGenerate := httptest.NewRecorder()
	router.ServeHTTP(wGenerate, generateReq)
	require.Equal(t, http.StatusCreated, wGenerate.Code)

	var generated externalAPIKeyStatusResponse
	require.NoError(t, json.Unmarshal(wGenerate.Body.Bytes(), &generated))
	require.True(t, generated.Configured)
	require.Equal(t, "settings", generated.Source)
	require.NotEmpty(t, generated.GeneratedKey)
	require.Equal(t, "http://paperless-gpt.local:8080/api/external/v1", generated.BaseURL)
	require.Empty(t, generated.LocalBaseURL)

	localReq := httptest.NewRequest(http.MethodGet, "/api/external-api-key", nil)
	localReq.Host = "paperless-gpt.local:8080"
	localReq.Header.Set("Origin", "http://192.168.1.25:3000")
	wLocal := httptest.NewRecorder()
	router.ServeHTTP(wLocal, localReq)
	require.Equal(t, http.StatusOK, wLocal.Code)

	var localStatus externalAPIKeyStatusResponse
	require.NoError(t, json.Unmarshal(wLocal.Body.Bytes(), &localStatus))
	require.Equal(t, "http://192.168.1.25:8080/api/external/v1", localStatus.LocalBaseURL)
	require.Equal(t, "http://192.168.1.25:8080/api/external/v1/openapi.json", localStatus.LocalOpenAPIURL)

	storedKey, err := app.externalAPIKey(context.Background())
	require.NoError(t, err)
	require.Equal(t, generated.GeneratedKey, storedKey)

	wRevoke := httptest.NewRecorder()
	router.ServeHTTP(wRevoke, httptest.NewRequest(http.MethodDelete, "/api/external-api-key", nil))
	require.Equal(t, http.StatusOK, wRevoke.Code)

	storedKey, err = app.externalAPIKey(context.Background())
	require.NoError(t, err)
	require.Empty(t, storedKey)
}
