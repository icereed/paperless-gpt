package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/go-fitz"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

// PaperlessClient struct to interact with the Paperless-NGX API
type PaperlessClient struct {
	BaseURL     string
	APIToken    string
	HTTPClient  *http.Client
	CacheFolder string
}

func hasSameTags(original, suggested []string) bool {
	if len(original) != len(suggested) {
		return false
	}

	// Create copies to avoid modifying original slices
	orig := make([]string, len(original))
	sugg := make([]string, len(suggested))

	copy(orig, original)
	copy(sugg, suggested)

	// Sort both slices
	sort.Strings(orig)
	sort.Strings(sugg)

	// Compare elements
	for i := range orig {
		if orig[i] != sugg[i] {
			return false
		}
	}

	return true
}

// NewPaperlessClient creates a new instance of PaperlessClient with a default HTTP client
func NewPaperlessClient(baseURL, apiToken string) *PaperlessClient {
	cacheFolder := os.Getenv("PAPERLESS_GPT_CACHE_DIR")

	// Create a custom HTTP transport with TLS configuration
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: paperlessInsecureSkipVerify,
		},
	}
	httpClient := &http.Client{Transport: tr}

	return &PaperlessClient{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		APIToken:    apiToken,
		HTTPClient:  httpClient,
		CacheFolder: cacheFolder,
	}
}

// Do method to make requests to the Paperless-NGX API
func (client *PaperlessClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := fmt.Sprintf("%s/%s", client.BaseURL, strings.TrimLeft(path, "/"))
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Token %s", client.APIToken))

	// Set Content-Type if body is present
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	log.WithFields(logrus.Fields{
		"method": method,
		"url":    url,
	}).Debug("Making HTTP request")

	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		log.WithError(err).WithFields(logrus.Fields{
			"url":    url,
			"method": method,
			"error":  err,
		}).Error("HTTP request failed")
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	// Check if response is HTML instead of JSON for API endpoints
	if strings.HasPrefix(path, "api/") {
		contentType := resp.Header.Get("Content-Type")
		if strings.Contains(contentType, "text/html") {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			// Create a new response with the same body for the caller
			resp = &http.Response{
				Status:     resp.Status,
				StatusCode: resp.StatusCode,
				Header:     resp.Header,
				Body:       io.NopCloser(bytes.NewBuffer(bodyBytes)),
			}

			log.WithFields(logrus.Fields{
				"url":          url,
				"method":       method,
				"content-type": contentType,
				"status-code":  resp.StatusCode,
				"response":     string(bodyBytes),
				"base-url":     client.BaseURL,
				"request-path": path,
				"full-headers": resp.Header,
			}).Error("Received HTML response for API request")

			return nil, fmt.Errorf("received HTML response instead of JSON (status: %d). This often indicates an SSL/TLS issue or invalid authentication. Check your PAPERLESS_URL, PAPERLESS_TOKEN and PAPERLESS_INSECURE_SKIP_VERIFY settings. Full response: %s", resp.StatusCode, string(bodyBytes))
		}
	}

	return resp, nil
}

// GetAllTags retrieves all tags from the Paperless-NGX API
func (client *PaperlessClient) GetAllTags(ctx context.Context) (map[string]int, error) {
	tagIDMapping := make(map[string]int)
	path := "api/tags/"

	for path != "" {
		resp, err := client.Do(ctx, "GET", path, nil)
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
			if strings.HasPrefix(nextURL, "http") {
				// Extract just the path portion from the full URL
				if parsedURL, err := url.Parse(nextURL); err == nil {
					path = strings.TrimPrefix(parsedURL.Path, "/")
					if parsedURL.RawQuery != "" {
						path += "?" + parsedURL.RawQuery
					}
				} else {
					return nil, fmt.Errorf("failed to parse next URL: %v", err)
				}
			} else {
				path = strings.TrimPrefix(nextURL, "/")
			}
		} else {
			path = ""
		}
	}

	return tagIDMapping, nil
}

// GetDocumentsByTags retrieves documents that match the specified tags
func (client *PaperlessClient) GetDocumentsByTags(ctx context.Context, tags []string, pageSize int) ([]Document, error) {
	tagQueries := make([]string, len(tags))
	for i, tag := range tags {
		tagQueries[i] = fmt.Sprintf("tags__name__iexact=%s", tag)
	}
	searchQuery := strings.Join(tagQueries, "&")
	path := fmt.Sprintf("api/documents/?%s&page_size=%d", urlEncode(searchQuery), pageSize)

	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed in GetDocumentsByTags: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"path":        path,
			"response":    string(bodyBytes),
			"headers":     resp.Header,
		}).Error("Error response from server in GetDocumentsByTags")
		return nil, fmt.Errorf("error searching documents: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	var documentsResponse GetDocumentsApiResponse
	err = json.Unmarshal(bodyBytes, &documentsResponse)
	if err != nil {
		log.WithFields(logrus.Fields{
			"response_body": string(bodyBytes),
			"error":         err,
		}).Error("Failed to parse JSON response in GetDocumentsByTags")
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	allTags, err := client.GetAllTags(ctx)
	if err != nil {
		return nil, err
	}

	allCorrespondents, err := client.GetAllCorrespondents(ctx)
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

		correspondentName := ""
		if result.Correspondent != 0 {
			for name, id := range allCorrespondents {
				if result.Correspondent == id {
					correspondentName = name
					break
				}
			}
		}

		documents = append(documents, Document{
			ID:            result.ID,
			Title:         result.Title,
			Content:       result.Content,
			Correspondent: correspondentName,
			Tags:          tagNames,
			CreatedDate:   result.CreatedDate,
		})
	}

	return documents, nil
}

// DownloadPDF downloads the PDF file of the specified document
func (client *PaperlessClient) DownloadPDF(ctx context.Context, document Document) ([]byte, error) {
	path := fmt.Sprintf("api/documents/%d/download/", document.ID)
	resp, err := client.Do(ctx, "GET", path, nil)
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

func (client *PaperlessClient) GetDocument(ctx context.Context, documentID int) (Document, error) {
	path := fmt.Sprintf("api/documents/%d/", documentID)
	resp, err := client.Do(ctx, "GET", path, nil)
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

	allTags, err := client.GetAllTags(ctx)
	if err != nil {
		return Document{}, err
	}

	allCorrespondents, err := client.GetAllCorrespondents(ctx)
	if err != nil {
		return Document{}, err
	}

	// Match tag IDs to tag names
	tagNames := make([]string, len(documentResponse.Tags))
	for i, resultTagID := range documentResponse.Tags {
		for tagName, tagID := range allTags {
			if resultTagID == tagID {
				tagNames[i] = tagName
				break
			}
		}
	}

	// Match correspondent ID to correspondent name
	correspondentName := ""
	for name, id := range allCorrespondents {
		if documentResponse.Correspondent == id {
			correspondentName = name
			break
		}
	}

	return Document{
		ID:               documentResponse.ID,
		Title:            documentResponse.Title,
		Content:          documentResponse.Content,
		Correspondent:    correspondentName,
		Tags:             tagNames,
		CreatedDate:      documentResponse.CreatedDate,
		OriginalFileName: documentResponse.OriginalFileName,
	}, nil
}

// UpdateDocuments updates the specified documents with suggested changes
func (client *PaperlessClient) UpdateDocuments(ctx context.Context, documents []DocumentSuggestion, db *gorm.DB, isUndo bool) error {
	// Fetch all available tags
	availableTags, err := client.GetAllTags(ctx)
	if err != nil {
		log.Errorf("Error fetching available tags: %v", err)
		return err
	}

	documentsContainSuggestedCorrespondent := false
	for _, document := range documents {
		if document.SuggestedCorrespondent != "" {
			documentsContainSuggestedCorrespondent = true
			break
		}
	}

	availableCorrespondents := make(map[string]int)
	if documentsContainSuggestedCorrespondent {
		availableCorrespondents, err = client.GetAllCorrespondents(ctx)
		if err != nil {
			log.Errorf("Error fetching available correspondents: %v",
				err)
			return err
		}
	}

	for _, document := range documents {
		documentID := document.ID

		//  Original fields will store any updated fields to store records for
		originalFields := make(map[string]interface{})
		updatedFields := make(map[string]interface{})
		newTags := []int{}

		tags := document.SuggestedTags
		originalTags := document.OriginalDocument.Tags

		originalTagsJSON, err := json.Marshal(originalTags)
		if err != nil {
			log.Errorf("Error marshalling JSON for document %d: %v", documentID, err)
			return err
		}

		// remove autoTag to prevent infinite loop (even if it is in the original tags)
		for _, tag := range document.RemoveTags {
			originalTags = removeTagFromList(originalTags, tag)
		}

		if len(tags) == 0 {
			tags = originalTags
		} else {
			// We have suggested tags to change
			originalFields["tags"] = originalTags
			// remove autoTag to prevent infinite loop - this is required in case of undo
			tags = removeTagFromList(tags, autoTag)

			if document.KeepOriginalTags {
				// Keep original tags
				tags = append(tags, originalTags...)
			}

			// remove duplicates
			slices.Sort(tags)
			tags = slices.Compact(tags)
		}

		updatedTagsJSON, err := json.Marshal(tags)
		if err != nil {
			log.Errorf("Error marshalling JSON for document %d: %v", documentID, err)
			return err
		}

		// Map suggested tag names to IDs
		for _, tagName := range tags {
			if tagID, exists := availableTags[tagName]; exists {
				// Skip the tag that we are filtering
				if !isUndo && tagName == manualTag {
					continue
				}
				newTags = append(newTags, tagID)
			} else {
				log.Errorf("Suggested tag '%s' does not exist in paperless-ngx, skipping.", tagName)
			}
		}
		updatedFields["tags"] = newTags

		// Map suggested correspondent names to IDs
		if document.SuggestedCorrespondent != "" {
			if correspondentID, exists := availableCorrespondents[document.SuggestedCorrespondent]; exists {
				updatedFields["correspondent"] = correspondentID
			} else {
				newCorrespondent := instantiateCorrespondent(document.SuggestedCorrespondent)
				newCorrespondentID, err := client.CreateOrGetCorrespondent(context.Background(), newCorrespondent)
				if err != nil {
					log.Errorf("Error creating/getting correspondent with name %s: %v\n", document.SuggestedCorrespondent, err)
					return err
				}
				log.Infof("Using correspondent with name %s and ID %d\n", document.SuggestedCorrespondent, newCorrespondentID)
				updatedFields["correspondent"] = newCorrespondentID
			}
		}

		suggestedTitle := document.SuggestedTitle
		if len(suggestedTitle) > 128 {
			suggestedTitle = suggestedTitle[:128]
		}
		if suggestedTitle != "" {
			originalFields["title"] = document.OriginalDocument.Title
			updatedFields["title"] = suggestedTitle
		} else {
			log.Warnf("No valid title found for document %d, skipping.", documentID)
		}

		// Suggested Content
		suggestedContent := document.SuggestedContent
		if suggestedContent != "" {
			originalFields["content"] = document.OriginalDocument.Content
			updatedFields["content"] = suggestedContent
		}

		// Suggested CreatedDate
		suggestedCreatedDate := document.SuggestedCreatedDate
		if suggestedCreatedDate != "" {
			originalFields["created_date"] = document.OriginalDocument.CreatedDate
			updatedFields["created_date"] = suggestedCreatedDate
		}

		log.Debugf("Document %d: Original fields: %v", documentID, originalFields)
		log.Debugf("Document %d: Updated fields: %v Tags: %v", documentID, updatedFields, tags)

		// Marshal updated fields to JSON
		jsonData, err := json.Marshal(updatedFields)
		if err != nil {
			log.Errorf("Error marshalling JSON for document %d: %v", documentID, err)
			return err
		}

		// Send the update request using the generic Do method
		path := fmt.Sprintf("api/documents/%d/", documentID)
		resp, err := client.Do(ctx, "PATCH", path, bytes.NewBuffer(jsonData))
		if err != nil {
			log.Errorf("Error updating document %d: %v", documentID, err)
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			log.Errorf("Error updating document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
			return fmt.Errorf("error updating document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
		} else {
			for field, value := range originalFields {
				log.Printf("Document %d: Updated %s from %v to %v", documentID, field, value, updatedFields[field])
				// Insert the modification record into the database
				var modificationRecord ModificationHistory
				if field == "tags" {
					// Make sure we only store changes where tags are changed - not the same before and after
					// And we have to use tags, not updatedFields as they are IDs not fields
					if !hasSameTags(document.OriginalDocument.Tags, tags) {
						modificationRecord = ModificationHistory{
							DocumentID:    uint(documentID),
							ModField:      field,
							PreviousValue: string(originalTagsJSON),
							NewValue:      string(updatedTagsJSON),
						}
					}
				} else {
					// Only store mod if field actually changed
					if originalFields[field] != updatedFields[field] {
						modificationRecord = ModificationHistory{
							DocumentID:    uint(documentID),
							ModField:      field,
							PreviousValue: fmt.Sprintf("%v", originalFields[field]),
							NewValue:      fmt.Sprintf("%v", updatedFields[field]),
						}
					}
				}

				// Only store if we have a valid modification record
				if (modificationRecord != ModificationHistory{}) {
					err = InsertModification(db, &modificationRecord)
				}
				if err != nil {
					log.Errorf("Error inserting modification record for document %d: %v", documentID, err)
					return err
				}
			}
		}

		log.Printf("Document %d updated successfully.", documentID)
	}

	return nil
}

// DownloadDocumentAsImages downloads the PDF file of the specified document and converts it to images
// If limitPages > 0, only the first N pages will be processed
// Returns the image paths and the total number of pages in the original document
func (client *PaperlessClient) DownloadDocumentAsImages(ctx context.Context, documentId int, limitPages int) ([]string, int, error) {
	// Create a directory named after the document ID
	docDir := filepath.Join(client.GetCacheFolder(), fmt.Sprintf("document-%d", documentId))
	if _, err := os.Stat(docDir); os.IsNotExist(err) {
		err = os.MkdirAll(docDir, 0755)
		if err != nil {
			return nil, 0, err
		}
	}

	// Proceed with downloading the document to get the total page count
	path := fmt.Sprintf("api/documents/%d/download/", documentId)
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("error downloading document %d: %d, %s", documentId, resp.StatusCode, string(bodyBytes))
	}

	pdfData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	tmpFile, err := os.CreateTemp("", "document-*.pdf")
	if err != nil {
		return nil, 0, err
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(pdfData)
	if err != nil {
		return nil, 0, err
	}
	tmpFile.Close()

	doc, err := fitz.New(tmpFile.Name())
	if err != nil {
		return nil, 0, err
	}
	defer doc.Close()

	totalPages := doc.NumPage()
	pagesToProcess := totalPages

	if limitPages > 0 && limitPages < totalPages {
		pagesToProcess = limitPages
	}

	// Check if images already exist
	var imagePaths []string
	for n := 0; n < pagesToProcess; n++ {
		imagePath := filepath.Join(docDir, fmt.Sprintf("page%03d.jpg", n))
		if _, err := os.Stat(imagePath); err == nil {
			// File exists
			imagePaths = append(imagePaths, imagePath)
		}
	}

	// If all images exist, return them
	if len(imagePaths) == pagesToProcess {
		return imagePaths, totalPages, nil
	}

	// Clear existing images to ensure consistency
	imagePaths = []string{}

	var mu sync.Mutex
	var g errgroup.Group

	for n := 0; n < pagesToProcess; n++ {
		n := n // capture loop variable
		g.Go(func() error {
			// DPI calculation constants
			const minDPI = 72                     // Minimum DPI to ensure readable text
			const maxPixelDimension = 10_000      // Maximum pixels along any side (10,000px)
			const maxTotalPixels = 40_000_000     // Maximum total pixel count (40 megapixels)
			const maxRenderDPI = 600              // Maximum DPI to use when rendering
			const maxFileBytes = 10 * 1024 * 1024 // Maximum JPEG file size (10 MB)

			mu.Lock() // MuPDF is not thread-safe
			rect, err := doc.Bound(n)
			if err != nil {
				mu.Unlock()
				return err
			}

			// Calculate optimal DPI based on page dimensions (in points)
			wPts := float64(rect.Dx())
			hPts := float64(rect.Dy())

			// Calculate DPI limits based on maximum allowed dimension and total pixels
			dpiSide := float64(maxPixelDimension*72) / math.Max(wPts, hPts)
			dpiArea := math.Sqrt(float64(maxTotalPixels) * 72 * 72 / (wPts * hPts))

			// Use the more restrictive of the two limits
			dpi := math.Min(dpiSide, dpiArea)

			// Ensure DPI stays within acceptable bounds
			dpi = math.Min(dpi, float64(maxRenderDPI))
			dpi = math.Max(dpi, float64(minDPI))

			// Render the page at calculated DPI
			var img image.Image
			img, err = doc.ImageDPI(n, dpi)
			mu.Unlock()
			if err != nil {
				return err
			}

			// Encode to buffer first to measure size
			buf := &bytes.Buffer{}
			if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: jpeg.DefaultQuality}); err != nil {
				return err
			}

			// Try moderate quality reduction first to avoid OCR-affecting artifacts
			// More granular steps (85, 80, 75, 70, 65, 60)
			for q := 85; buf.Len() > maxFileBytes && q >= 60; q -= 5 {
				buf.Reset()
				if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: q}); err != nil {
					return err
				}
			}

			// If quality reduction wasn't enough, resize the image as last resort
			if buf.Len() > maxFileBytes {
				// Calculate precise scale factor needed to meet file size target
				scale := math.Sqrt(float64(maxFileBytes) / float64(buf.Len()))

				// Resize image proportionally using high-quality Lanczos algorithm
				img = imaging.Resize(img,
					int(float64(img.Bounds().Dx())*scale),
					int(float64(img.Bounds().Dy())*scale),
					imaging.Lanczos)
				buf.Reset()
				if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: jpeg.DefaultQuality}); err != nil {
					return err
				}
			}

			// Save image to file
			imagePath := filepath.Join(docDir, fmt.Sprintf("page%03d.jpg", n))
			f, err := os.Create(imagePath)
			if err != nil {
				return err
			}

			if _, err := f.Write(buf.Bytes()); err != nil {
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
		return nil, totalPages, err
	}

	// sort the image paths to ensure they are in order
	slices.Sort(imagePaths)

	return imagePaths, totalPages, nil
}

// GetCacheFolder returns the cache folder for the PaperlessClient
func (client *PaperlessClient) GetCacheFolder() string {
	if client.CacheFolder == "" {
		client.CacheFolder = filepath.Join(os.TempDir(), "paperless-gpt")
	}
	return client.CacheFolder
}

// urlEncode encodes a string for safe URL usage
func urlEncode(s string) string {
	return strings.ReplaceAll(s, " ", "+")
}

// instantiateCorrespondent creates a new Correspondent object with default values
func instantiateCorrespondent(name string) Correspondent {
	return Correspondent{
		Name:              name,
		MatchingAlgorithm: 0,
		Match:             "",
		IsInsensitive:     true,
		Owner:             nil,
	}
}

// CreateOrGetCorrespondent creates a new correspondent or returns existing one if name already exists
func (client *PaperlessClient) CreateOrGetCorrespondent(ctx context.Context, correspondent Correspondent) (int, error) {
	// First try to find existing correspondent
	correspondents, err := client.GetAllCorrespondents(ctx)
	if err != nil {
		return 0, fmt.Errorf("error fetching correspondents: %w", err)
	}

	// Check if correspondent already exists
	if id, exists := correspondents[correspondent.Name]; exists {
		log.Infof("Using existing correspondent with name %s and ID %d", correspondent.Name, id)
		return id, nil
	}

	// If not found, create new correspondent
	url := "api/correspondents/"
	jsonData, err := json.Marshal(correspondent)
	if err != nil {
		return 0, err
	}

	resp, err := client.Do(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("error creating correspondent: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	var createdCorrespondent struct {
		ID int `json:"id"`
	}
	err = json.NewDecoder(resp.Body).Decode(&createdCorrespondent)
	if err != nil {
		return 0, err
	}

	return createdCorrespondent.ID, nil
}

// CorrespondentResponse represents the response structure for correspondents
type CorrespondentResponse struct {
	Results []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"results"`
}

// GetAllCorrespondents retrieves all correspondents from the Paperless-NGX API
func (client *PaperlessClient) GetAllCorrespondents(ctx context.Context) (map[string]int, error) {
	correspondentIDMapping := make(map[string]int)
	path := "api/correspondents/?page_size=9999"

	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error fetching correspondents: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	var correspondentsResponse CorrespondentResponse

	err = json.NewDecoder(resp.Body).Decode(&correspondentsResponse)
	if err != nil {
		return nil, err
	}

	for _, correspondent := range correspondentsResponse.Results {
		correspondentIDMapping[correspondent.Name] = correspondent.ID
	}

	return correspondentIDMapping, nil
}

// DeleteDocument deletes a document by its ID
func (client *PaperlessClient) DeleteDocument(ctx context.Context, documentID int) error {
	path := fmt.Sprintf("api/documents/%d/", documentID)
	resp, err := client.Do(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error deleting document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// GetTaskStatus checks the status of a document processing task
func (client *PaperlessClient) GetTaskStatus(ctx context.Context, taskID string) (map[string]interface{}, error) {
	path := fmt.Sprintf("api/tasks/?task_id=%s", taskID)
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error checking task status: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("error parsing response: %w", err)
	}

	return result, nil
}

// CreateTag creates a new tag and returns its ID
func (client *PaperlessClient) CreateTag(ctx context.Context, tagName string) (int, error) {
	type tagRequest struct {
		Name string `json:"name"`
	}

	requestBody, err := json.Marshal(tagRequest{Name: tagName})
	if err != nil {
		return 0, fmt.Errorf("error marshaling tag request: %w", err)
	}

	resp, err := client.Do(ctx, "POST", "api/tags/", bytes.NewBuffer(requestBody))
	if err != nil {
		return 0, fmt.Errorf("error creating tag: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("error creating tag: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	var createdTag struct {
		ID int `json:"id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&createdTag); err != nil {
		return 0, fmt.Errorf("error parsing created tag response: %w", err)
	}

	return createdTag.ID, nil
}

// UploadDocument uploads a document to paperless-ngx
func (client *PaperlessClient) UploadDocument(ctx context.Context, pdfData []byte, filename string, metadata map[string]interface{}) (string, error) {
	// Create a new multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add the document file part
	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return "", fmt.Errorf("error creating form file: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(pdfData)); err != nil {
		return "", fmt.Errorf("error copying file data: %w", err)
	}

	// Add metadata fields
	for key, value := range metadata {
		if value == nil {
			continue
		}

		var strValue string
		switch v := value.(type) {
		case string:
			strValue = v
		case int:
			strValue = strconv.Itoa(v)
		case []int:
			for _, tagID := range v {
				if err := writer.WriteField("tags", strconv.Itoa(tagID)); err != nil {
					return "", fmt.Errorf("error adding tag ID: %w", err)
				}
			}
			continue
		default:
			continue
		}

		if err := writer.WriteField(key, strValue); err != nil {
			return "", fmt.Errorf("error adding metadata field %s: %w", key, err)
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("error closing multipart writer: %w", err)
	}

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/documents/post_document/", client.BaseURL), body)
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", fmt.Sprintf("Token %s", client.APIToken))

	// Send the request
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("error uploading document: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	// Read the response directly as a string (per Swagger docs)
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response: %w", err)
	}

	// Trim any whitespace and quotes
	taskID := strings.Trim(strings.TrimSpace(string(bodyBytes)), "\"")
	if taskID == "" {
		return "", fmt.Errorf("empty task ID returned")
	}

	log.Infof("Successfully uploaded document, received task ID: %s", taskID)
	return taskID, nil
}
