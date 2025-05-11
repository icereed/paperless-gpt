package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"paperless-gpt/ocr"
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
	"github.com/tmc/langchaingo/llms/mistral"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"gorm.io/gorm"
)

// Global Variables and Constants
var (

	// Logger
	log = logrus.New()

	// Environment Variables
	paperlessInsecureSkipVerify   = os.Getenv("PAPERLESS_INSECURE_SKIP_VERIFY") == "true"
	correspondentBlackList        = strings.Split(os.Getenv("CORRESPONDENT_BLACK_LIST"), ",")
	paperlessBaseURL              = os.Getenv("PAPERLESS_BASE_URL")
	paperlessAPIToken             = os.Getenv("PAPERLESS_API_TOKEN")
	azureDocAIEndpoint            = os.Getenv("AZURE_DOCAI_ENDPOINT")
	azureDocAIKey                 = os.Getenv("AZURE_DOCAI_KEY")
	azureDocAIModelID             = os.Getenv("AZURE_DOCAI_MODEL_ID")
	azureDocAITimeout             = os.Getenv("AZURE_DOCAI_TIMEOUT_SECONDS")
	AzureDocAIOutputContentFormat = os.Getenv("AZURE_DOCAI_OUTPUT_CONTENT_FORMAT")
	openaiAPIKey                  = os.Getenv("OPENAI_API_KEY")
	manualTag                     = os.Getenv("MANUAL_TAG")
	autoTag                       = os.Getenv("AUTO_TAG")
	manualOcrTag                  = os.Getenv("MANUAL_OCR_TAG") // Not used yet
	autoOcrTag                    = os.Getenv("AUTO_OCR_TAG")
	llmProvider                   = os.Getenv("LLM_PROVIDER")
	llmModel                      = os.Getenv("LLM_MODEL")
	visionLlmProvider             = os.Getenv("VISION_LLM_PROVIDER")
	visionLlmModel                = os.Getenv("VISION_LLM_MODEL")
	logLevel                      = strings.ToLower(os.Getenv("LOG_LEVEL"))
	listenInterface               = os.Getenv("LISTEN_INTERFACE")
	autoGenerateTitle             = os.Getenv("AUTO_GENERATE_TITLE")
	autoGenerateTags              = os.Getenv("AUTO_GENERATE_TAGS")
	autoGenerateCorrespondents    = os.Getenv("AUTO_GENERATE_CORRESPONDENTS")
	autoGenerateCreatedDate       = os.Getenv("AUTO_GENERATE_CREATED_DATE")
	limitOcrPages                 int // Will be read from OCR_LIMIT_PAGES
	tokenLimit                    = 0 // Will be read from TOKEN_LIMIT
	createLocalHOCR               = os.Getenv("CREATE_LOCAL_HOCR") == "true"
	createLocalPDF                = os.Getenv("CREATE_LOCAL_PDF") == "true"
	localHOCRPath                 = os.Getenv("LOCAL_HOCR_PATH")
	localPDFPath                  = os.Getenv("LOCAL_PDF_PATH")
	pdfUpload                     = os.Getenv("PDF_UPLOAD") == "true"
	pdfReplace                    = os.Getenv("PDF_REPLACE") == "true"
	pdfCopyMetadata               = os.Getenv("PDF_COPY_METADATA") == "true"
	pdfOCRCompleteTag             = os.Getenv("PDF_OCR_COMPLETE_TAG")
	pdfOCRTagging                 = os.Getenv("PDF_OCR_TAGGING") == "true"
	doclingURL                    = os.Getenv("DOCLING_URL")
	doclingImageExportMode        = os.Getenv("DOCLING_IMAGE_EXPORT_MODE")

	// Templates
	titleTemplate         *template.Template
	tagTemplate           *template.Template
	correspondentTemplate *template.Template
	createdDateTemplate   *template.Template
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
	defaultCreatedDateTemplate = `I will provide you with the content of a document. Your task is to find the date when the document was created.
Respond only with the date in YYYY-MM-DD format, without any additional information. If no day was found, use the first day of the month. If no month was found, use January. If no date was found at all, answer with today's date.
The content is likely in {{.Language}}. Today's date is {{.Today}}.

Content:
{{.Content}}
`
	defaultOcrPrompt = `Just transcribe the text in this image and preserve the formatting and layout (high quality OCR). Do that for ALL the text in the image. Be thorough and pay attention. This is very important. The image is from a text document so be sure to continue until the bottom of the page. Thanks a lot! You tend to forget about some text in the image so please focus! Use markdown format but without a code block.`
)

// App struct to hold dependencies
type App struct {
	Client            ClientInterface
	Database          *gorm.DB
	LLM               llms.Model
	VisionLLM         llms.Model
	ocrProvider       ocr.Provider      // OCR provider interface
	docProcessor      DocumentProcessor // Optional: Can be used for mocking
	localHOCRPath     string            // Path for saving hOCR files locally
	localPDFPath      string            // Path for saving PDF files locally
	createLocalHOCR   bool              // Whether to save hOCR files locally
	createLocalPDF    bool              // Whether to create PDF files locally
	pdfUpload         bool              // Whether to upload processed PDFs to paperless-ngx
	pdfReplace        bool              // Whether to replace original document after upload
	pdfCopyMetadata   bool              // Whether to copy metadata from original to uploaded PDF
	pdfOCRCompleteTag string            // Tag to add to documents that have been OCR processed
	pdfOCRTagging     bool              // Whether to add the OCR complete tag to processed PDFs
}

func main() {
	// Context for proper control of background-thread
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	// Initialize OCR provider
	var ocrProvider ocr.Provider
	providerType := os.Getenv("OCR_PROVIDER")
	if providerType == "" {
		providerType = "llm" // Default to LLM provider
	}

	var promptBuffer bytes.Buffer
	err = ocrTemplate.Execute(&promptBuffer, map[string]interface{}{
		"Language": getLikelyLanguage(),
	})
	if err != nil {
		log.Fatalf("error executing tag template: %v", err)
	}

	ocrPrompt := promptBuffer.String()

	ocrConfig := ocr.Config{
		Provider:                 providerType,
		GoogleProjectID:          os.Getenv("GOOGLE_PROJECT_ID"),
		GoogleLocation:           os.Getenv("GOOGLE_LOCATION"),
		GoogleProcessorID:        os.Getenv("GOOGLE_PROCESSOR_ID"),
		VisionLLMProvider:        visionLlmProvider,
		VisionLLMModel:           visionLlmModel,
		VisionLLMPrompt:          ocrPrompt,
		AzureEndpoint:            azureDocAIEndpoint,
		AzureAPIKey:              azureDocAIKey,
		AzureModelID:             azureDocAIModelID,
		AzureOutputContentFormat: AzureDocAIOutputContentFormat,
		MistralAPIKey:            os.Getenv("MISTRAL_API_KEY"),
		MistralModel:             os.Getenv("MISTRAL_MODEL"),
		DoclingURL:               doclingURL,
		DoclingImageExportMode:   doclingImageExportMode,
		EnableHOCR:               true, // Always generate hOCR struct if provider supports it
	}

	// Parse Azure timeout if set
	if azureDocAITimeout != "" {
		if timeout, err := strconv.Atoi(azureDocAITimeout); err == nil {
			ocrConfig.AzureTimeout = timeout
		} else {
			log.Warnf("Invalid AZURE_DOCAI_TIMEOUT_SECONDS value: %v, using default", err)
		}
	}

	// If provider is LLM, but no VISION_LLM_PROVIDER is set, don't initialize OCR provider
	if providerType == "llm" && visionLlmProvider == "" {
		log.Warn("OCR provider is set to LLM, but no VISION_LLM_PROVIDER is set. Disabling OCR.")
	} else {
		ocrProvider, err = ocr.NewProvider(ocrConfig)
		if err != nil {
			log.Fatalf("Failed to initialize OCR provider: %v", err)
		}
	}

	// Initialize App with dependencies
	app := &App{
		Client:            client,
		Database:          database,
		LLM:               llm,
		VisionLLM:         visionLlm,
		ocrProvider:       ocrProvider,
		docProcessor:      nil, // App itself implements DocumentProcessor
		localHOCRPath:     localHOCRPath,
		localPDFPath:      localPDFPath,
		createLocalHOCR:   createLocalHOCR,
		createLocalPDF:    createLocalPDF,
		pdfUpload:         pdfUpload,
		pdfReplace:        pdfReplace,
		pdfCopyMetadata:   pdfCopyMetadata,
		pdfOCRCompleteTag: pdfOCRCompleteTag,
		pdfOCRTagging:     pdfOCRTagging,
	}

	if app.isOcrEnabled() {
		fmt.Printf("Using %s as manual OCR tag\n", manualOcrTag)
		fmt.Printf("Using %s as auto OCR tag\n", autoOcrTag)
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

	// Start Background-Tasks for Auto-Tagging and Auto-OCR (if enabled)
	StartBackgroundTasks(ctx, app)

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
			enabled := app.isOcrEnabled()
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

func (app *App) isOcrEnabled() bool {
	return app.ocrProvider != nil
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

	if autoOcrTag == "" {
		autoOcrTag = "paperless-gpt-ocr-auto"
	}

	if pdfOCRCompleteTag == "" {
		pdfOCRCompleteTag = "paperless-gpt-ocr-complete"
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

	if visionLlmProvider != "" && visionLlmProvider != "openai" && visionLlmProvider != "ollama" && visionLlmProvider != "mistral" {
		log.Fatal("Please set the VISION_LLM_PROVIDER environment variable to 'openai', 'ollama', or 'mistral'.")
	}

	// Validate OCR provider if set
	ocrProvider := os.Getenv("OCR_PROVIDER")
	if ocrProvider == "azure" {
		if azureDocAIEndpoint == "" {
			log.Fatal("Please set the AZURE_DOCAI_ENDPOINT environment variable for Azure provider")
		}
		if azureDocAIKey == "" {
			log.Fatal("Please set the AZURE_DOCAI_KEY environment variable for Azure provider")
		}
	}

	if ocrProvider == "docling" {
		if doclingURL == "" {
			log.Fatal("Please set the DOCLING_URL environment variable for Docling provider")
		}
		if doclingImageExportMode == "" {
			doclingImageExportMode = "embedded" // Default to PNG
			log.Infof("DOCLING_IMAGE_EXPORT_MODE not set, defaulting to %s", doclingImageExportMode)
		}
	}

	if llmModel == "" {
		log.Fatal("Please set the LLM_MODEL environment variable.")
	}

	if llmProvider == "mistral" {
		if os.Getenv("MISTRAL_API_KEY") == "" {
			log.Fatal("Please set the MISTRAL_API_KEY environment variable for Mistral provider.")
		}
	} else if llmProvider == "openai" || visionLlmProvider == "openai" {
		if openaiAPIKey == "" {
			log.Fatal("Please set the OPENAI_API_KEY environment variable for OpenAI provider.")
		}

		// Check Azure specific configuration
		if strings.ToLower(os.Getenv("OPENAI_API_TYPE")) == "azure" {
			if baseURL := os.Getenv("OPENAI_BASE_URL"); baseURL == "" {
				log.Fatal("Please set the OPENAI_BASE_URL environment variable for Azure OpenAI.")
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

	// Set default for hOCR output path
	if localHOCRPath == "" && createLocalHOCR {
		localHOCRPath = "/app/hocr"

		// Fallback dir
		if _, err := os.Stat("/app"); os.IsNotExist(err) {
			localHOCRPath = filepath.Join(os.TempDir(), "hocr")
			log.Warnf("'/app' directory not found, using %s as fallback for hOCR output", localHOCRPath)
		}
	}

	// Set default for PDF output path
	if localPDFPath == "" && createLocalPDF {
		localPDFPath = "/app/pdf"

		// Fallback dir
		if _, err := os.Stat("/app"); os.IsNotExist(err) {
			localPDFPath = filepath.Join(os.TempDir(), "pdf")
			log.Warnf("'/app' directory not found, using %s as fallback for PDF output", localPDFPath)
		}
	}

	// Log OCR feature settings
	ocrProviderEnv := os.Getenv("OCR_PROVIDER")
	if ocrProviderEnv != "" {
		log.Infof("OCR provider: %s", os.Getenv("OCR_PROVIDER"))

		if createLocalHOCR {
			log.Infof("hOCR file creation is enabled, output path: %s", localHOCRPath)
		}

		if createLocalPDF {
			log.Infof("PDF generation is enabled, output path: %s", localPDFPath)
		}
	}
	if pdfUpload {
		log.Infof("PDF upload to paperless-ngx is enabled")
		if pdfReplace {
			log.Infof("Original documents will be replaced after OCR upload")
		}
		if pdfCopyMetadata {
			log.Infof("Metadata will be copied from original documents")
		}
	}
	if pdfOCRTagging {
		log.Infof("OCR complete tagging enabled with tag: %s", pdfOCRCompleteTag)
	}
}

// documentLogger creates a logger with document context
func documentLogger(documentID int) *logrus.Entry {
	return log.WithField("document_id", documentID)
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

	// Load createdDate template
	createdDateTemplatePath := filepath.Join(promptsDir, "created_date_prompt.tmpl")
	createdDateTemplateContent, err := os.ReadFile(createdDateTemplatePath)
	if err != nil {
		log.Errorf("Could not read %s, using default template: %v", createdDateTemplatePath, err)
		createdDateTemplateContent = []byte(defaultCreatedDateTemplate)
		if err := os.WriteFile(createdDateTemplatePath, createdDateTemplateContent, os.ModePerm); err != nil {
			log.Fatalf("Failed to write default date template to disk: %v", err)
		}
	}

	createdDateTemplate, err = template.New("created_date").Funcs(sprig.FuncMap()).Parse(string(createdDateTemplateContent))
	if err != nil {
		log.Fatalf("Failed to parse createdDate template: %v", err)
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

// getRateLimitConfig gets rate limiting configuration from environment variables
// with LLM or VISION_LLM prefixes or default values
func getRateLimitConfig(isVision bool) RateLimitConfig {
	// Use LLM or VISION_LLM prefix based on the type of LLM
	prefix := "LLM_"
	if isVision {
		prefix = "VISION_LLM_"
	}

	// Read environment variables with appropriate prefix
	rpmStr := os.Getenv(prefix + "REQUESTS_PER_MINUTE")
	maxRetriesStr := os.Getenv(prefix + "MAX_RETRIES")
	backoffMaxWaitStr := os.Getenv(prefix + "BACKOFF_MAX_WAIT")

	// Default values
	var rpm float64 = 120                 // Default to 120 requests per minute (2/second)
	var maxRetries int = 3                // Default to 3 retries
	var backoffMaxWait = 30 * time.Second // Default to 30 seconds

	// Parse values if provided
	if rpmStr != "" {
		if parsed, err := strconv.ParseFloat(rpmStr, 64); err == nil {
			rpm = parsed
		}
	}
	if maxRetriesStr != "" {
		if parsed, err := strconv.Atoi(maxRetriesStr); err == nil {
			maxRetries = parsed
		}
	}
	if backoffMaxWaitStr != "" {
		if parsed, err := time.ParseDuration(backoffMaxWaitStr); err == nil {
			backoffMaxWait = parsed
		}
	}

	return RateLimitConfig{
		RequestsPerMinute: rpm,
		MaxRetries:        maxRetries,
		BackoffMaxWait:    backoffMaxWait,
	}
}

// createLLM creates the appropriate LLM client based on the provider
func createLLM() (llms.Model, error) {
	switch strings.ToLower(llmProvider) {
	case "mistral":
		mistralApiKey := os.Getenv("MISTRAL_API_KEY")
		if mistralApiKey == "" {
			return nil, fmt.Errorf("Mistral API key is not set")
		}
		llm, err := mistral.New(
			mistral.WithModel(llmModel),
			mistral.WithAPIKey(mistralApiKey),
		)
		if err != nil {
			return nil, err
		}

		// Apply rate limiting with isVision=false
		return NewRateLimitedLLM(llm, getRateLimitConfig(false)), nil
	case "openai":
		if openaiAPIKey == "" {
			return nil, fmt.Errorf("OpenAI API key is not set")
		}

		options := []openai.Option{
			openai.WithModel(llmModel),
			openai.WithToken(openaiAPIKey),
			openai.WithHTTPClient(createCustomHTTPClient()),
		}

		if strings.ToLower(os.Getenv("OPENAI_API_TYPE")) == "azure" {
			baseURL := os.Getenv("OPENAI_BASE_URL")
			if baseURL == "" {
				return nil, fmt.Errorf("OPENAI_BASE_URL is required for Azure OpenAI")
			}
			options = append(options,
				openai.WithAPIType(openai.APITypeAzure),
				openai.WithBaseURL(baseURL),
				openai.WithEmbeddingModel("this-is-not-used"), // This is mandatory for Azure by langchain-go
			)
		}

		llm, err := openai.New(options...)
		if err != nil {
			return nil, err
		}

		// Apply rate limiting with isVision=false
		return NewRateLimitedLLM(llm, getRateLimitConfig(false)), nil
	case "ollama":
		host := os.Getenv("OLLAMA_HOST")
		if host == "" {
			host = "http://127.0.0.1:11434"
		}
		llm, err := ollama.New(
			ollama.WithModel(llmModel),
			ollama.WithServerURL(host),
		)
		if err != nil {
			return nil, err
		}

		// Apply rate limiting with isVision=false
		return NewRateLimitedLLM(llm, getRateLimitConfig(false)), nil
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", llmProvider)
	}
}

func createVisionLLM() (llms.Model, error) {
	switch strings.ToLower(visionLlmProvider) {
	case "mistral":
		mistralApiKey := os.Getenv("MISTRAL_API_KEY")
		if mistralApiKey == "" {
			return nil, fmt.Errorf("Mistral API key is not set")
		}
		llm, err := openai.New(
			openai.WithToken(mistralApiKey),
			openai.WithModel(visionLlmModel),
			openai.WithBaseURL("https://api.mistral.ai/v1"),
		)
		if err != nil {
			return nil, err
		}

		// Apply rate limiting with isVision=true
		return NewRateLimitedLLM(llm, getRateLimitConfig(true)), nil
	case "openai":
		if openaiAPIKey == "" {
			return nil, fmt.Errorf("OpenAI API key is not set")
		}

		options := []openai.Option{
			openai.WithModel(visionLlmModel),
			openai.WithToken(openaiAPIKey),
			openai.WithHTTPClient(createCustomHTTPClient()),
		}

		if strings.ToLower(os.Getenv("OPENAI_API_TYPE")) == "azure" {
			baseURL := os.Getenv("OPENAI_BASE_URL")
			if baseURL == "" {
				return nil, fmt.Errorf("OPENAI_BASE_URL is required for Azure OpenAI")
			}
			options = append(options,
				openai.WithAPIType(openai.APITypeAzure),
				openai.WithBaseURL(baseURL),
				openai.WithEmbeddingModel("this-is-not-used"), // This is mandatory for Azure by langchain-go
			)
		}

		llm, err := openai.New(options...)
		if err != nil {
			return nil, err
		}

		// Apply rate limiting with isVision=true
		return NewRateLimitedLLM(llm, getRateLimitConfig(true)), nil
	case "ollama":
		host := os.Getenv("OLLAMA_HOST")
		if host == "" {
			host = "http://127.0.0.1:11434"
		}
		llm, err := ollama.New(
			ollama.WithModel(visionLlmModel),
			ollama.WithServerURL(host),
		)
		if err != nil {
			return nil, err
		}

		// Apply rate limiting with isVision=true
		return NewRateLimitedLLM(llm, getRateLimitConfig(true)), nil
	default:
		log.Infoln("Vision LLM not enabled")
		return nil, nil
	}
}

func createCustomHTTPClient() *http.Client {
	// Create custom transport that adds headers
	customTransport := &headerTransport{
		transport: http.DefaultTransport,
		headers: map[string]string{
			"X-Title": "paperless-gpt",
		},
	}

	// Create custom client with the transport
	httpClient := http.DefaultClient
	httpClient.Transport = customTransport

	return httpClient
}

// headerTransport is a custom http.RoundTripper that adds custom headers to requests
type headerTransport struct {
	transport http.RoundTripper
	headers   map[string]string
}

// RoundTrip implements the http.RoundTripper interface
func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, value := range t.headers {
		req.Header.Add(key, value)
	}
	return t.transport.RoundTrip(req)
}
