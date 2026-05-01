package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const externalAPIVersion = "v1"
const externalAPIKeyName = "default"

type externalAPIKeyStatusResponse struct {
	Configured      bool   `json:"configured"`
	Source          string `json:"source,omitempty"`
	BaseURL         string `json:"base_url"`
	LocalBaseURL    string `json:"local_base_url,omitempty"`
	OpenAPIURL      string `json:"openapi_url"`
	LocalOpenAPIURL string `json:"local_openapi_url,omitempty"`
	HeaderName      string `json:"header_name"`
	GeneratedKey    string `json:"api_key,omitempty"`
}

type externalDocumentListResponse struct {
	Count     int        `json:"count"`
	PageSize  int        `json:"page_size"`
	Documents []Document `json:"documents"`
}

type externalSummaryResponse struct {
	Service          string                        `json:"service"`
	Version          string                        `json:"version"`
	APIVersion       string                        `json:"api_version"`
	PendingDocuments externalPendingSummary        `json:"pending_documents"`
	OCR              externalOCRSummary            `json:"ocr"`
	Integrations     []IntegrationConnectionStatus `json:"integrations,omitempty"`
}

type externalPendingSummary struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type externalOCRSummary struct {
	Enabled bool                        `json:"enabled"`
	Jobs    map[string]externalJobCount `json:"jobs"`
}

type externalJobCount struct {
	Count int `json:"count"`
}

func (app *App) registerExternalAPIRoutes(external *gin.RouterGroup) {
	external.GET("/health", app.externalHealthHandler)
	external.GET("/summary", app.externalSummaryHandler)
	external.GET("/documents/pending", app.externalPendingDocumentsHandler)
	external.GET("/documents/:id", app.externalDocumentHandler)
	external.GET("/ocr/jobs", app.externalOCRJobsHandler)
	external.GET("/openapi.json", externalOpenAPIHandler)
}

func (app *App) externalAPIMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected, err := app.externalAPIKey(c.Request.Context())
		if err != nil {
			log.WithError(err).Warn("failed to load external API key")
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to load external API key"})
			return
		}
		if expected == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "External API is disabled. Generate an API key in Settings or set PAPERLESS_GPT_API_KEY on the Paperless GPT server.",
			})
			return
		}

		provided := strings.TrimSpace(c.GetHeader("X-API-Key"))
		if provided == "" {
			authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
			if strings.HasPrefix(authHeader, "Bearer ") {
				provided = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			}
		}
		if provided == "" || !constantTimeStringEqual(provided, expected) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or missing API key"})
			return
		}
		go app.recordExternalAPIKeyUse(context.Background())
		c.Next()
	}
}

func (app *App) registerExternalAPIKeySettingsRoutes(api *gin.RouterGroup) {
	api.GET("/external-api-key", app.getExternalAPIKeySettingsHandler)
	api.POST("/external-api-key/generate", app.generateExternalAPIKeyHandler)
	api.DELETE("/external-api-key", app.revokeExternalAPIKeyHandler)
}

func (app *App) getExternalAPIKeySettingsHandler(c *gin.Context) {
	status, err := app.externalAPIKeyStatus(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load external API key status"})
		return
	}
	c.JSON(http.StatusOK, status)
}

func (app *App) generateExternalAPIKeyHandler(c *gin.Context) {
	if strings.TrimSpace(envExternalAPIKey()) != "" {
		c.JSON(http.StatusConflict, gin.H{"error": "PAPERLESS_GPT_API_KEY is configured in the environment. Rotate it in your server/container configuration."})
		return
	}
	key, err := generateExternalAPIKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate API key"})
		return
	}
	encrypted, err := EncryptSecret(key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encrypt API key"})
		return
	}
	if err := app.upsertExternalAPIKey(c.Request.Context(), encrypted); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save API key"})
		return
	}
	status, err := app.externalAPIKeyStatus(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "API key saved but status could not be loaded"})
		return
	}
	status.GeneratedKey = key
	c.JSON(http.StatusCreated, status)
}

func (app *App) revokeExternalAPIKeyHandler(c *gin.Context) {
	if strings.TrimSpace(envExternalAPIKey()) != "" {
		c.JSON(http.StatusConflict, gin.H{"error": "PAPERLESS_GPT_API_KEY is configured in the environment. Remove it from your server/container configuration to disable the external API."})
		return
	}
	if app.Database == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database is not configured"})
		return
	}
	if err := app.Database.WithContext(c.Request.Context()).Where("name = ?", externalAPIKeyName).Delete(&ExternalAPIKey{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to revoke API key"})
		return
	}
	status, err := app.externalAPIKeyStatus(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "API key revoked but status could not be loaded"})
		return
	}
	c.JSON(http.StatusOK, status)
}

func generateExternalAPIKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "pgpt_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func envExternalAPIKey() string {
	if key := strings.TrimSpace(os.Getenv("PAPERLESS_GPT_API_KEY")); key != "" {
		return key
	}
	return strings.TrimSpace(os.Getenv("EXTERNAL_API_KEY"))
}

func constantTimeStringEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (app *App) externalAPIKey(ctx context.Context) (string, error) {
	if key := envExternalAPIKey(); key != "" {
		return key, nil
	}
	if app == nil || app.Database == nil || !app.Database.Migrator().HasTable(&ExternalAPIKey{}) {
		return "", nil
	}
	var record ExternalAPIKey
	err := app.Database.WithContext(ctx).First(&record, "name = ?", externalAPIKeyName).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return DecryptSecret(record.EncryptedKey)
}

func (app *App) upsertExternalAPIKey(ctx context.Context, encryptedKey string) error {
	if app == nil || app.Database == nil {
		return errors.New("database is not configured")
	}
	var record ExternalAPIKey
	err := app.Database.WithContext(ctx).First(&record, "name = ?", externalAPIKeyName).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		record = ExternalAPIKey{Name: externalAPIKeyName}
	} else if err != nil {
		return err
	}
	record.EncryptedKey = encryptedKey
	return app.Database.WithContext(ctx).Save(&record).Error
}

func (app *App) recordExternalAPIKeyUse(ctx context.Context) {
	if app == nil || app.Database == nil || envExternalAPIKey() != "" {
		return
	}
	now := time.Now().UTC()
	if err := app.Database.WithContext(ctx).Model(&ExternalAPIKey{}).Where("name = ?", externalAPIKeyName).Update("last_used_at", &now).Error; err != nil {
		log.WithError(err).Debug("failed to record external API key usage")
	}
}

func (app *App) externalAPIKeyStatus(c *gin.Context) (externalAPIKeyStatusResponse, error) {
	baseURL := strings.TrimRight(currentBaseURL(c), "/") + "/api/external/v1"
	localBaseURL := localExternalAPIBaseURL(c)
	if localBaseURL == "" {
		localBaseURL = localExternalAPIBaseURLFromOrigin(c)
	}
	status := externalAPIKeyStatusResponse{
		BaseURL:         baseURL,
		LocalBaseURL:    localBaseURL,
		OpenAPIURL:      baseURL + "/openapi.json",
		LocalOpenAPIURL: localBaseURL + "/openapi.json",
		HeaderName:      "X-API-Key",
	}
	if envExternalAPIKey() != "" {
		status.Configured = true
		status.Source = "environment"
		return status, nil
	}
	if app == nil || app.Database == nil {
		return status, nil
	}
	var count int64
	if err := app.Database.WithContext(c.Request.Context()).Model(&ExternalAPIKey{}).Where("name = ?", externalAPIKeyName).Count(&count).Error; err != nil {
		return status, err
	}
	if count > 0 {
		status.Configured = true
		status.Source = "settings"
	}
	return status, nil
}

func (app *App) externalHealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"service":     "paperless-gpt",
		"version":     version,
		"api_version": externalAPIVersion,
	})
}

func (app *App) externalSummaryHandler(c *gin.Context) {
	ctx := c.Request.Context()
	pendingCount, err := app.Client.GetDocumentCountByTag(ctx, manualTag)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Error fetching pending document count: %v", err)})
		return
	}

	statuses := []IntegrationConnectionStatus{}
	if app.Integrations != nil {
		statuses = append(statuses,
			app.Integrations.Status("jobber"),
			app.Integrations.Status("google_drive"),
			app.Integrations.FireflyStatus(ctx),
		)
	}

	c.JSON(http.StatusOK, externalSummaryResponse{
		Service:    "paperless-gpt",
		Version:    version,
		APIVersion: externalAPIVersion,
		PendingDocuments: externalPendingSummary{
			Tag:   manualTag,
			Count: pendingCount,
		},
		OCR: externalOCRSummary{
			Enabled: app.isOcrEnabled(),
			Jobs:    externalOCRJobCounts(),
		},
		Integrations: statuses,
	})
}

func (app *App) externalPendingDocumentsHandler(c *gin.Context) {
	pageSize := parsePositiveIntQuery(c, "page_size", 25, 100)
	documents, err := app.Client.GetDocumentsByTag(c.Request.Context(), manualTag, pageSize)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Error fetching pending documents: %v", err)})
		return
	}
	c.JSON(http.StatusOK, externalDocumentListResponse{
		Count:     len(documents),
		PageSize:  pageSize,
		Documents: documents,
	})
}

func (app *App) externalDocumentHandler(c *gin.Context) {
	documentID, err := strconv.Atoi(c.Param("id"))
	if err != nil || documentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}
	document, err := app.Client.GetDocument(c.Request.Context(), documentID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Error fetching document: %v", err)})
		return
	}
	c.JSON(http.StatusOK, document)
}

func (app *App) externalOCRJobsHandler(c *gin.Context) {
	jobs := jobStore.GetAllJobs()
	statusFilter := strings.TrimSpace(c.Query("status"))
	documentFilter := 0
	if rawDocumentID := strings.TrimSpace(c.Query("document_id")); rawDocumentID != "" {
		parsed, err := strconv.Atoi(rawDocumentID)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document_id"})
			return
		}
		documentFilter = parsed
	}

	jobList := make([]gin.H, 0, len(jobs))
	for _, job := range jobs {
		if statusFilter != "" && job.Status != statusFilter {
			continue
		}
		if documentFilter > 0 && job.DocumentID != documentFilter {
			continue
		}
		jobList = append(jobList, externalOCRJobResponse(job))
	}

	c.JSON(http.StatusOK, gin.H{
		"count": len(jobList),
		"jobs":  jobList,
	})
}

func parsePositiveIntQuery(c *gin.Context, name string, defaultValue, maxValue int) int {
	value := defaultValue
	if raw := strings.TrimSpace(c.Query(name)); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			value = parsed
		}
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func externalOCRJobCounts() map[string]externalJobCount {
	counts := map[string]externalJobCount{}
	for _, job := range jobStore.GetAllJobs() {
		count := counts[job.Status]
		count.Count++
		counts[job.Status] = count
	}
	return counts
}

func externalOCRJobResponse(job *Job) gin.H {
	response := gin.H{
		"job_id":      job.ID,
		"document_id": job.DocumentID,
		"status":      job.Status,
		"created_at":  job.CreatedAt,
		"updated_at":  job.UpdatedAt,
		"pages_done":  job.PagesDone,
		"total_pages": job.TotalPages,
	}
	if job.Status == "failed" || job.Status == "cancelled" {
		response["error"] = job.Result
	}
	return response
}

func externalOpenAPIHandler(c *gin.Context) {
	spec := map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "Paperless GPT External API",
			"version":     externalAPIVersion,
			"description": "Read-only API for local self-hosted apps to inspect Paperless GPT state.",
		},
		"servers": []map[string]string{
			{"url": "/api/external/v1"},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"ApiKeyAuth": map[string]string{
					"type": "apiKey",
					"in":   "header",
					"name": "X-API-Key",
				},
				"BearerAuth": map[string]string{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "API key",
				},
			},
		},
		"security": []map[string][]string{
			{"ApiKeyAuth": {}},
			{"BearerAuth": {}},
		},
		"paths": map[string]interface{}{
			"/health": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Check API availability",
					"responses": map[string]interface{}{
						"200": map[string]string{"description": "API is available"},
					},
				},
			},
			"/summary": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Get pending document, OCR, and integration summary",
					"responses": map[string]interface{}{
						"200": map[string]string{"description": "Summary returned"},
					},
				},
			},
			"/documents/pending": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "List documents tagged for Paperless GPT review",
					"parameters": []map[string]interface{}{
						{
							"name":        "page_size",
							"in":          "query",
							"required":    false,
							"description": "Maximum documents to return, capped at 100",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 25,
								"maximum": 100,
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]string{"description": "Pending documents returned"},
					},
				},
			},
			"/documents/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Get one document by ID",
					"parameters": []map[string]interface{}{
						{
							"name":     "id",
							"in":       "path",
							"required": true,
							"schema": map[string]string{
								"type": "integer",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]string{"description": "Document returned"},
					},
				},
			},
			"/ocr/jobs": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "List in-memory OCR jobs",
					"parameters": []map[string]interface{}{
						{
							"name":     "status",
							"in":       "query",
							"required": false,
							"schema":   map[string]string{"type": "string"},
						},
						{
							"name":     "document_id",
							"in":       "query",
							"required": false,
							"schema":   map[string]string{"type": "integer"},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]string{"description": "OCR jobs returned"},
					},
				},
			},
			"/openapi.json": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "Get this OpenAPI document",
					"responses": map[string]interface{}{
						"200": map[string]string{"description": "OpenAPI document returned"},
					},
				},
			},
		},
		"x-generated-at": time.Now().UTC().Format(time.RFC3339),
	}

	c.Header("Content-Type", "application/json")
	encoder := json.NewEncoder(c.Writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(spec); err != nil {
		log.WithError(err).Warn("failed to write external OpenAPI response")
	}
}
