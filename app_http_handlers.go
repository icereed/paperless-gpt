package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"paperless-gpt/sanitize"

	"github.com/Masterminds/sprig/v3"
	"github.com/gin-gonic/gin"
)

// getPromptsHandler handles the GET /api/prompts endpoint
func getPromptsHandler(c *gin.Context) {
	promptsDir := "prompts"
	files, err := os.ReadDir(promptsDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not read prompts directory"})
		log.Errorf("Could not read prompts directory: %v", err)
		return
	}

	prompts := make(map[string]string)
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".tmpl") {
			fullPath := filepath.Join(promptsDir, file.Name())
			content, err := os.ReadFile(fullPath)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Could not read prompt file: %s", file.Name())})
				log.Errorf("Could not read prompt file %s: %v", file.Name(), err)
				return
			}
			prompts[file.Name()] = string(content)
		}
	}
	c.JSON(http.StatusOK, prompts)
}

// updatePromptsHandler handles the POST /api/prompts endpoint
func updatePromptsHandler(c *gin.Context) {
	var req struct {
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	// Basic input validation
	if req.Filename == "" || !strings.HasSuffix(req.Filename, ".tmpl") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename or missing .tmpl extension"})
		return
	}
	if containsDotDot(req.Filename) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename: path traversal is not allowed"})
		return
	}

	promptPath := filepath.Join("prompts", req.Filename)

	// Validate template content
	_, err := template.New(req.Filename).Option("missingkey=error").Funcs(sprig.FuncMap()).Parse(req.Content)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid template content: %v", err)})
		return
	}

	// Write the updated prompt file
	err = os.WriteFile(promptPath, []byte(req.Content), 0644)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write prompt file"})
		log.Errorf("Failed to write prompt file %s: %v", req.Filename, err)
		return
	}

	// Reload templates to apply changes immediately
	if err := loadTemplates(); err != nil {
		log.Errorf("Failed to reload templates after update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Prompt saved but failed to reload templates"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Prompt saved successfully"})
}

// getAllTagsHandler handles the GET /api/tags endpoint
func (app *App) getAllTagsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	tags, err := app.Client.GetAllTags(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching tags: %v", err)})
		log.Errorf("Error fetching tags: %v", err)
		return
	}

	c.JSON(http.StatusOK, tags)
}

// getSettingsHandler handles the GET /api/settings endpoint
func (app *App) getSettingsHandler(c *gin.Context) {
	// Refresh the cache when settings are requested
	go refreshCustomFieldsCache(app.Client)

	settingsMutex.RLock()
	defer settingsMutex.RUnlock()
	customFieldsCacheMu.RLock()
	defer customFieldsCacheMu.RUnlock()

	// Create a response that includes both settings and custom fields
	response := gin.H{
		"settings":      settings,
		"custom_fields": customFieldsCache,
	}
	c.JSON(http.StatusOK, response)
}

// updateSettingsHandler handles the POST /api/settings endpoint
func (app *App) updateSettingsHandler(c *gin.Context) {
	settingsMutex.Lock()
	defer settingsMutex.Unlock()

	var newSettings Settings
	if err := c.ShouldBindJSON(&newSettings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	// Update the global settings variable
	settings = newSettings

	// Save the updated settings to file
	if err := saveSettingsLocked(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save settings"})
		log.Errorf("Failed to save settings: %v", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings saved successfully"})
}

// getCustomFieldsHandler handles the GET /api/custom_fields endpoint
func (app *App) getCustomFieldsHandler(c *gin.Context) {
	// Check for "force_pull" query parameter
	if forcePull := c.Query("force_pull"); forcePull == "true" {
		// Force a refresh of the custom fields cache
		go refreshCustomFieldsCache(app.Client)
	}

	customFieldsCacheMu.RLock()
	defer customFieldsCacheMu.RUnlock()

	c.JSON(http.StatusOK, customFieldsCache)
}

// documentsHandler handles the GET /api/documents endpoint
func (app *App) documentsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	documents, err := app.Client.GetDocumentsByTag(ctx, manualTag, 25)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching documents: %v", err)})
		log.Errorf("Error fetching documents: %v", err)
		return
	}

	c.JSON(http.StatusOK, documents)
}

// generateSuggestionsHandler handles the POST /api/generate-suggestions endpoint
func (app *App) generateSuggestionsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	var suggestionRequest GenerateSuggestionsRequest
	if err := c.ShouldBindJSON(&suggestionRequest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request payload: %v", err)})
		log.Errorf("Invalid request payload: %v", err)
		return
	}

	results, err := app.generateDocumentSuggestions(ctx, suggestionRequest, log.WithContext(ctx))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error processing documents: %v", err)})
		log.Errorf("Error processing documents: %v", err)
		return
	}

	c.JSON(http.StatusOK, results)
}

func (app *App) submitSuggestionJobHandler(c *gin.Context) {
	var suggestionRequest GenerateSuggestionsRequest
	if err := c.ShouldBindJSON(&suggestionRequest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request payload: %v", err)})
		log.Errorf("Invalid suggestion job request payload: %v", err)
		return
	}

	if len(suggestionRequest.Documents) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "At least one document is required"})
		return
	}

	jobID := generateJobID()
	job := &SuggestionJob{
		ID:             jobID,
		Status:         "pending",
		Request:        suggestionRequest,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		TotalDocuments: len(suggestionRequest.Documents),
	}

	suggestionJobStore.addJob(job)
	select {
	case suggestionJobQueue <- job:
		c.JSON(http.StatusAccepted, gin.H{"job_id": jobID})
	default:
		suggestionJobStore.updateStatus(jobID, "failed", "Suggestion queue is full")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Suggestion queue is full, please retry"})
	}
}

func (app *App) getSuggestionJobStatusHandler(c *gin.Context) {
	jobID := c.Param("job_id")

	job, exists := suggestionJobStore.getJob(jobID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	c.JSON(http.StatusOK, suggestionJobResponse(job))
}

func (app *App) getAllSuggestionJobsHandler(c *gin.Context) {
	jobs := suggestionJobStore.getAllJobs()

	jobList := make([]gin.H, 0, len(jobs))
	for _, job := range jobs {
		jobList = append(jobList, suggestionJobResponse(job))
	}

	c.JSON(http.StatusOK, jobList)
}

func (app *App) stopSuggestionJobHandler(c *gin.Context) {
	jobID := c.Param("job_id")
	if suggestionJobStore.cancelPending(jobID) {
		c.Status(http.StatusNoContent)
		return
	}

	suggestionJobCancellersMu.Lock()
	cancel, exists := suggestionJobCancellers[jobID]
	suggestionJobCancellersMu.Unlock()
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No running job with this ID"})
		return
	}
	cancel()
	c.Status(http.StatusNoContent)
}

func suggestionJobResponse(job SuggestionJob) gin.H {
	response := gin.H{
		"job_id":              job.ID,
		"status":              job.Status,
		"created_at":          job.CreatedAt,
		"updated_at":          job.UpdatedAt,
		"documents_done":      job.DocumentsDone,
		"total_documents":     job.TotalDocuments,
		"current_document_id": job.CurrentDocumentID,
	}

	if job.Status == "completed" {
		response["result"] = job.Result
	} else if job.Status == "failed" || job.Status == "cancelled" {
		response["error"] = job.Error
	}
	if len(job.FailedDocuments) > 0 {
		response["failed_documents"] = job.FailedDocuments
	}

	return response
}

// updateDocumentsHandler handles the PATCH /api/update-documents endpoint
func (app *App) updateDocumentsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var documents []DocumentSuggestion
	if err := c.ShouldBindJSON(&documents); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request payload: %v", err)})
		log.Errorf("Invalid request payload: %v", err)
		return
	}

	err := app.Client.UpdateDocuments(ctx, documents, app.Database, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error updating documents: %v", err)})
		log.Errorf("Error updating documents: %v", err)
		return
	}

	c.Status(http.StatusOK)
}

// submitOCRJobRequest is the optional body of POST /api/documents/:id/ocr.
// Absent fields fall back to the effective defaults (settings over env).
type submitOCRJobRequest struct {
	LimitPages      *int    `json:"limit_pages"`
	ProcessMode     *string `json:"process_mode"`
	UploadPDF       *bool   `json:"upload_pdf"`
	ReplaceOriginal *bool   `json:"replace_original"`
	CopyMetadata    *bool   `json:"copy_metadata"`
	PromptOverride  string  `json:"prompt_override"`
}

// ocrSupportsHOCR reports whether the configured provider can produce hOCR
// (the prerequisite for searchable PDFs).
func (app *App) ocrSupportsHOCR() bool {
	if app.ocrProvider == nil {
		return false
	}
	capable, ok := app.ocrProvider.(HOCRCapable)
	return ok && capable.IsHOCREnabled()
}

func (app *App) submitOCRJobHandler(c *gin.Context) {
	documentIDStr := c.Param("id")
	documentID, err := strconv.Atoi(documentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}

	var req submitOCRJobRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request payload: %v", err)})
			return
		}
	}

	options := app.effectiveOCRDefaults()
	if req.LimitPages != nil {
		if *req.LimitPages < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit_pages must be 0 (no limit) or positive"})
			return
		}
		options.LimitPages = *req.LimitPages
	}
	if req.ProcessMode != nil && *req.ProcessMode != "" {
		mode := *req.ProcessMode
		if mode != "image" && mode != "pdf" && mode != "whole_pdf" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "process_mode must be one of: image, pdf, whole_pdf"})
			return
		}
		options.ProcessMode = mode
	}
	if req.UploadPDF != nil {
		options.UploadPDF = *req.UploadPDF
	}
	if req.ReplaceOriginal != nil {
		options.ReplaceOriginal = *req.ReplaceOriginal
	}
	if req.CopyMetadata != nil {
		options.CopyMetadata = *req.CopyMetadata
	}
	if options.ReplaceOriginal && !options.UploadPDF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "replace_original requires upload_pdf"})
		return
	}
	if options.UploadPDF && !app.ocrSupportsHOCR() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Searchable PDFs need an hOCR-capable OCR provider (enable hOCR for the LLM provider)"})
		return
	}
	if req.PromptOverride != "" {
		if _, err := renderOCRPromptOverride(req.PromptOverride, ""); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Prompt override does not render: %v", err)})
			return
		}
		options.PromptOverride = req.PromptOverride
	}

	// Fetch title and existing content: the title makes the persisted run
	// self-describing, the content feeds the OCR prompt's cross-reference.
	documentTitle := fmt.Sprintf("Document %d", documentID)
	if doc, err := app.Client.GetDocument(c.Request.Context(), documentID); err == nil {
		documentTitle = doc.Title
		options.ExistingContent = doc.Content
	} else {
		log.Warnf("Could not fetch document %d before OCR run: %v", documentID, err)
	}

	// Create a new job
	jobID := generateJobID()
	job := &Job{
		ID:         jobID,
		DocumentID: documentID,
		Status:     "pending",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Options:    options,
	}

	run := &OCRRun{
		JobID:            jobID,
		DocumentID:       documentID,
		DocumentTitle:    documentTitle,
		Trigger:          "manual",
		LimitPages:       options.LimitPages,
		ProcessMode:      options.ProcessMode,
		UploadPDF:        options.UploadPDF,
		ReplaceOriginal:  options.ReplaceOriginal,
		CopyMetadata:     options.CopyMetadata,
		PromptOverridden: options.PromptOverride != "",
		PromptOverride:   options.PromptOverride,
		Provider:         app.ocrProviderLabel,
		PDFAction:        "none",
	}
	if err := CreateOCRRun(app.Database, run); err != nil {
		log.Warnf("Failed to persist OCR run for job %s: %v", jobID, err)
	}

	// Add job to store and queue
	jobStore.addJob(job)
	jobQueue <- job

	// Return the job ID to the client
	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID})
}

// listOCRRunsHandler returns persisted OCR runs (the Activity log), newest
// first, optionally filtered by document.
func (app *App) listOCRRunsHandler(c *gin.Context) {
	limit := 50
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "50")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && o >= 0 {
		offset = o
	}
	documentID := 0
	if d, err := strconv.Atoi(c.DefaultQuery("document_id", "0")); err == nil && d > 0 {
		documentID = d
	}

	runs, total, err := ListOCRRuns(app.Database, documentID, limit, offset)
	if err != nil {
		log.Errorf("Failed to list OCR runs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list OCR runs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs, "total": total})
}

// getOCRConfigHandler describes the OCR setup: provider, capabilities,
// effective defaults, and the auto-OCR queue. It powers the Playground's
// transparency panel, the setup state, and the Activity header.
func (app *App) getOCRConfigHandler(c *gin.Context) {
	enabled := app.isOcrEnabled()
	defaults := app.effectiveOCRDefaults()

	response := gin.H{
		"enabled":      enabled,
		"provider":     app.ocrProviderLabel,
		"hocr_capable": app.ocrSupportsHOCR(),
		"defaults": gin.H{
			"limit_pages":      defaults.LimitPages,
			"process_mode":     defaults.ProcessMode,
			"upload_pdf":       defaults.UploadPDF,
			"replace_original": defaults.ReplaceOriginal,
			"copy_metadata":    defaults.CopyMetadata,
		},
		"auto_tag":         autoOcrTag,
		"ocr_complete_tag": pdfOCRCompleteTag,
		"ocr_tagging":      pdfOCRTagging,
	}

	if enabled && autoOcrTag != "" {
		if count, err := app.Client.GetDocumentCountByTag(c.Request.Context(), autoOcrTag); err == nil {
			response["auto_queue_count"] = count
		}
	}

	c.JSON(http.StatusOK, response)
}

// updateOCRDefaultsHandler persists new OCR Run Option defaults — the
// "make this the auto default" ramp from Playground to hands-off auto mode.
func (app *App) updateOCRDefaultsHandler(c *gin.Context) {
	var defaults OCRDefaults
	if err := c.ShouldBindJSON(&defaults); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request payload: %v", err)})
		return
	}
	if defaults.LimitPages != nil && *defaults.LimitPages < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit_pages must be 0 (no limit) or positive"})
		return
	}
	if defaults.ProcessMode != nil && *defaults.ProcessMode != "" {
		mode := *defaults.ProcessMode
		if mode != "image" && mode != "pdf" && mode != "whole_pdf" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "process_mode must be one of: image, pdf, whole_pdf"})
			return
		}
	}
	if defaults.ReplaceOriginal != nil && *defaults.ReplaceOriginal {
		uploadEnabled := app.pdfUpload
		if defaults.UploadPDF != nil {
			uploadEnabled = *defaults.UploadPDF
		}
		if !uploadEnabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "replace_original requires upload_pdf"})
			return
		}
	}

	if err := updateOCRDefaults(defaults); err != nil {
		log.Errorf("Failed to save OCR defaults: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save OCR defaults"})
		return
	}

	effective := app.effectiveOCRDefaults()
	c.JSON(http.StatusOK, gin.H{
		"defaults": gin.H{
			"limit_pages":      effective.LimitPages,
			"process_mode":     effective.ProcessMode,
			"upload_pdf":       effective.UploadPDF,
			"replace_original": effective.ReplaceOriginal,
			"copy_metadata":    effective.CopyMetadata,
		},
	})
}

// documentIDFromQuery recognizes bare document IDs and paperless-ngx
// document URLs so the picker's search field doubles as a paste target.
var documentURLPattern = regexp.MustCompile(`/documents/(\d+)`)

func documentIDFromQuery(query string) int {
	trimmed := strings.TrimSpace(query)
	if id, err := strconv.Atoi(trimmed); err == nil && id > 0 {
		return id
	}
	if m := documentURLPattern.FindStringSubmatch(trimmed); m != nil {
		if id, err := strconv.Atoi(m[1]); err == nil && id > 0 {
			return id
		}
	}
	return 0
}

// searchDocumentsHandler powers the Playground document picker: recent
// documents for an empty query, full-text search otherwise, and direct hits
// for pasted IDs or paperless-ngx URLs.
func (app *App) searchDocumentsHandler(c *gin.Context) {
	query := c.Query("q")
	limit := 20
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && l > 0 && l <= 50 {
		limit = l
	}

	if id := documentIDFromQuery(query); id > 0 {
		if doc, err := app.Client.GetDocument(c.Request.Context(), id); err == nil {
			c.JSON(http.StatusOK, gin.H{"documents": []Document{doc}})
			return
		}
		// Fall through to full-text search: the number might be part of a title.
	}

	documents, err := app.Client.SearchDocuments(c.Request.Context(), query, limit)
	if err != nil {
		log.Errorf("Error searching documents: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to search documents"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"documents": documents})
}

// getDocumentPageImageHandler serves a rendered page of a document for the
// Playground's scan-next-to-text view.
func (app *App) getDocumentPageImageHandler(c *gin.Context) {
	documentID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}
	pageIndex, err := strconv.Atoi(c.Param("pageIndex"))
	if err != nil || pageIndex < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page index"})
		return
	}

	data, err := app.Client.GetDocumentPageImage(c.Request.Context(), documentID, pageIndex)
	if err != nil {
		if strings.Contains(err.Error(), "out of range") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		log.Errorf("Error rendering page image for document %d page %d: %v", documentID, pageIndex, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to render page image"})
		return
	}
	c.Header("Cache-Control", "private, max-age=3600")
	c.Data(http.StatusOK, "image/jpeg", data)
}

func (app *App) getJobStatusHandler(c *gin.Context) {
	jobID := c.Param("job_id")

	job, exists := jobStore.getJob(jobID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	response := gin.H{
		"job_id":      job.ID,
		"status":      job.Status,
		"created_at":  job.CreatedAt,
		"updated_at":  job.UpdatedAt,
		"pages_done":  job.PagesDone,
		"total_pages": job.TotalPages,
	}

	if job.Status == "completed" {
		response["result"] = job.Result
	} else if job.Status == "failed" {
		response["error"] = job.Result
	}

	c.JSON(http.StatusOK, response)
}

func (app *App) getAllJobsHandler(c *gin.Context) {
	jobs := jobStore.GetAllJobs()

	jobList := make([]gin.H, 0, len(jobs))
	for _, job := range jobs {
		response := gin.H{
			"job_id":     job.ID,
			"status":     job.Status,
			"created_at": job.CreatedAt,
			"updated_at": job.UpdatedAt,
			"pages_done": job.PagesDone,
		}

		if job.Status == "completed" {
			response["result"] = job.Result
		} else if job.Status == "failed" {
			response["error"] = job.Result
		}

		jobList = append(jobList, response)
	}

	c.JSON(http.StatusOK, jobList)
}

// POST /api/ocr/jobs/:job_id/stop
func (app *App) stopOCRJobHandler(c *gin.Context) {
	jobID := c.Param("job_id")
	jobCancellersMu.Lock()
	cancel, exists := jobCancellers[jobID]
	jobCancellersMu.Unlock()
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No running job with this ID"})
		return
	}
	cancel()
	c.Status(http.StatusNoContent)
}

// getDocumentThumbnailHandler proxies the paperless-ngx thumbnail for a document so
// the frontend can show scan previews without direct access to paperless-ngx.
func (app *App) getDocumentThumbnailHandler(c *gin.Context) {
	parsedID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}
	data, contentType, err := app.Client.GetDocumentThumbnail(c.Request.Context(), parsedID)
	if err != nil {
		log.Errorf("Error fetching thumbnail for document %d: %v", parsedID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch thumbnail"})
		return
	}
	c.Header("Cache-Control", "private, max-age=3600")
	c.Data(http.StatusOK, contentType, data)
}

// getDocumentHandler handles the retrieval of a document by its ID
func (app *App) getDocumentHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		parsedID, err := strconv.Atoi(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
			return
		}
		document, err := app.Client.GetDocument(c, parsedID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			log.Errorf("Error fetching document: %v", err)
			return
		}
		c.JSON(http.StatusOK, document)
	}
}

// getOCRPagesHandler returns per-page OCR results for a document
func (app *App) getOCRPagesHandler(c *gin.Context) {
	id := c.Param("id")
	parsedID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}

	// job_id selects a specific run's pages; empty means the latest run.
	requestedJobID := c.Query("job_id")
	dbResults, err := GetOcrPageResults(app.Database, parsedID, requestedJobID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch OCR page results"})
		return
	}

	type OCRPageResult struct {
		PageIndex      int                    `json:"pageIndex"`
		Text           string                 `json:"text"`
		OcrLimitHit    bool                   `json:"ocrLimitHit"`
		GenerationInfo map[string]interface{} `json:"generationInfo,omitempty"`
	}

	pages := []OCRPageResult{}
	jobID := requestedJobID
	for _, res := range dbResults {
		var genInfo map[string]interface{}
		if res.GenerationInfo != "" {
			_ = json.Unmarshal([]byte(res.GenerationInfo), &genInfo)
		}
		if jobID == "" {
			jobID = res.JobID
		}
		pages = append(pages, OCRPageResult{
			PageIndex:      res.PageIndex,
			Text:           res.Text,
			OcrLimitHit:    res.OcrLimitHit,
			GenerationInfo: genInfo,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"pages":  pages,
		"job_id": jobID,
	})
}

func (app *App) reOCRPageHandler(c *gin.Context) {
	id := c.Param("id")
	pageIdxStr := c.Param("pageIndex")
	parsedID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}
	pageIdx, err := strconv.Atoi(pageIdxStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page index"})
		return
	}

	// Download all images for the document, but only process the requested page
	imagePaths, _, err := app.Client.DownloadDocumentAsImages(c.Request.Context(), parsedID, limitOcrPages)
	if err != nil || pageIdx < 0 || pageIdx >= len(imagePaths) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page index or failed to download images"})
		return
	}
	imageContent, err := os.ReadFile(imagePaths[pageIdx])
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read image file"})
		return
	}

	cancelKey := fmt.Sprintf("%d-%d", parsedID, pageIdx)
	reOcrCtx, cancelReOcr := context.WithCancel(c.Request.Context())
	defer cancelReOcr()

	reOcrCancellersMu.Lock()
	if existingCancel, ok := reOcrCancellers[cancelKey]; ok {
		existingCancel()
	}
	reOcrCancellers[cancelKey] = cancelReOcr
	reOcrCancellersMu.Unlock()

	defer func() {
		reOcrCancellersMu.Lock()
		delete(reOcrCancellers, cancelKey)
		reOcrCancellersMu.Unlock()
	}()

	result, err := app.ocrProvider.ProcessImage(reOcrCtx, imageContent, pageIdx+1)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Infof("Re-OCR for doc %d page %d cancelled.", parsedID, pageIdx)
			c.Status(499)
		} else {
			log.Errorf("Failed to re-OCR doc %d page %d: %v", parsedID, pageIdx, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to re-OCR page"})
		}
		return
	}
	if result == nil {
		log.Errorf("Re-OCR for doc %d page %d returned nil result.", parsedID, pageIdx)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Re-OCR returned no result"})
		return
	}

	var genInfoJSON string
	if result.GenerationInfo != nil {
		if b, err := json.Marshal(result.GenerationInfo); err == nil {
			genInfoJSON = string(b)
		}
	}
	// Attribute the corrected page to a run: an explicit job_id wins,
	// otherwise the latest run of the document.
	jobID := c.Query("job_id")
	if jobID == "" {
		jobID = LatestOCRRunJobID(app.Database, parsedID)
	}
	saveErr := SaveSingleOcrPageResult(app.Database, parsedID, jobID, pageIdx, result.Text, result.OcrLimitHit, genInfoJSON)
	if saveErr != nil {
		log.Errorf("Failed to save re-OCR result for doc %d page %d: %v", parsedID, pageIdx, saveErr)
	}

	c.JSON(http.StatusOK, gin.H{
		"text":           result.Text,
		"ocrLimitHit":    result.OcrLimitHit,
		"generationInfo": result.GenerationInfo,
	})
}

// cancelReOCRPageHandler handles the DELETE request to cancel an ongoing re-OCR for a specific page.
func (app *App) cancelReOCRPageHandler(c *gin.Context) {
	id := c.Param("id")
	pageIdxStr := c.Param("pageIndex")
	parsedID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}
	pageIdx, err := strconv.Atoi(pageIdxStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page index"})
		return
	}

	cancelKey := fmt.Sprintf("%d-%d", parsedID, pageIdx)

	reOcrCancellersMu.Lock()
	cancel, exists := reOcrCancellers[cancelKey]
	if exists {
		delete(reOcrCancellers, cancelKey)
	}
	reOcrCancellersMu.Unlock()

	if exists {
		cancel()
		log.Infof("Cancellation requested for re-OCR doc %d page %d", parsedID, pageIdx)
		c.Status(http.StatusNoContent)
	} else {
		log.Warnf("No active re-OCR found to cancel for doc %d page %d", parsedID, pageIdx)
		c.JSON(http.StatusNotFound, gin.H{"error": "No active re-OCR operation found for this page"})
	}
}

// Section for local-db actions

func (app *App) getModificationHistoryHandler(c *gin.Context) {
	// Parse pagination parameters
	page := 1
	pageSize := 20

	if p, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && p > 0 {
		page = p
	}
	if ps, err := strconv.Atoi(c.DefaultQuery("pageSize", "20")); err == nil && ps > 0 && ps <= 100 {
		pageSize = ps
	}

	// Get paginated modifications and total count
	modifications, total, err := GetPaginatedModifications(app.Database, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve modification history"})
		log.Errorf("Failed to retrieve modification history: %v", err)
		return
	}

	totalPages := (int(total) + pageSize - 1) / pageSize

	c.JSON(http.StatusOK, gin.H{
		"items":       modifications,
		"totalItems":  total,
		"totalPages":  totalPages,
		"currentPage": page,
		"pageSize":    pageSize,
	})
}

func (app *App) undoModificationHandler(c *gin.Context) {
	id := c.Param("id")
	modID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid modification ID"})
		log.Errorf("Invalid modification ID: %v", err)
		return
	}

	modification, err := GetModification(app.Database, uint(modID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve modification"})
		log.Errorf("Failed to retrieve modification: %v", err)
		return
	}

	if modification.Undone {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Modification has already been undone"})
		log.Errorf("Modification has already been undone: %v", id)
		return
	}

	// Ok, we're actually doing the update:
	ctx := c.Request.Context()

	// Make the document suggestions for UpdateDocuments
	var suggestion DocumentSuggestion
	suggestion.ID = int(modification.DocumentID)
	suggestion.OriginalDocument, err = app.Client.GetDocument(ctx, int(modification.DocumentID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve original document"})
		log.Errorf("Failed to retrieve original document: %v", err)
		return
	}
	switch modification.ModField {
	case "title":
		suggestion.SuggestedTitle = modification.PreviousValue
	case "tags":
		var tags []string
		err := json.Unmarshal([]byte(modification.PreviousValue), &tags)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to unmarshal previous tags"})
			log.Errorf("Failed to unmarshal previous tags: %v", err)
			return
		}
		suggestion.SuggestedTags = tags
	case "content":
		suggestion.SuggestedContent = modification.PreviousValue
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid modification field"})
		log.Errorf("Invalid modification field: %v", modification.ModField)
		return
	}

	// Update the document
	err = app.Client.UpdateDocuments(ctx, []DocumentSuggestion{suggestion}, app.Database, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update document"})
		log.Errorf("Failed to update document: %v", err)
		return
	}

	// Successful, so set modification as undone
	err = SetModificationUndone(app.Database, modification)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to mark modification as undone"})
		return
	}

	// Else all was ok
	c.Status(http.StatusOK)
}

// analyzeDocumentsHandler handles the POST /api/analyze-documents endpoint
func (app *App) analyzeDocumentsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	var req AnalyzeDocumentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request payload: %v", err)})
		log.Errorf("Invalid request payload: %v", err)
		return
	}

	var documents []Document
	for _, docID := range req.DocumentIDs {
		doc, err := app.Client.GetDocument(ctx, docID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching document %d: %v", docID, err)})
			log.Errorf("Error fetching document %d: %v", docID, err)
			return
		}
		doc.Content = sanitize.Sanitize(doc.Content)
		documents = append(documents, doc)
	}

	// Create a new template from the prompt string in the request
	tmpl, err := template.New("adhoc-analysis").Funcs(sprig.FuncMap()).Parse(req.Prompt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid prompt template"})
		log.Errorf("Invalid prompt template: %v", err)
		return
	}

	var promptBuffer bytes.Buffer
	err = tmpl.Execute(&promptBuffer, map[string]interface{}{
		"Documents": documents,
		"Language":  getLikelyLanguage(),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error executing adhoc analysis template"})
		log.Errorf("Error executing adhoc analysis template: %v", err)
		return
	}

	finalPrompt := promptBuffer.String()

	// Call LLM with the custom prompt and document contexts
	llmResponse, err := app.LLM.Call(ctx, finalPrompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error calling LLM"})
		log.Errorf("Error calling LLM: %v", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": llmResponse})
}

// getVersionHandler handles the GET /api/version endpoint
func getVersionHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version":   version,
		"commit":    commit,
		"buildDate": buildDate,
	})
}

// containsDotDot checks if a string contains ".." to prevent path traversal.
func containsDotDot(s string) bool {
	return strings.Contains(s, "..")
}
