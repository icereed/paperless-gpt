package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gen2brain/go-fitz"
	"golang.org/x/sync/errgroup"
)

// PaperlessClient struct to interact with the Paperless-NGX API
type PaperlessClient struct {
	BaseURL     string
	APIToken    string
	HTTPClient  *http.Client
	CacheFolder string
}

// NewPaperlessClient creates a new instance of PaperlessClient with a default HTTP client
func NewPaperlessClient(baseURL, apiToken string) *PaperlessClient {
	cacheFolder := os.Getenv("PAPERLESS_GPT_CACHE_DIR")

	return &PaperlessClient{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		APIToken:    apiToken,
		HTTPClient:  &http.Client{},
		CacheFolder: cacheFolder,
	}
}

// Do method to make requests to the Paperless-NGX API
func (c *PaperlessClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := fmt.Sprintf("%s/%s", c.BaseURL, strings.TrimLeft(path, "/"))
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Token %s", c.APIToken))

	// Set Content-Type if body is present
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.HTTPClient.Do(req)
}

// GetAllTags retrieves all tags from the Paperless-NGX API
func (c *PaperlessClient) GetAllTags(ctx context.Context) (map[string]int, error) {
	tagIDMapping := make(map[string]int)
	path := "api/tags/"

	for path != "" {
		resp, err := c.Do(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("error fetching tags: %d, %s", resp.StatusCode, string(bodyBytes))
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

		// Extract relative path from the Next URL
		if tagsResponse.Next != "" {
			nextURL := tagsResponse.Next
			if strings.HasPrefix(nextURL, c.BaseURL) {
				nextURL = strings.TrimPrefix(nextURL, c.BaseURL+"/")
			}
			path = nextURL
		} else {
			path = ""
		}
	}

	return tagIDMapping, nil
}

// GetDocumentsByTags retrieves documents that match the specified tags
func (c *PaperlessClient) GetDocumentsByTags(ctx context.Context, tags []string) ([]Document, error) {
	tagQueries := make([]string, len(tags))
	for i, tag := range tags {
		tagQueries[i] = fmt.Sprintf("tag:%s", tag)
	}
	searchQuery := strings.Join(tagQueries, " ")
	path := fmt.Sprintf("api/documents/?query=%s", urlEncode(searchQuery))

	resp, err := c.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error searching documents: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	var documentsResponse GetDocumentsApiResponse
	err = json.NewDecoder(resp.Body).Decode(&documentsResponse)
	if err != nil {
		return nil, err
	}

	allTags, err := c.GetAllTags(ctx)
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

// DownloadPDF downloads the PDF file of the specified document
func (c *PaperlessClient) DownloadPDF(ctx context.Context, document Document) ([]byte, error) {
	path := fmt.Sprintf("api/documents/%d/download/", document.ID)
	resp, err := c.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error downloading document %d: %d, %s", document.ID, resp.StatusCode, string(bodyBytes))
	}

	return io.ReadAll(resp.Body)
}

func (c *PaperlessClient) GetDocument(ctx context.Context, documentID int) (Document, error) {
	path := fmt.Sprintf("api/documents/%d/", documentID)
	resp, err := c.Do(ctx, "GET", path, nil)
	if err != nil {
		return Document{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return Document{}, fmt.Errorf("error fetching document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
	}

	var documentResponse GetDocumentApiResponse
	err = json.NewDecoder(resp.Body).Decode(&documentResponse)
	if err != nil {
		return Document{}, err
	}

	allTags, err := c.GetAllTags(ctx)
	if err != nil {
		return Document{}, err
	}

	tagNames := make([]string, len(documentResponse.Tags))
	for i, resultTagID := range documentResponse.Tags {
		for tagName, tagID := range allTags {
			if resultTagID == tagID {
				tagNames[i] = tagName
				break
			}
		}
	}

	return Document{
		ID:      documentResponse.ID,
		Title:   documentResponse.Title,
		Content: documentResponse.Content,
		Tags:    tagNames,
	}, nil
}

// UpdateDocuments updates the specified documents with suggested changes
func (c *PaperlessClient) UpdateDocuments(ctx context.Context, documents []DocumentSuggestion) error {
	// Fetch all available tags
	availableTags, err := c.GetAllTags(ctx)
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
		// remove autoTag to prevent infinite loop (even if it is in the original tags)
		tags = removeTagFromList(tags, autoTag)

		// Map suggested tag names to IDs
		for _, tagName := range tags {
			if tagID, exists := availableTags[tagName]; exists {
				// Skip the tag that we are filtering
				if tagName == manualTag {
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

		// Suggested Content
		suggestedContent := document.SuggestedContent
		if suggestedContent != "" {
			updatedFields["content"] = suggestedContent
		}

		// Marshal updated fields to JSON
		jsonData, err := json.Marshal(updatedFields)
		if err != nil {
			log.Printf("Error marshalling JSON for document %d: %v", documentID, err)
			return err
		}

		// Send the update request using the generic Do method
		path := fmt.Sprintf("api/documents/%d/", documentID)
		resp, err := c.Do(ctx, "PATCH", path, bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("Error updating document %d: %v", documentID, err)
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			log.Printf("Error updating document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
			return fmt.Errorf("error updating document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
		}

		log.Printf("Document %d updated successfully.", documentID)
	}

	return nil
}

// DownloadDocumentAsImages downloads the PDF file of the specified document and converts it to images
func (c *PaperlessClient) DownloadDocumentAsImages(ctx context.Context, documentId int) ([]string, error) {
	// Create a directory named after the document ID
	docDir := filepath.Join(c.GetCacheFolder(), fmt.Sprintf("/document-%d", documentId))
	if _, err := os.Stat(docDir); os.IsNotExist(err) {
		err = os.MkdirAll(docDir, 0755)
		if err != nil {
			return nil, err
		}
	}

	// Check if images already exist
	var imagePaths []string
	for n := 0; ; n++ {
		imagePath := filepath.Join(docDir, fmt.Sprintf("page%03d.jpg", n))
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			break
		}
		imagePaths = append(imagePaths, imagePath)
	}

	// If images exist, return them
	if len(imagePaths) > 0 {
		return imagePaths, nil
	}

	// Proceed with downloading and converting the document to images
	path := fmt.Sprintf("api/documents/%d/download/", documentId)
	resp, err := c.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error downloading document %d: %d, %s", documentId, resp.StatusCode, string(bodyBytes))
	}

	pdfData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	tmpFile, err := os.CreateTemp("", "document-*.pdf")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(pdfData)
	if err != nil {
		return nil, err
	}
	tmpFile.Close()

	doc, err := fitz.New(tmpFile.Name())
	if err != nil {
		return nil, err
	}
	defer doc.Close()

	var mu sync.Mutex
	var g errgroup.Group

	for n := 0; n < doc.NumPage(); n++ {
		n := n // capture loop variable
		g.Go(func() error {
			mu.Lock()
			// I assume the libmupdf library is not thread-safe
			img, err := doc.Image(n)
			mu.Unlock()
			if err != nil {
				return err
			}

			imagePath := filepath.Join(docDir, fmt.Sprintf("page%03d.jpg", n))
			f, err := os.Create(imagePath)
			if err != nil {
				return err
			}

			err = jpeg.Encode(f, img, &jpeg.Options{Quality: jpeg.DefaultQuality})
			if err != nil {
				f.Close()
				return err
			}
			f.Close()

			// Verify the JPEG file
			file, err := os.Open(imagePath)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = jpeg.Decode(file)
			if err != nil {
				return fmt.Errorf("invalid JPEG file: %s", imagePath)
			}

			mu.Lock()
			imagePaths = append(imagePaths, imagePath)
			mu.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return imagePaths, nil
}

// GetCacheFolder returns the cache folder for the PaperlessClient
func (c *PaperlessClient) GetCacheFolder() string {
	if c.CacheFolder == "" {
		c.CacheFolder = filepath.Join(os.TempDir(), "paperless-gpt")
	}
	return c.CacheFolder
}

// urlEncode encodes a string for safe URL usage
func urlEncode(s string) string {
	return strings.ReplaceAll(s, " ", "+")
}
