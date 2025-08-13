package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
)

// getPromptsHandler handles the GET /api/prompts endpoint
func getPromptsHandler(c *gin.Context) {
	templateMutex.RLock()
	defer templateMutex.RUnlock()

	// Read the templates from files or use default content
	titleTemplateContent, err := os.ReadFile("prompts/title_prompt.tmpl")
	if err != nil {
		titleTemplateContent = []byte(defaultTitleTemplate)
	}

	tagTemplateContent, err := os.ReadFile("prompts/tag_prompt.tmpl")
	if err != nil {
		tagTemplateContent = []byte(defaultTagTemplate)
	}

	c.JSON(http.StatusOK, gin.H{
		"title_template": string(titleTemplateContent),
		"tag_template":   string(tagTemplateContent),
	})
}

// updatePromptsHandler handles the POST /api/prompts endpoint
func updatePromptsHandler(c *gin.Context) {
	var req struct {
		TitleTemplate string `json:"title_template"`
		TagTemplate   string `json:"tag_template"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	templateMutex.Lock()
	defer templateMutex.Unlock()

	// Update title template
	if req.TitleTemplate != "" {
		t, err := template.New("title").Parse(req.TitleTemplate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid title template: %v", err)})
			return
		}
		titleTemplate = t
		err = os.WriteFile("prompts/title_prompt.tmpl", []byte(req.TitleTemplate), 0644)
		if err != nil {
			log.Errorf("Failed to write title_prompt.tmpl: %v", err)
		}
	}

	// Update tag template
	if req.TagTemplate != "" {
		t, err := template.New("tag").Parse(req.TagTemplate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid tag template: %v", err)})
			return
		}
		tagTemplate = t
		err = os.WriteFile("prompts/tag_prompt.tmpl", []byte(req.TagTemplate), 0644)
		if err != nil {
			log.Errorf("Failed to write tag_prompt.tmpl: %v", err)
		}
	}

	c.Status(http.StatusOK)
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
