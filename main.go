package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/gin-gonic/gin"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

var (
	paperlessBaseURL  = os.Getenv("PAPERLESS_BASE_URL")
	paperlessAPIToken = os.Getenv("PAPERLESS_API_TOKEN")
	openaiAPIKey      = os.Getenv("OPENAI_API_KEY")
	tagToFilter       = "paperless-gpt"
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

func main() {
	if paperlessBaseURL == "" || paperlessAPIToken == "" {
		log.Fatal("Please set the PAPERLESS_BASE_URL and PAPERLESS_API_TOKEN environment variables.")
	}

	if llmProvider == "" || llmModel == "" {
		log.Fatal("Please set the LLM_PROVIDER and LLM_MODEL environment variables.")
	}

	if llmProvider == "openai" && openaiAPIKey == "" {
		log.Fatal("Please set the OPENAI_API_KEY environment variable for OpenAI provider.")
	}

	loadTemplates()

	// Create a Gin router with default middleware (logger and recovery)
	router := gin.Default()

	// API routes
	api := router.Group("/api")
	{
		api.GET("/documents", documentsHandler)
		api.POST("/generate-suggestions", generateSuggestionsHandler)
		api.PATCH("/update-documents", updateDocumentsHandler)
		api.GET("/filter-tag", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"tag": tagToFilter})
		})
		// get all tags
		api.GET("/tags", func(c *gin.Context) {
			ctx := c.Request.Context()

			tags, err := getAllTags(ctx, paperlessBaseURL, paperlessAPIToken)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching tags: %v", err)})
				log.Printf("Error fetching tags: %v", err)
				return
			}

			c.JSON(http.StatusOK, tags)
		})
		api.GET("/prompts", getPromptsHandler)
		api.POST("/prompts", updatePromptsHandler)
	}

	// Serve static files for the frontend under /static
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

func getPromptsHandler(c *gin.Context) {
	templateMutex.RLock()
	defer templateMutex.RUnlock()

	// Read the templates from files or use default content
	titleTemplateContent, err := os.ReadFile("title_prompt.tmpl")
	if err != nil {
		titleTemplateContent = []byte(defaultTitleTemplate)
	}

	tagTemplateContent, err := os.ReadFile("tag_prompt.tmpl")
	if err != nil {
		tagTemplateContent = []byte(defaultTagTemplate)
	}

	c.JSON(http.StatusOK, gin.H{
		"title_template": string(titleTemplateContent),
		"tag_template":   string(tagTemplateContent),
	})
}

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
		err = os.WriteFile("title_prompt.tmpl", []byte(req.TitleTemplate), 0644)
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
		err = os.WriteFile("tag_prompt.tmpl", []byte(req.TagTemplate), 0644)
		if err != nil {
			log.Printf("Failed to write tag_prompt.tmpl: %v", err)
		}
	}

	c.Status(http.StatusOK)
}

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

func getAllTags(ctx context.Context, baseURL, apiToken string) (map[string]int, error) {
	tagIDMapping := make(map[string]int)
	url := fmt.Sprintf("%s/api/tags/", baseURL)

	client := &http.Client{}

	for url != "" {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", fmt.Sprintf("Token %s", apiToken))

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("Error fetching tags: %d, %s", resp.StatusCode, string(bodyBytes))
		}

		var tagsResponse struct {
			Results []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"results"`
			Next string `json:"next"`
		}

		err = json.NewDecoder(resp.Body).Decode(&tagsResponse)
		if err != nil {
			return nil, err
		}

		for _, tag := range tagsResponse.Results {
			tagIDMapping[tag.Name] = tag.ID
		}

		url = tagsResponse.Next
	}

	return tagIDMapping, nil
}

// documentsHandler returns documents with the specific tag
func documentsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	documents, err := getDocumentsByTags(ctx, paperlessBaseURL, paperlessAPIToken, []string{tagToFilter})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching documents: %v", err)})
		log.Printf("Error fetching documents: %v", err)
		return
	}

	c.JSON(http.StatusOK, documents)
}

// generateSuggestionsHandler generates title suggestions for documents
func generateSuggestionsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	var suggestionRequest GenerateSuggestionsRequest
	if err := c.ShouldBindJSON(&suggestionRequest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request payload: %v", err)})
		log.Printf("Invalid request payload: %v", err)
		return
	}

	results, err := generateDocumentSuggestions(ctx, suggestionRequest)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error processing documents: %v", err)})
		log.Printf("Error processing documents: %v", err)
		return
	}

	c.JSON(http.StatusOK, results)
}

// updateDocumentsHandler updates documents with new titles
func updateDocumentsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var documents []DocumentSuggestion
	if err := c.ShouldBindJSON(&documents); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request payload: %v", err)})
		log.Printf("Invalid request payload: %v", err)
		return
	}

	err := updateDocuments(ctx, paperlessBaseURL, paperlessAPIToken, documents)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error updating documents: %v", err)})
		log.Printf("Error updating documents: %v", err)
		return
	}

	c.Status(http.StatusOK)
}

func getDocumentsByTags(ctx context.Context, baseURL, apiToken string, tags []string) ([]Document, error) {
	tagQueries := make([]string, len(tags))
	for i, tag := range tags {
		tagQueries[i] = fmt.Sprintf("tag:%s", tag)
	}
	searchQuery := strings.Join(tagQueries, " ")

	url := fmt.Sprintf("%s/api/documents/?query=%s", baseURL, searchQuery)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Token %s", apiToken))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Error searching documents: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	var documentsResponse GetDocumentsApiResponse

	err = json.NewDecoder(resp.Body).Decode(&documentsResponse)
	if err != nil {
		return nil, err
	}

	allTags, err := getAllTags(ctx, baseURL, apiToken)
	if err != nil {
		return nil, err
	}
	documents := make([]Document, 0, len(documentsResponse.Results))
	for _, result := range documentsResponse.Results {
		tagNames := make([]string, len(result.Tags))
		for i, resultTagID := range result.Tags {
			for tagName, tagID := range allTags {
				if resultTagID == tagID {
					tagNames[i] = tagName
					break
				}
			}
		}

		documents = append(documents, Document{
			ID:      result.ID,
			Title:   result.Title,
			Content: result.Content,
			Tags:    tagNames,
		})
	}

	return documents, nil
}

func generateDocumentSuggestions(ctx context.Context, suggestionRequest GenerateSuggestionsRequest) ([]DocumentSuggestion, error) {
	llm, err := createLLM()
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %v", err)
	}

	// Fetch all available tags from paperless-ngx
	availableTags, err := getAllTags(ctx, paperlessBaseURL, paperlessAPIToken)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch available tags: %v", err)
	}

	// Prepare a list of tag names
	availableTagNames := make([]string, 0, len(availableTags))
	for tagName := range availableTags {
		if tagName == tagToFilter {
			continue
		}
		availableTagNames = append(availableTagNames, tagName)
	}

	documents := suggestionRequest.Documents
	documentSuggestions := []DocumentSuggestion{}

	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := make([]error, 0)

	for i := range documents {
		wg.Add(1)
		go func(doc *Document) {
			defer wg.Done()
			documentID := doc.ID
			log.Printf("Processing Document %v...", documentID)

			content := doc.Content
			if len(content) > 5000 {
				content = content[:5000]
			}

			var suggestedTitle string
			var suggestedTags []string

			if suggestionRequest.GenerateTitles {
				suggestedTitle, err = getSuggestedTitle(ctx, llm, content)
				if err != nil {
					mu.Lock()
					errors = append(errors, fmt.Errorf("Document %d: %v", documentID, err))
					mu.Unlock()
					log.Printf("Error processing document %d: %v", documentID, err)
					return
				}
			}

			if suggestionRequest.GenerateTags {
				suggestedTags, err = getSuggestedTags(ctx, llm, content, suggestedTitle, availableTagNames)
				if err != nil {
					mu.Lock()
					errors = append(errors, fmt.Errorf("Document %d: %v", documentID, err))
					mu.Unlock()
					log.Printf("Error generating tags for document %d: %v", documentID, err)
					return
				}
			}

			mu.Lock()
			suggestion := DocumentSuggestion{
				ID:               documentID,
				OriginalDocument: *doc,
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
				suggestion.SuggestedTags = removeTagFromList(doc.Tags, tagToFilter)
			}
			documentSuggestions = append(documentSuggestions, suggestion)
			mu.Unlock()
			log.Printf("Document %d processed successfully.", documentID)
		}(&documents[i])
	}

	wg.Wait()

	if len(errors) > 0 {
		return nil, errors[0]
	}

	return documentSuggestions, nil
}

func removeTagFromList(tags []string, tagToRemove string) []string {
	filteredTags := []string{}
	for _, tag := range tags {
		if tag != tagToRemove {
			filteredTags = append(filteredTags, tag)
		}
	}
	return filteredTags
}

func getSuggestedTags(ctx context.Context, llm llms.Model, content string, suggestedTitle string, availableTags []string) ([]string, error) {
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

	completion, err := llm.GenerateContent(ctx, []llms.MessageContent{
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

func getLikelyLanguage() string {
	likelyLanguage := os.Getenv("LLM_LANGUAGE")
	if likelyLanguage == "" {
		likelyLanguage = "English"
	}
	return strings.Title(strings.ToLower(likelyLanguage))
}

func getSuggestedTitle(ctx context.Context, llm llms.Model, content string) (string, error) {
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

	completion, err := llm.GenerateContent(ctx, []llms.MessageContent{
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

func updateDocuments(ctx context.Context, baseURL, apiToken string, documents []DocumentSuggestion) error {
	client := &http.Client{}

	// Fetch all available tags
	availableTags, err := getAllTags(ctx, baseURL, apiToken)
	if err != nil {
		log.Printf("Error fetching available tags: %v", err)
		return err
	}

	for _, document := range documents {
		documentID := document.ID

		updatedFields := make(map[string]interface{})

		newTags := []int{}

		tags := document.SuggestedTags
		if len(tags) == 0 {
			tags = document.OriginalDocument.Tags
		}

		// Map suggested tag names to IDs
		for _, tagName := range tags {
			if tagID, exists := availableTags[tagName]; exists {
				// Skip the tag that we are filtering
				if tagName == tagToFilter {
					continue
				}
				newTags = append(newTags, tagID)
			} else {
				log.Printf("Tag '%s' does not exist in paperless-ngx, skipping.", tagName)
			}
		}

		updatedFields["tags"] = newTags

		suggestedTitle := document.SuggestedTitle
		if len(suggestedTitle) > 128 {
			suggestedTitle = suggestedTitle[:128]
		}
		if suggestedTitle != "" {
			updatedFields["title"] = suggestedTitle
		} else {
			log.Printf("No valid title found for document %d, skipping.", documentID)
		}

		// Send the update request
		url := fmt.Sprintf("%s/api/documents/%d/", baseURL, documentID)

		jsonData, err := json.Marshal(updatedFields)
		if err != nil {
			log.Printf("Error marshalling JSON for document %d: %v", documentID, err)
			return err
		}

		req, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("Error creating request for document %d: %v", documentID, err)
			return err
		}
		req.Header.Set("Authorization", fmt.Sprintf("Token %s", apiToken))
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Error updating document %d: %v", documentID, err)
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			log.Printf("Error updating document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
			return fmt.Errorf("Error updating document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
		}

		log.Printf("Document %d updated successfully.", documentID)
	}

	return nil
}
