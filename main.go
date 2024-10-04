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
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

type GetDocumentsApiResponse struct {
	Count    int         `json:"count"`
	Next     interface{} `json:"next"`
	Previous interface{} `json:"previous"`
	All      []int       `json:"all"`
	Results  []struct {
		ID                  int           `json:"id"`
		Correspondent       interface{}   `json:"correspondent"`
		DocumentType        interface{}   `json:"document_type"`
		StoragePath         interface{}   `json:"storage_path"`
		Title               string        `json:"title"`
		Content             string        `json:"content"`
		Tags                []int         `json:"tags"`
		Created             time.Time     `json:"created"`
		CreatedDate         string        `json:"created_date"`
		Modified            time.Time     `json:"modified"`
		Added               time.Time     `json:"added"`
		ArchiveSerialNumber interface{}   `json:"archive_serial_number"`
		OriginalFileName    string        `json:"original_file_name"`
		ArchivedFileName    string        `json:"archived_file_name"`
		Owner               int           `json:"owner"`
		UserCanChange       bool          `json:"user_can_change"`
		Notes               []interface{} `json:"notes"`
		SearchHit           struct {
			Score          float64 `json:"score"`
			Highlights     string  `json:"highlights"`
			NoteHighlights string  `json:"note_highlights"`
			Rank           int     `json:"rank"`
		} `json:"__search_hit__"`
	} `json:"results"`
}

type Document struct {
	ID             int      `json:"id"`
	Title          string   `json:"title"`
	Content        string   `json:"content"`
	Tags           []string `json:"tags"`
	SuggestedTitle string   `json:"suggested_title,omitempty"`
	SuggestedTags  []string `json:"suggested_tags,omitempty"`
}

var (
	paperlessBaseURL  = os.Getenv("PAPERLESS_BASE_URL")
	paperlessAPIToken = os.Getenv("PAPERLESS_API_TOKEN")
	openaiAPIKey      = os.Getenv("OPENAI_API_KEY")
	tagToFilter       = "paperless-gpt"
	llmProvider       = os.Getenv("LLM_PROVIDER")
	llmModel          = os.Getenv("LLM_MODEL")
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
	url := fmt.Sprintf("%s/api/tags/", baseURL)
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
		return nil, fmt.Errorf("Error fetching tags: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	var tagsResponse struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}

	err = json.NewDecoder(resp.Body).Decode(&tagsResponse)
	if err != nil {
		return nil, err
	}

	tagIDMapping := make(map[string]int)
	for _, tag := range tagsResponse.Results {
		tagIDMapping[tag.Name] = tag.ID
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

	var documents []Document
	if err := c.ShouldBindJSON(&documents); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request payload: %v", err)})
		log.Printf("Invalid request payload: %v", err)
		return
	}

	results, err := processDocuments(ctx, documents)
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
	var documents []Document
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

func getIDMappingForTags(ctx context.Context, baseURL, apiToken string, tagsToFilter []string) (map[string]int, error) {
	url := fmt.Sprintf("%s/api/tags/", baseURL)
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
		return nil, fmt.Errorf("Error fetching tags: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	var tagsResponse struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}

	err = json.NewDecoder(resp.Body).Decode(&tagsResponse)
	if err != nil {
		return nil, err
	}

	tagIDMapping := make(map[string]int)
	for _, tag := range tagsResponse.Results {
		for _, filterTag := range tagsToFilter {
			if tag.Name == filterTag {
				tagIDMapping[tag.Name] = tag.ID
			}
		}
	}

	return tagIDMapping, nil
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

func processDocuments(ctx context.Context, documents []Document) ([]Document, error) {
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

			suggestedTitle, err := getSuggestedTitle(ctx, llm, content)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("Document %d: %v", documentID, err))
				mu.Unlock()
				log.Printf("Error processing document %d: %v", documentID, err)
				return
			}

			suggestedTags, err := getSuggestedTags(ctx, llm, content, suggestedTitle, availableTagNames)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("Document %d: %v", documentID, err))
				mu.Unlock()
				log.Printf("Error generating tags for document %d: %v", documentID, err)
				return
			}

			mu.Lock()
			doc.SuggestedTitle = suggestedTitle
			doc.SuggestedTags = suggestedTags
			mu.Unlock()
			log.Printf("Document %d processed successfully.", documentID)
		}(&documents[i])
	}

	wg.Wait()

	if len(errors) > 0 {
		return nil, errors[0]
	}

	return documents, nil
}

func getSuggestedTags(ctx context.Context, llm llms.Model, content string, suggestedTitle string, availableTags []string) ([]string, error) {
	likelyLanguage := getLikelyLanguage()

	prompt := fmt.Sprintf(`I will provide you with the content and the title of a document. Your task is to select appropriate tags for the document from the list of available tags I will provide. Only select tags from the provided list. Respond only with the selected tags as a comma-separated list, without any additional information. The content is likely in %s.

Available Tags:
%s

Title:
%s

Content:
%s

Please concisely select the %s tags from the list above that best describe the document.
Be very selective and only choose the most relevant tags since too many tags will make the document less discoverable.
`, likelyLanguage, strings.Join(availableTags, ", "), suggestedTitle, content, likelyLanguage)

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
		return nil, fmt.Errorf("Error getting response from LLM: %v", err)
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

	prompt := fmt.Sprintf(`I will provide you with the content of a document that has been partially read by OCR (so it may contain errors).
Your task is to find a suitable document title that I can use as the title in the paperless-ngx program.
Respond only with the title, without any additional information. The content is likely in %s.

Content:
%s
`, likelyLanguage, content)
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
		return "", fmt.Errorf("Error getting response from LLM: %v", err)
	}

	return strings.TrimSpace(strings.Trim(completion.Choices[0].Content, "\"")), nil
}

func updateDocuments(ctx context.Context, baseURL, apiToken string, documents []Document) error {
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

		// Map suggested tag names to IDs
		for _, tagName := range document.SuggestedTags {
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
		updatedFields["title"] = suggestedTitle

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
