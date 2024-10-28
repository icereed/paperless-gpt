package main

import (
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
	visionLlmProvider = os.Getenv("VISION_LLM_PROVIDER")
	visionLlmModel    = os.Getenv("VISION_LLM_MODEL")

	// Templates
	titleTemplate *template.Template
	tagTemplate   *template.Template
	ocrTemplate   *template.Template
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

	defaultOcrPrompt = `Just transcribe the text in this image and preserve the formatting and layout (high quality OCR). Do that for ALL the text in the image. Be thorough and pay attention. This is very important. The image is from a text document so be sure to continue until the bottom of the page. Thanks a lot! You tend to forget about some text in the image so please focus! Use markdown format.`
)

// App struct to hold dependencies
type App struct {
	Client    *PaperlessClient
	LLM       llms.Model
	VisionLLM llms.Model
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

	// Initialize Vision LLM
	visionLlm, err := createVisionLLM()
	if err != nil {
		log.Fatalf("Failed to create Vision LLM client: %v", err)
	}

	// Initialize App with dependencies
	app := &App{
		Client:    client,
		LLM:       llm,
		VisionLLM: visionLlm,
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

		// OCR endpoints
		api.POST("/documents/:id/ocr", app.submitOCRJobHandler)
		api.GET("/jobs/ocr/:job_id", app.getJobStatusHandler)
		api.GET("/jobs/ocr", app.getAllJobsHandler)

		// Endpoint to see if user enabled OCR
		api.GET("/experimental/ocr", func(c *gin.Context) {
			enabled := isOcrEnabled()
			c.JSON(http.StatusOK, gin.H{"enabled": enabled})
		})
	}

	// Serve static files for the frontend under /assets
	router.StaticFS("/assets", gin.Dir("./web-app/dist/assets", true))
	router.StaticFile("/vite.svg", "./web-app/dist/vite.svg")

	// Catch-all route for serving the frontend
	router.NoRoute(func(c *gin.Context) {
		c.File("./web-app/dist/index.html")
	})

	// Start OCR worker pool
	numWorkers := 1 // Number of workers to start
	startWorkerPool(app, numWorkers)

	log.Println("Server started on port :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func isOcrEnabled() bool {
	return visionLlmModel != "" && visionLlmProvider != ""
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

// getLikelyLanguage determines the likely language of the document content
func getLikelyLanguage() string {
	likelyLanguage := os.Getenv("LLM_LANGUAGE")
	if likelyLanguage == "" {
		likelyLanguage = "English"
	}
	return strings.Title(strings.ToLower(likelyLanguage))
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

	// Load OCR template
	ocrTemplatePath := filepath.Join(promptsDir, "ocr_prompt.tmpl")
	ocrTemplateContent, err := os.ReadFile(ocrTemplatePath)
	if err != nil {
		log.Printf("Could not read %s, using default template: %v", ocrTemplatePath, err)
		ocrTemplateContent = []byte(defaultOcrPrompt)
		if err := os.WriteFile(ocrTemplatePath, ocrTemplateContent, os.ModePerm); err != nil {
			log.Fatalf("Failed to write default OCR template to disk: %v", err)
		}
	}
	ocrTemplate, err = template.New("ocr").Funcs(sprig.FuncMap()).Parse(string(ocrTemplateContent))
	if err != nil {
		log.Fatalf("Failed to parse OCR template: %v", err)
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

func createVisionLLM() (llms.Model, error) {
	switch strings.ToLower(visionLlmProvider) {
	case "openai":
		if openaiAPIKey == "" {
			return nil, fmt.Errorf("OpenAI API key is not set")
		}
		return openai.New(
			openai.WithModel(visionLlmModel),
			openai.WithToken(openaiAPIKey),
		)
	case "ollama":
		host := os.Getenv("OLLAMA_HOST")
		if host == "" {
			host = "http://127.0.0.1:11434"
		}
		return ollama.New(
			ollama.WithModel(visionLlmModel),
			ollama.WithServerURL(host),
		)
	default:
		log.Printf("No Vision LLM provider created: %s", visionLlmProvider)
		return nil, nil
	}
}
