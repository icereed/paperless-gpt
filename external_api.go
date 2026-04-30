package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const externalAPIVersion = "v1"

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
