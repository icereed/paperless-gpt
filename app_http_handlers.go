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
	"strconv"
	"strings"
	"text/template"
	"time"

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

	documents, err := app.Client.GetDocumentsByTags(ctx, []string{manualTag}, 25)
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

func (app *App) submitOCRJobHandler(c *gin.Context) {
	documentIDStr := c.Param("id")
	documentID, err := strconv.Atoi(documentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}

	// Create a new job
	jobID := generateJobID() // Implement a function to generate unique job IDs
	job := &Job{
		ID:         jobID,
		DocumentID: documentID,
		Status:     "pending",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Add job to store and queue
	jobStore.addJob(job)
	jobQueue <- job

	// Return the job ID to the client
	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID})
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

	dbResults, err := GetOcrPageResults(app.Database, parsedID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch OCR page results"})
		return
	}

	type OCRPageResult struct {
		Text           string                 `json:"text"`
		OcrLimitHit    bool                   `json:"ocrLimitHit"`
		GenerationInfo map[string]interface{} `json:"generationInfo,omitempty"`
	}

	var pages []OCRPageResult
	for _, res := range dbResults {
		var genInfo map[string]interface{}
		if res.GenerationInfo != "" {
			_ = json.Unmarshal([]byte(res.GenerationInfo), &genInfo)
		}
		pages = append(pages, OCRPageResult{
			Text:           res.Text,
			OcrLimitHit:    res.OcrLimitHit,
			GenerationInfo: genInfo,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"pages": pages,
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
	saveErr := SaveSingleOcrPageResult(app.Database, parsedID, pageIdx, result.Text, result.OcrLimitHit, genInfoJSON)
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

// containsDotDot checks if a string contains ".." to prevent path traversal.
func containsDotDot(s string) bool {
	return strings.Contains(s, "..")
}
