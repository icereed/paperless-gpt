package main

import (
	"encoding/json"
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

	documents, err := app.Client.GetDocumentsByTags(ctx, []string{manualTag})
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

	results, err := app.generateDocumentSuggestions(ctx, suggestionRequest)
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

// Section for local-db actions

func (app *App) getModificationHistoryHandler(c *gin.Context) {
	modifications, err := GetAllModifications(app.Database)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve modification history"})
		log.Errorf("Failed to retrieve modification history: %v", err)
		return
	}
	c.JSON(http.StatusOK, modifications)
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
