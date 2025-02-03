package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/fatih/color"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"gorm.io/gorm"
)

// Global Variables and Constants
var (

	// Logger
	log = logrus.New()

	// Environment Variables
	correspondentBlackList = strings.Split(os.Getenv("CORRESPONDENT_BLACK_LIST"), ",")

	paperlessBaseURL           = os.Getenv("PAPERLESS_BASE_URL")
	paperlessAPIToken          = os.Getenv("PAPERLESS_API_TOKEN")
	openaiAPIKey               = os.Getenv("OPENAI_API_KEY")
	manualTag                  = os.Getenv("MANUAL_TAG")
	autoTag                    = os.Getenv("AUTO_TAG")
	manualOcrTag               = os.Getenv("MANUAL_OCR_TAG") // Not used yet
	autoOcrTag                 = os.Getenv("AUTO_OCR_TAG")
	llmProvider                = os.Getenv("LLM_PROVIDER")
	llmModel                   = os.Getenv("LLM_MODEL")
	visionLlmProvider          = os.Getenv("VISION_LLM_PROVIDER")
	visionLlmModel             = os.Getenv("VISION_LLM_MODEL")
	logLevel                   = strings.ToLower(os.Getenv("LOG_LEVEL"))
	listenInterface            = os.Getenv("LISTEN_INTERFACE")
	autoGenerateTitle          = os.Getenv("AUTO_GENERATE_TITLE")
	autoGenerateTags           = os.Getenv("AUTO_GENERATE_TAGS")
	autoGenerateCorrespondents = os.Getenv("AUTO_GENERATE_CORRESPONDENTS")
	limitOcrPages              int // Will be read from OCR_LIMIT_PAGES
	tokenLimit                 = 0 // Will be read from TOKEN_LIMIT

	// Templates
	titleTemplate         *template.Template
	tagTemplate           *template.Template
	correspondentTemplate *template.Template
	ocrTemplate           *template.Template
	templateMutex         sync.RWMutex

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
	defaultCorrespondentTemplate = `I will provide you with the content of a document. Your task is to suggest a correspondent that is most relevant to the document.

Correspondents are the senders of documents that reach you. In the other direction, correspondents are the recipients of documents that you send.
In Paperless-ngx we can imagine correspondents as virtual drawers in which all documents of a person or company are stored. With just one click, we can find all the documents assigned to a specific correspondent.
Try to suggest a correspondent, either from the example list or come up with a new correspondent.

Respond only with a correspondent, without any additional information!

Be sure to choose a correspondent that is most relevant to the document.
Try to avoid any legal or financial suffixes like "GmbH" or "AG" in the correspondent name. For example use "Microsoft" instead of "Microsoft Ireland Operations Limited" or "Amazon" instead of "Amazon EU S.a.r.l.".

If you can't find a suitable correspondent, you can respond with "Unknown".

Example Correspondents:
{{.AvailableCorrespondents | join ", "}}

List of Correspondents with Blacklisted Names. Please avoid these correspondents or variations of their names:
{{.BlackList | join ", "}}

Title of the document:
{{.Title}}

The content is likely in {{.Language}}.

Document Content:
{{.Content}}
`
	defaultOcrPrompt = `Just transcribe the text in this image and preserve the formatting and layout (high quality OCR). Do that for ALL the text in the image. Be thorough and pay attention. This is very important. The image is from a text document so be sure to continue until the bottom of the page. Thanks a lot! You tend to forget about some text in the image so please focus! Use markdown format but without a code block.`
)

// App struct to hold dependencies
type App struct {
	Client    *PaperlessClient
	Database  *gorm.DB
	LLM       llms.Model
	VisionLLM llms.Model
}

func main() {
	// Validate Environment Variables
	validateOrDefaultEnvVars()

	// Initialize logrus logger
	initLogger()

	// Print version
	printVersion()

	// Initialize PaperlessClient
	client := NewPaperlessClient(paperlessBaseURL, paperlessAPIToken)

	// Initialize Database
	database := InitializeDB()

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
		Database:  database,
		LLM:       llm,
		VisionLLM: visionLlm,
	}

	// Start background process for auto-tagging
	go func() {
		minBackoffDuration := 10 * time.Second
		maxBackoffDuration := time.Hour
		pollingInterval := 10 * time.Second

		backoffDuration := minBackoffDuration
		for {
			processedCount, err := func() (int, error) {
				count := 0
				if isOcrEnabled() {
					ocrCount, err := app.processAutoOcrTagDocuments()
					if err != nil {
						return 0, fmt.Errorf("error in processAutoOcrTagDocuments: %w", err)
					}
					count += ocrCount
				}
				autoCount, err := app.processAutoTagDocuments()
				if err != nil {
					return 0, fmt.Errorf("error in processAutoTagDocuments: %w", err)
				}
				count += autoCount
				return count, nil
			}()

			if err != nil {
				log.Errorf("Error in processAutoTagDocuments: %v", err)
				time.Sleep(backoffDuration)
				backoffDuration *= 2 // Exponential backoff
				if backoffDuration > maxBackoffDuration {
					log.Warnf("Repeated errors in processAutoTagDocuments detected. Setting backoff to %v", maxBackoffDuration)
					backoffDuration = maxBackoffDuration
				}
			} else {
				backoffDuration = minBackoffDuration
			}

			if processedCount == 0 {
				time.Sleep(pollingInterval)
			}
		}
	}()

	// Create a Gin router with default middleware (logger and recovery)
	router := gin.Default()

	// API routes
	api := router.Group("/api")
	{
		api.GET("/documents", app.documentsHandler)
		// http://localhost:8080/api/documents/544
		api.GET("/documents/:id", app.getDocumentHandler())
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

		// Local db actions
		api.GET("/modifications", app.getModificationHistoryHandler)
		api.POST("/undo-modification/:id", app.undoModificationHandler)

		// Get public Paperless environment (as set in environment variables)
		api.GET("/paperless-url", func(c *gin.Context) {
			baseUrl := os.Getenv("PAPERLESS_PUBLIC_URL")
			if baseUrl == "" {
				baseUrl = os.Getenv("PAPERLESS_BASE_URL")
			}
			baseUrl = strings.TrimRight(baseUrl, "/")
			c.JSON(http.StatusOK, gin.H{"url": baseUrl})
		})
	}

	// Serve embedded web-app files
	// router.GET("/*filepath", func(c *gin.Context) {
	// 	filepath := c.Param("filepath")
	// 	// Remove leading slash from filepath
	// 	filepath = strings.TrimPrefix(filepath, "/")
	// 	// Handle static assets under /assets/
	// 	serveEmbeddedFile(c, "", filepath)
	// })

	// Instead of wildcard, serve specific files
	router.GET("/favicon.ico", func(c *gin.Context) {
		serveEmbeddedFile(c, "", "favicon.ico")
	})
	router.GET("/vite.svg", func(c *gin.Context) {
		serveEmbeddedFile(c, "", "vite.svg")
	})
	router.GET("/assets/*filepath", func(c *gin.Context) {
		filepath := c.Param("filepath")
		fmt.Printf("Serving asset: %s\n", filepath)
		serveEmbeddedFile(c, "assets", filepath)
	})
	router.GET("/", func(c *gin.Context) {
		serveEmbeddedFile(c, "", "index.html")
	})
	// history route
	router.GET("/history", func(c *gin.Context) {
		serveEmbeddedFile(c, "", "index.html")
	})
	// experimental-ocr route
	router.GET("/experimental-ocr", func(c *gin.Context) {
		serveEmbeddedFile(c, "", "index.html")
	})

	// Start OCR worker pool
	numWorkers := 1 // Number of workers to start
	startWorkerPool(app, numWorkers)

	if listenInterface == "" {
		listenInterface = ":8080"
	}
	log.Infoln("Server started on interface", listenInterface)
	if err := router.Run(listenInterface); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func printVersion() {
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	banner := `
    ╔═══════════════════════════════════════╗
    ║             Paperless GPT             ║
    ╚═══════════════════════════════════════╝`

	fmt.Printf("%s\n", cyan(banner))
	fmt.Printf("\n%s %s\n", yellow("Version:"), version)
	if commit != "" {
		fmt.Printf("%s %s\n", yellow("Commit:"), commit)
	}
	if buildDate != "" {
		fmt.Printf("%s %s\n", yellow("Build Date:"), buildDate)
	}
	fmt.Printf("%s %s/%s\n", yellow("Platform:"), runtime.GOOS, runtime.GOARCH)
	fmt.Printf("%s %s\n", yellow("Go Version:"), runtime.Version())
	fmt.Printf("%s %s\n", yellow("Started:"), time.Now().Format(time.RFC1123))
	fmt.Println()
}

func initLogger() {
	switch logLevel {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
		if logLevel != "" {
			log.Fatalf("Invalid log level: '%s'.", logLevel)
		}
	}

	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
}

func isOcrEnabled() bool {
	return visionLlmModel != "" && visionLlmProvider != ""
}

// validateOrDefaultEnvVars ensures all necessary environment variables are set
func validateOrDefaultEnvVars() {
	if manualTag == "" {
		manualTag = "paperless-gpt"
	}
	fmt.Printf("Using %s as manual tag\n", manualTag)

	if autoTag == "" {
		autoTag = "paperless-gpt-auto"
	}
	fmt.Printf("Using %s as auto tag\n", autoTag)

	if manualOcrTag == "" {
		manualOcrTag = "paperless-gpt-ocr"
	}
	if isOcrEnabled() {
		fmt.Printf("Using %s as manual OCR tag\n", manualOcrTag)
	}

	if autoOcrTag == "" {
		autoOcrTag = "paperless-gpt-ocr-auto"
	}
	if isOcrEnabled() {
		fmt.Printf("Using %s as auto OCR tag\n", autoOcrTag)
	}

	if paperlessBaseURL == "" {
		log.Fatal("Please set the PAPERLESS_BASE_URL environment variable.")
	}

	if paperlessAPIToken == "" {
		log.Fatal("Please set the PAPERLESS_API_TOKEN environment variable.")
	}

	if llmProvider == "" {
		log.Fatal("Please set the LLM_PROVIDER environment variable.")
	}

	if visionLlmProvider != "" && visionLlmProvider != "openai" && visionLlmProvider != "ollama" {
		log.Fatal("Please set the LLM_PROVIDER environment variable to 'openai' or 'ollama'.")
	}

	if llmModel == "" {
		log.Fatal("Please set the LLM_MODEL environment variable.")
	}

	if (llmProvider == "openai" || visionLlmProvider == "openai") && openaiAPIKey == "" {
		log.Fatal("Please set the OPENAI_API_KEY environment variable for OpenAI provider.")
	}

	if isOcrEnabled() {
		rawLimitOcrPages := os.Getenv("OCR_LIMIT_PAGES")
		if rawLimitOcrPages == "" {
			limitOcrPages = 5
		} else {
			var err error
			limitOcrPages, err = strconv.Atoi(rawLimitOcrPages)
			if err != nil {
				log.Fatalf("Invalid OCR_LIMIT_PAGES value: %v", err)
			}
		}
	}

	// Initialize token limit from environment variable
	if limit := os.Getenv("TOKEN_LIMIT"); limit != "" {
		if parsed, err := strconv.Atoi(limit); err == nil {
			if parsed < 0 {
				log.Fatalf("TOKEN_LIMIT must be non-negative, got: %d", parsed)
			}
			tokenLimit = parsed
			log.Infof("Using token limit: %d", tokenLimit)
		}
	}
}

// documentLogger creates a logger with document context
func documentLogger(documentID int) *logrus.Entry {
	return log.WithField("document_id", documentID)
}

// processAutoTagDocuments handles the background auto-tagging of documents
func (app *App) processAutoTagDocuments() (int, error) {
	ctx := context.Background()

	documents, err := app.Client.GetDocumentsByTags(ctx, []string{autoTag}, 25)
	if err != nil {
		return 0, fmt.Errorf("error fetching documents with autoTag: %w", err)
	}

	if len(documents) == 0 {
		log.Debugf("No documents with tag %s found", autoTag)
		return 0, nil // No documents to process
	}

	log.Debugf("Found at least %d remaining documents with tag %s", len(documents), autoTag)

	for _, document := range documents {
		docLogger := documentLogger(document.ID)
		docLogger.Info("Processing document for auto-tagging")

		suggestionRequest := GenerateSuggestionsRequest{
			Documents:              []Document{document},
			GenerateTitles:         strings.ToLower(autoGenerateTitle) != "false",
			GenerateTags:           strings.ToLower(autoGenerateTags) != "false",
			GenerateCorrespondents: strings.ToLower(autoGenerateCorrespondents) != "false",
		}

		suggestions, err := app.generateDocumentSuggestions(ctx, suggestionRequest, docLogger)
		if err != nil {
			return 0, fmt.Errorf("error generating suggestions for document %d: %w", document.ID, err)
		}

		err = app.Client.UpdateDocuments(ctx, suggestions, app.Database, false)
		if err != nil {
			return 0, fmt.Errorf("error updating document %d: %w", document.ID, err)
		}

		docLogger.Info("Successfully processed document")
	}
	return len(documents), nil
}

// processAutoOcrTagDocuments handles the background auto-tagging of OCR documents
func (app *App) processAutoOcrTagDocuments() (int, error) {
	ctx := context.Background()

	documents, err := app.Client.GetDocumentsByTags(ctx, []string{autoOcrTag}, 25)
	if err != nil {
		return 0, fmt.Errorf("error fetching documents with autoOcrTag: %w", err)
	}

	if len(documents) == 0 {
		log.Debugf("No documents with tag %s found", autoOcrTag)
		return 0, nil // No documents to process
	}

	log.Debugf("Found at least %d remaining documents with tag %s", len(documents), autoOcrTag)

	for _, document := range documents {
		docLogger := documentLogger(document.ID)
		docLogger.Info("Processing document for OCR")

		ocrContent, err := app.ProcessDocumentOCR(ctx, document.ID)
		if err != nil {
			return 0, fmt.Errorf("error processing OCR for document %d: %w", document.ID, err)
		}
		docLogger.Debug("OCR processing completed")

		err = app.Client.UpdateDocuments(ctx, []DocumentSuggestion{
			{
				ID:               document.ID,
				OriginalDocument: document,
				SuggestedContent: ocrContent,
				RemoveTags:       []string{autoOcrTag},
			},
		}, app.Database, false)
		if err != nil {
			return 0, fmt.Errorf("error updating document %d after OCR: %w", document.ID, err)
		}

		docLogger.Info("Successfully processed document OCR")
	}
	return 1, nil
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
		log.Errorf("Could not read %s, using default template: %v", titleTemplatePath, err)
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
		log.Errorf("Could not read %s, using default template: %v", tagTemplatePath, err)
		tagTemplateContent = []byte(defaultTagTemplate)
		if err := os.WriteFile(tagTemplatePath, tagTemplateContent, os.ModePerm); err != nil {
			log.Fatalf("Failed to write default tag template to disk: %v", err)
		}
	}
	tagTemplate, err = template.New("tag").Funcs(sprig.FuncMap()).Parse(string(tagTemplateContent))
	if err != nil {
		log.Fatalf("Failed to parse tag template: %v", err)
	}

	// Load correspondent template
	correspondentTemplatePath := filepath.Join(promptsDir, "correspondent_prompt.tmpl")
	correspondentTemplateContent, err := os.ReadFile(correspondentTemplatePath)
	if err != nil {
		log.Errorf("Could not read %s, using default template: %v", correspondentTemplatePath, err)
		correspondentTemplateContent = []byte(defaultCorrespondentTemplate)
		if err := os.WriteFile(correspondentTemplatePath, correspondentTemplateContent, os.ModePerm); err != nil {
			log.Fatalf("Failed to write default correspondent template to disk: %v", err)
		}
	}
	correspondentTemplate, err = template.New("correspondent").Funcs(sprig.FuncMap()).Parse(string(correspondentTemplateContent))
	if err != nil {
		log.Fatalf("Failed to parse correspondent template: %v", err)
	}

	// Load OCR template
	ocrTemplatePath := filepath.Join(promptsDir, "ocr_prompt.tmpl")
	ocrTemplateContent, err := os.ReadFile(ocrTemplatePath)
	if err != nil {
		log.Errorf("Could not read %s, using default template: %v", ocrTemplatePath, err)
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
		log.Infoln("Vision LLM not enabled")
		return nil, nil
	}
}
