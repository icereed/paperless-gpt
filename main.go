package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/gin-gonic/gin"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

// Global Variables and Constants
var (
	paperlessBaseURL  = os.Getenv("PAPERLESS_BASE_URL")
	paperlessAPIToken = os.Getenv("PAPERLESS_API_TOKEN")
	openaiAPIKey      = os.Getenv("OPENAI_API_KEY")
	manualTag         = "paperless-gpt"
	autoTag           = "paperless-gpt-auto"
	llmProvider       = os.Getenv("LLM_PROVIDER")
	llmModel          = os.Getenv("LLM_MODEL")

	// Templates
	titleTemplate *template.Template
	tagTemplate   *template.Template
	templateMutex sync.RWMutex

	// Default templates
	defaultTitleTemplate = `I will provide you with the content of a document that has been partially read by OCR (so it may contain errors).
Your task is to find a suitable document title that I can use as the title in the paperless-ngx program.
Respond only with the title, without any additional information. The content is likely in {{.Language}}.

Content:
{{.Content}}
`

	defaultTagTemplate = `I will provide you with the content and the title of a document. Your task is to select appropriate tags for the document from the list of available tags I will provide. Only select tags from the provided list. Respond only with the selected tags as a comma-separated list, without any additional information. The content is likely in {{.Language}}.

Available Tags:
{{.AvailableTags | join ", "}}

Title:
{{.Title}}

Content:
{{.Content}}

Please concisely select the {{.Language}} tags from the list above that best describe the document.
Be very selective and only choose the most relevant tags since too many tags will make the document less discoverable.
`
)

// App struct to hold dependencies
type App struct {
	Client *PaperlessClient
	LLM    llms.Model
}

func main() {
	// Validate Environment Variables
	validateEnvVars()

	// Initialize PaperlessClient
	client := NewPaperlessClient(paperlessBaseURL, paperlessAPIToken)

	// Load Templates
	loadTemplates()

	// Initialize LLM
	llm, err := createLLM()
	if err != nil {
		log.Fatalf("Failed to create LLM client: %v", err)
	}

	// Initialize App with dependencies
	app := &App{
		Client: client,
		LLM:    llm,
	}

	// Start background process for auto-tagging
	go func() {

		minBackoffDuration := time.Second
		maxBackoffDuration := time.Hour
		pollingInterval := 10 * time.Second

		backoffDuration := minBackoffDuration
		for {
			if err := app.processAutoTagDocuments(); err != nil {
				log.Printf("Error in processAutoTagDocuments: %v", err)
				time.Sleep(backoffDuration)
				backoffDuration *= 2 // Exponential backoff
				if backoffDuration > maxBackoffDuration {
					log.Printf("Repeated errors in processAutoTagDocuments detected. Setting backoff to %v", maxBackoffDuration)
					backoffDuration = maxBackoffDuration
				}
			} else {
				backoffDuration = minBackoffDuration
			}
			time.Sleep(pollingInterval)
		}
	}()

	// Create a Gin router with default middleware (logger and recovery)
	router := gin.Default()

	// API routes
	api := router.Group("/api")
	{
		api.GET("/documents", app.documentsHandler)
		api.POST("/generate-suggestions", app.generateSuggestionsHandler)
		api.PATCH("/update-documents", app.updateDocumentsHandler)
		api.GET("/filter-tag", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"tag": manualTag})
		})
		// Get all tags
		api.GET("/tags", app.getAllTagsHandler)
		api.GET("/prompts", getPromptsHandler)
		api.POST("/prompts", updatePromptsHandler)
	}

	// Serve static files for the frontend under /assets
	router.StaticFS("/assets", gin.Dir("./web-app/dist/assets", true))
	router.StaticFile("/vite.svg", "./web-app/dist/vite.svg")

	// Catch-all route for serving the frontend
	router.NoRoute(func(c *gin.Context) {
		c.File("./web-app/dist/index.html")
	})

	log.Println("Server started on port :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

// validateEnvVars ensures all necessary environment variables are set
func validateEnvVars() {
	if paperlessBaseURL == "" {
		log.Fatal("Please set the PAPERLESS_BASE_URL environment variable.")
	}

	if paperlessAPIToken == "" {
		log.Fatal("Please set the PAPERLESS_API_TOKEN environment variable.")
	}

	if llmProvider == "" {
		log.Fatal("Please set the LLM_PROVIDER environment variable.")
	}

	if llmModel == "" {
		log.Fatal("Please set the LLM_MODEL environment variable.")
	}

	if llmProvider == "openai" && openaiAPIKey == "" {
		log.Fatal("Please set the OPENAI_API_KEY environment variable for OpenAI provider.")
	}
}

// processAutoTagDocuments handles the background auto-tagging of documents
func (app *App) processAutoTagDocuments() error {
	ctx := context.Background()

	documents, err := app.Client.GetDocumentsByTags(ctx, []string{autoTag})
	if err != nil {
		return fmt.Errorf("error fetching documents with autoTag: %w", err)
	}

	if len(documents) == 0 {
		return nil // No documents to process
	}

	suggestionRequest := GenerateSuggestionsRequest{
		Documents:      documents,
		GenerateTitles: true,
		GenerateTags:   true,
	}

	suggestions, err := app.generateDocumentSuggestions(ctx, suggestionRequest)
	if err != nil {
		return fmt.Errorf("error generating suggestions: %w", err)
	}

	err = app.Client.UpdateDocuments(ctx, suggestions)
	if err != nil {
		return fmt.Errorf("error updating documents: %w", err)
	}

	return nil
}

// getAllTagsHandler handles the GET /api/tags endpoint
func (app *App) getAllTagsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	tags, err := app.Client.GetAllTags(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching tags: %v", err)})
		log.Printf("Error fetching tags: %v", err)
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
		log.Printf("Error fetching documents: %v", err)
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
		log.Printf("Invalid request payload: %v", err)
		return
	}

	results, err := app.generateDocumentSuggestions(ctx, suggestionRequest)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error processing documents: %v", err)})
		log.Printf("Error processing documents: %v", err)
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
		log.Printf("Invalid request payload: %v", err)
		return
	}

	err := app.Client.UpdateDocuments(ctx, documents)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error updating documents: %v", err)})
		log.Printf("Error updating documents: %v", err)
		return
	}

	c.Status(http.StatusOK)
}

// generateDocumentSuggestions generates suggestions for a set of documents
func (app *App) generateDocumentSuggestions(ctx context.Context, suggestionRequest GenerateSuggestionsRequest) ([]DocumentSuggestion, error) {
	// Fetch all available tags from paperless-ngx
	availableTagsMap, err := app.Client.GetAllTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch available tags: %v", err)
	}

	// Prepare a list of tag names
	availableTagNames := make([]string, 0, len(availableTagsMap))
	for tagName := range availableTagsMap {
		if tagName == manualTag {
			continue
		}
		availableTagNames = append(availableTagNames, tagName)
	}

	documents := suggestionRequest.Documents
	documentSuggestions := []DocumentSuggestion{}

	var wg sync.WaitGroup
	var mu sync.Mutex
	errorsList := make([]error, 0)

	for i := range documents {
		wg.Add(1)
		go func(doc Document) {
			defer wg.Done()
			documentID := doc.ID
			log.Printf("Processing Document ID %d...", documentID)

			content := doc.Content
			if len(content) > 5000 {
				content = content[:5000]
			}

			var suggestedTitle string
			var suggestedTags []string

			if suggestionRequest.GenerateTitles {
				suggestedTitle, err = app.getSuggestedTitle(ctx, content)
				if err != nil {
					mu.Lock()
					errorsList = append(errorsList, fmt.Errorf("Document %d: %v", documentID, err))
					mu.Unlock()
					log.Printf("Error processing document %d: %v", documentID, err)
					return
				}
			}

			if suggestionRequest.GenerateTags {
				suggestedTags, err = app.getSuggestedTags(ctx, content, suggestedTitle, availableTagNames)
				if err != nil {
					mu.Lock()
					errorsList = append(errorsList, fmt.Errorf("Document %d: %v", documentID, err))
					mu.Unlock()
					log.Printf("Error generating tags for document %d: %v", documentID, err)
					return
				}
			}

			mu.Lock()
			suggestion := DocumentSuggestion{
				ID:               documentID,
				OriginalDocument: doc,
			}
			// Titles
			if suggestionRequest.GenerateTitles {
				suggestion.SuggestedTitle = suggestedTitle
			} else {
				suggestion.SuggestedTitle = doc.Title
			}

			// Tags
			if suggestionRequest.GenerateTags {
				suggestion.SuggestedTags = suggestedTags
			} else {
				suggestion.SuggestedTags = removeTagFromList(doc.Tags, manualTag)
			}
			documentSuggestions = append(documentSuggestions, suggestion)
			mu.Unlock()
			log.Printf("Document %d processed successfully.", documentID)
		}(documents[i])
	}

	wg.Wait()

	if len(errorsList) > 0 {
		return nil, errorsList[0] // Return the first error encountered
	}

	return documentSuggestions, nil
}

// removeTagFromList removes a specific tag from a list of tags
func removeTagFromList(tags []string, tagToRemove string) []string {
	filteredTags := []string{}
	for _, tag := range tags {
		if tag != tagToRemove {
			filteredTags = append(filteredTags, tag)
		}
	}
	return filteredTags
}

// getSuggestedTags generates suggested tags for a document using the LLM
func (app *App) getSuggestedTags(ctx context.Context, content string, suggestedTitle string, availableTags []string) ([]string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	defer templateMutex.RUnlock()

	var promptBuffer bytes.Buffer
	err := tagTemplate.Execute(&promptBuffer, map[string]interface{}{
		"Language":      likelyLanguage,
		"AvailableTags": availableTags,
		"Title":         suggestedTitle,
		"Content":       content,
	})
	if err != nil {
		return nil, fmt.Errorf("error executing tag template: %v", err)
	}

	prompt := promptBuffer.String()
	log.Printf("Tag suggestion prompt: %s", prompt)

	completion, err := app.LLM.GenerateContent(ctx, []llms.MessageContent{
		{
			Parts: []llms.ContentPart{
				llms.TextContent{
					Text: prompt,
				},
			},
			Role: llms.ChatMessageTypeHuman,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error getting response from LLM: %v", err)
	}

	response := strings.TrimSpace(completion.Choices[0].Content)
	suggestedTags := strings.Split(response, ",")
	for i, tag := range suggestedTags {
		suggestedTags[i] = strings.TrimSpace(tag)
	}

	// Filter out tags that are not in the available tags list
	filteredTags := []string{}
	for _, tag := range suggestedTags {
		for _, availableTag := range availableTags {
			if strings.EqualFold(tag, availableTag) {
				filteredTags = append(filteredTags, availableTag)
				break
			}
		}
	}

	return filteredTags, nil
}

// getLikelyLanguage determines the likely language of the document content
func getLikelyLanguage() string {
	likelyLanguage := os.Getenv("LLM_LANGUAGE")
	if likelyLanguage == "" {
		likelyLanguage = "English"
	}
	return strings.Title(strings.ToLower(likelyLanguage))
}

// getSuggestedTitle generates a suggested title for a document using the LLM
func (app *App) getSuggestedTitle(ctx context.Context, content string) (string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	defer templateMutex.RUnlock()

	var promptBuffer bytes.Buffer
	err := titleTemplate.Execute(&promptBuffer, map[string]interface{}{
		"Language": likelyLanguage,
		"Content":  content,
	})
	if err != nil {
		return "", fmt.Errorf("error executing title template: %v", err)
	}

	prompt := promptBuffer.String()

	log.Printf("Title suggestion prompt: %s", prompt)

	completion, err := app.LLM.GenerateContent(ctx, []llms.MessageContent{
		{
			Parts: []llms.ContentPart{
				llms.TextContent{
					Text: prompt,
				},
			},
			Role: llms.ChatMessageTypeHuman,
		},
	})
	if err != nil {
		return "", fmt.Errorf("error getting response from LLM: %v", err)
	}

	return strings.TrimSpace(strings.Trim(completion.Choices[0].Content, "\"")), nil
}

// loadTemplates loads the title and tag templates from files or uses default templates
func loadTemplates() {
	templateMutex.Lock()
	defer templateMutex.Unlock()

	// Ensure prompts directory exists
	promptsDir := "prompts"
	if err := os.MkdirAll(promptsDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create prompts directory: %v", err)
	}

	// Load title template
	titleTemplatePath := filepath.Join(promptsDir, "title_prompt.tmpl")
	titleTemplateContent, err := os.ReadFile(titleTemplatePath)
	if err != nil {
		log.Printf("Could not read %s, using default template: %v", titleTemplatePath, err)
		titleTemplateContent = []byte(defaultTitleTemplate)
		if err := os.WriteFile(titleTemplatePath, titleTemplateContent, os.ModePerm); err != nil {
			log.Fatalf("Failed to write default title template to disk: %v", err)
		}
	}
	titleTemplate, err = template.New("title").Funcs(sprig.FuncMap()).Parse(string(titleTemplateContent))
	if err != nil {
		log.Fatalf("Failed to parse title template: %v", err)
	}

	// Load tag template
	tagTemplatePath := filepath.Join(promptsDir, "tag_prompt.tmpl")
	tagTemplateContent, err := os.ReadFile(tagTemplatePath)
	if err != nil {
		log.Printf("Could not read %s, using default template: %v", tagTemplatePath, err)
		tagTemplateContent = []byte(defaultTagTemplate)
		if err := os.WriteFile(tagTemplatePath, tagTemplateContent, os.ModePerm); err != nil {
			log.Fatalf("Failed to write default tag template to disk: %v", err)
		}
	}
	tagTemplate, err = template.New("tag").Funcs(sprig.FuncMap()).Parse(string(tagTemplateContent))
	if err != nil {
		log.Fatalf("Failed to parse tag template: %v", err)
	}
}

// createLLM creates the appropriate LLM client based on the provider
func createLLM() (llms.Model, error) {
	switch strings.ToLower(llmProvider) {
	case "openai":
		if openaiAPIKey == "" {
			return nil, fmt.Errorf("OpenAI API key is not set")
		}
		return openai.New(
			openai.WithModel(llmModel),
			openai.WithToken(openaiAPIKey),
		)
	case "ollama":
		host := os.Getenv("OLLAMA_HOST")
		if host == "" {
			host = "http://127.0.0.1:11434"
		}
		return ollama.New(
			ollama.WithModel(llmModel),
			ollama.WithServerURL(host),
		)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", llmProvider)
	}
}

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
			log.Printf("Failed to write title_prompt.tmpl: %v", err)
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
			log.Printf("Failed to write tag_prompt.tmpl: %v", err)
		}
	}

	c.Status(http.StatusOK)
}
