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
	"time"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/go-fitz"
	"github.com/pdfcpu/pdfcpu/pkg/api"
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

// CustomField represents a custom field from the Paperless-ngx API
type SelectOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type CustomFieldExtraData struct {
	SelectOptions []SelectOption `json:"select_options"`
}

type CustomField struct {
	ID        int                   `json:"id"`
	Name      string                `json:"name"`
	DataType  string                `json:"data_type"`
	ExtraData *CustomFieldExtraData `json:"extra_data"`
}

// DocumentType represents a document type from the Paperless-ngx API
type DocumentType struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
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

// GetDocumentCountByTag checks if there is a document for the specified tag (it is much faster than api/documents/)
func (client *PaperlessClient) GetDocumentCountByTag(ctx context.Context, tag string) (int, error) {
	path := fmt.Sprintf("api/tags/?name__iexact=%s", url.QueryEscape(tag))

	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("error fetching tags: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	var tagsResponse struct {
		Results []struct {
			DocumentCount int `json:"document_count"`
		} `json:"results"`
		Count int `json:"count"`
	}

	err = json.NewDecoder(resp.Body).Decode(&tagsResponse)
	if err != nil {
		return 0, err
	}

	if tagsResponse.Count == 0 {
		return 0, nil
	}

	return tagsResponse.Results[0].DocumentCount, nil
}

// GetDocumentsByTag retrieves documents that match the specified tag
func (client *PaperlessClient) GetDocumentsByTag(ctx context.Context, tag string, pageSize int) ([]Document, error) {
	documentCount, err := client.GetDocumentCountByTag(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("error checking document count for tag %s: %w", tag, err)
	}
	if documentCount == 0 {
		return []Document{}, nil
	}

	path := fmt.Sprintf("api/documents/?tags__name__iexact=%s&page_size=%d", url.QueryEscape(tag), pageSize)

	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed in GetDocumentsByTag: %w", err)
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
		}).Error("Error response from server in GetDocumentsByTag")
		return nil, fmt.Errorf("error searching documents: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	var documentsResponse GetDocumentsApiResponse
	err = json.Unmarshal(bodyBytes, &documentsResponse)
	if err != nil {
		log.WithFields(logrus.Fields{
			"response_body": string(bodyBytes),
			"error":         err,
		}).Error("Failed to parse JSON response in GetDocumentsByTag")
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
	// TODO: This function can be optimized by caching the results of GetAllTags, GetAllCorrespondents, and GetCustomFields.
	// A simple time-based cache could be implemented in the PaperlessClient to avoid fetching this data on every call.
	path := fmt.Sprintf("api/documents/%d/?full_perms=true", documentID)
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

	allCustomFields, err := client.GetCustomFields(ctx)
	if err != nil {
		return Document{}, err
	}
	customFieldMap := make(map[int]string)
	for _, field := range allCustomFields {
		customFieldMap[field.ID] = field.Name
	}

	for i, cf := range documentResponse.CustomFields {
		if name, ok := customFieldMap[cf.Field]; ok {
			documentResponse.CustomFields[i].Name = name
		}
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

	// Get all document types to find the name
	allDocumentTypes, err := client.GetAllDocumentTypes(ctx)
	if err != nil {
		return Document{}, err
	}
	documentTypeName := ""
	for _, docType := range allDocumentTypes {
		if documentResponse.DocumentType == docType.ID {
			documentTypeName = docType.Name
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
		CustomFields:     documentResponse.CustomFields,
		DocumentTypeName: documentTypeName,
		DocumentType:     documentResponse.DocumentType,
		Owner:            documentResponse.Owner,
		Permissions:      documentResponse.Permissions,
	}, nil
}

// PatchDocument patches a document with the given fields
func (client *PaperlessClient) PatchDocument(ctx context.Context, documentID int, fields map[string]interface{}) error {
	jsonData, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("error marshalling JSON: %w", err)
	}

	path := fmt.Sprintf("api/documents/%d/", documentID)
	resp, err := client.Do(ctx, "PATCH", path, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error patching document %d: %w", documentID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error patching document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// UpdateDocuments updates the specified documents with suggested changes
func (client *PaperlessClient) UpdateDocuments(ctx context.Context, documents []DocumentSuggestion, db *gorm.DB, isUndo bool) error {
	availableTags, err := client.GetAllTags(ctx)
	if err != nil {
		return fmt.Errorf("error fetching available tags: %w", err)
	}

	availableCorrespondents, err := client.GetAllCorrespondents(ctx)
	if err != nil {
		return fmt.Errorf("error fetching available correspondents: %w", err)
	}

	// Build document type name -> ID map
	documentTypes, err := client.GetAllDocumentTypes(ctx)
	if err != nil {
		return fmt.Errorf("error fetching available document types: %w", err)
	}
	availableDocumentTypes := make(map[string]int)
	for _, dt := range documentTypes {
		availableDocumentTypes[dt.Name] = dt.ID
	}

	// Build a field-id -> data_type map once per call, lazily — only fetch
	// when at least one document has suggested custom fields, so users who
	// don't use the feature pay nothing. Used to normalize monetary values
	// at the API boundary so paperless-ngx's validator accepts them
	// (see normalize_monetary.go).
	var customFieldTypes map[int]string
	for _, d := range documents {
		if len(d.SuggestedCustomFields) > 0 {
			allFields, err := client.GetCustomFields(ctx)
			if err != nil {
				return fmt.Errorf("error fetching custom fields for normalization: %w", err)
			}
			customFieldTypes = make(map[int]string, len(allFields))
			for _, f := range allFields {
				customFieldTypes[f.ID] = f.DataType
			}
			break
		}
	}

	// firstPartial captures the first document in the batch whose update only
	// succeeded after paperless-gpt had to drop some fields rejected by
	// paperless-ngx. If non-nil at the end of the function, the caller sees a
	// PartialUpdateError and applies the fail tag.
	// Single-document calls (which are the normal usage from background.go)
	// only ever see the result for that one document; multi-document calls
	// report on the first one that had drops, matching the existing
	// stop-on-first-failure semantics for hard errors.
	var firstPartial *PartialUpdateError

	for _, document := range documents {
		documentID := document.ID
		originalDoc := document.OriginalDocument
		updatedFields := make(map[string]interface{})
		originalFields := make(map[string]interface{})
		var partialDroppedFields []string

		// --- TAGS ---
		finalTagNames := originalDoc.Tags
		if len(document.SuggestedTags) > 0 {
			if document.KeepOriginalTags {
				finalTagNames = append(finalTagNames, document.SuggestedTags...)
			} else {
				finalTagNames = document.SuggestedTags
			}
		}
		var cleanedTags []string
		for _, tagName := range finalTagNames {
			isRemoved := false
			for _, tagToRemove := range document.RemoveTags {
				if strings.EqualFold(tagName, tagToRemove) {
					isRemoved = true
					break
				}
			}
			if !isRemoved {
				cleanedTags = append(cleanedTags, tagName)
			}
		}
		finalTagNames = cleanedTags

		slices.Sort(finalTagNames)
		finalTagNames = slices.Compact(finalTagNames)

		log.Debugf("Document %d: Final tag names after compacting: %v", documentID, finalTagNames)

		// NOTE: this will dump the OCR complete tag if it doesn't exist in paperless-ngx
		if !hasSameTags(originalDoc.Tags, finalTagNames) {
			var finalTagIDs []int
			for _, tagName := range finalTagNames {
				if tagID, exists := availableTags[tagName]; exists {
					finalTagIDs = append(finalTagIDs, tagID)
				} else if createNewTags {
					// Create the new tag in paperless-ngx
					newTagID, err := client.CreateTag(ctx, tagName)
					if err != nil {
						log.Warnf("Document %d: Failed to create new tag '%s': %v", documentID, tagName, err)
						continue
					}
					log.Infof("Document %d: Created new tag '%s' with ID %d", documentID, tagName, newTagID)
					availableTags[tagName] = newTagID
					finalTagIDs = append(finalTagIDs, newTagID)
				}
			}
			// Only update tags if there are remaining tags after changes
			// Sending an empty tags array causes Paperless-NGX to return an error
			// However, we need to track this for a potential second update
			if len(finalTagIDs) > 0 {
				originalFields["tags"] = originalDoc.Tags
				updatedFields["tags"] = finalTagIDs
			} else {
				// Mark that we need to remove tags but can't do it in this update
				// We'll handle this after the main update completes
				originalFields["tags"] = originalDoc.Tags
			}
		}

		// --- CORRESPONDENT ---
		if document.SuggestedCorrespondent != "" && document.SuggestedCorrespondent != originalDoc.Correspondent {
			originalFields["correspondent"] = originalDoc.Correspondent
			if corrID, exists := availableCorrespondents[document.SuggestedCorrespondent]; exists {
				updatedFields["correspondent"] = corrID
			} else {
				newCorr := instantiateCorrespondent(document.SuggestedCorrespondent)
				newCorrID, err := client.CreateOrGetCorrespondent(ctx, newCorr)
				if err != nil {
					return fmt.Errorf("error creating correspondent '%s': %w", document.SuggestedCorrespondent, err)
				}
				updatedFields["correspondent"] = newCorrID
			}
		}

		// --- DOCUMENT TYPE ---
		if document.SuggestedDocumentType != "" && document.SuggestedDocumentType != originalDoc.DocumentTypeName {
			originalFields["document_type"] = originalDoc.DocumentTypeName
			if docTypeID, exists := availableDocumentTypes[document.SuggestedDocumentType]; exists {
				updatedFields["document_type"] = docTypeID
			} else {
				// Unlike correspondents, we don't create new document types - only use existing ones
				log.Warnf("Document type '%s' not found in available types, skipping for document %d", document.SuggestedDocumentType, documentID)
			}
		}

		// --- TITLE ---
		suggestedTitle := document.SuggestedTitle
		if len(suggestedTitle) > 128 {
			suggestedTitle = suggestedTitle[:128]
		}
		if suggestedTitle != "" && suggestedTitle != originalDoc.Title {
			originalFields["title"] = originalDoc.Title
			updatedFields["title"] = suggestedTitle
		}

		// --- CREATED DATE ---
		suggestedCreatedDate := document.SuggestedCreatedDate
		if suggestedCreatedDate != "" {
			// Validate that the date is a real Gregorian calendar date, not just
			// a correctly-formatted string. time.Parse rejects impossible dates
			// like "2023-01-79" (day 79) that the regex `^\d{4}-\d{2}-\d{2}$`
			// would accept. Dropped pre-flight rather than after a 400 to avoid
			// an unnecessary round-trip, but the effect on the user is the same:
			// the field is skipped and the fail tag is applied.
			if _, err := time.Parse("2006-01-02", suggestedCreatedDate); err == nil {
				originalFields["created_date"] = document.OriginalDocument.CreatedDate
				updatedFields["created_date"] = suggestedCreatedDate
			} else {
				log.Warnf("Document %d: created_date %q is not a valid calendar date, skipping. (%v)", documentID, suggestedCreatedDate, err)
				partialDroppedFields = append(partialDroppedFields, "created_date")
			}
		}

		// --- CONTENT ---
		if document.SuggestedContent != "" && document.SuggestedContent != originalDoc.Content {
			originalFields["content"] = originalDoc.Content
			updatedFields["content"] = document.SuggestedContent
		}

		// --- CUSTOM FIELDS ---
		if len(document.SuggestedCustomFields) > 0 {
			log.Infof("Processing custom fields for document %d with mode: '%s'", documentID, document.CustomFieldsWriteMode)
			finalCustomFields := slices.Clone(originalDoc.CustomFields)
			originalCustomFieldsJSON, _ := json.Marshal(originalDoc.CustomFields)

			switch document.CustomFieldsWriteMode {
			case "replace":
				finalCustomFields = []CustomFieldResponse{}
				for _, sf := range document.SuggestedCustomFields {
					value := normalizeCustomFieldValue(customFieldTypes[sf.ID], sf.Value)
					finalCustomFields = append(finalCustomFields, CustomFieldResponse{Field: sf.ID, Value: value})
				}
			case "update":
				existingFieldsMap := make(map[int]*CustomFieldResponse)
				for i := range finalCustomFields {
					existingFieldsMap[finalCustomFields[i].Field] = &finalCustomFields[i]
				}
				for _, sf := range document.SuggestedCustomFields {
					value := normalizeCustomFieldValue(customFieldTypes[sf.ID], sf.Value)
					if ef, ok := existingFieldsMap[sf.ID]; ok {
						ef.Value = value
					} else {
						finalCustomFields = append(finalCustomFields, CustomFieldResponse{Field: sf.ID, Value: value})
					}
				}
			case "append":
				existingFieldsMap := make(map[int]bool)
				for _, f := range finalCustomFields {
					existingFieldsMap[f.Field] = true
				}
				for _, sf := range document.SuggestedCustomFields {
					if _, exists := existingFieldsMap[sf.ID]; !exists {
						value := normalizeCustomFieldValue(customFieldTypes[sf.ID], sf.Value)
						finalCustomFields = append(finalCustomFields, CustomFieldResponse{Field: sf.ID, Value: value})
					}
				}
			}

			finalCustomFieldsJSON, _ := json.Marshal(finalCustomFields)
			if string(originalCustomFieldsJSON) != string(finalCustomFieldsJSON) {
				originalFields["custom_fields"] = string(originalCustomFieldsJSON)
				updatedFields["custom_fields"] = finalCustomFields
			}
		}

		if len(updatedFields) == 0 {
			log.Infof("No fields to update for document %d.", documentID)
			// Still need to remove the auto-tag if it exists
			if slices.Contains(originalDoc.Tags, autoTag) || slices.Contains(originalDoc.Tags, manualTag) || slices.Contains(originalDoc.Tags, autoOcrTag) {
				var finalTagIDs []int
				for _, tagName := range originalDoc.Tags {
					if !strings.EqualFold(tagName, autoTag) && !strings.EqualFold(tagName, manualTag) && !strings.EqualFold(tagName, autoOcrTag) {
						if tagID, exists := availableTags[tagName]; exists {
							finalTagIDs = append(finalTagIDs, tagID)
						}
					}
				}
				// Mark that we need to remove tags
				// We'll send the tag update directly (even if empty) since there are no other field changes
				originalFields["tags"] = originalDoc.Tags
				if len(finalTagIDs) > 0 {
					updatedFields["tags"] = finalTagIDs
				} else {
					// Document only had auto/manual tags with no other changes
					// We need to send an empty tags array to remove the manual tag
					log.Infof("Document %d: Removing manual/auto tag (only tag present, no other changes)", documentID)
					updatedFields["tags"] = []int{}
				}
			} else {
				continue
			}
		}

		log.Debugf("Document %d: Fields to update: %v", documentID, updatedFields)

		// PATCH with strip-and-retry: if paperless-ngx rejects the update with
		// a 400 (e.g. an LLM-suggested value that fails server-side
		// validation), parse the response to identify which fields paperless
		// rejected, remove them from updatedFields, and retry. This salvages
		// the valid fields the LLM suggested instead of discarding the entire
		// update.
		//
		// In practice paperless-ngx reports all validation errors in a single
		// 400 response, so one retry is normally sufficient. The retry cap
		// guards against pathological cases (e.g. cascading validation).
		// If after all retries we still cannot get a 200, the caller's
		// recoverFromFailedUpdate handles the final loop-break (remove auto
		// tag, add fail tag).
		const maxRetries = 3
		patchPath := fmt.Sprintf("api/documents/%d/", documentID)
		patchSucceeded := false

		for attempt := 0; attempt <= maxRetries; attempt++ {
			if len(updatedFields) == 0 {
				// All fields stripped — nothing left to send.
				return fmt.Errorf("error updating document %d: paperless-ngx rejected every field that was suggested (dropped on previous attempts: %v)", documentID, partialDroppedFields)
			}

			jsonData, err := json.Marshal(updatedFields)
			if err != nil {
				return fmt.Errorf("error marshalling JSON for document %d: %w", documentID, err)
			}

			resp, err := client.Do(ctx, "PATCH", patchPath, bytes.NewBuffer(jsonData))
			if err != nil {
				return fmt.Errorf("error updating document %d: %w", documentID, err)
			}
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				patchSucceeded = true
				break
			}

			if resp.StatusCode != http.StatusBadRequest {
				return fmt.Errorf("error updating document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
			}

			// 400 — try to identify the failing fields so we can drop them and retry.
			scalarFails, cfIdxFails, unrecoverable := parsePaperlessValidationErrors(bodyBytes)
			if unrecoverable || (len(scalarFails) == 0 && len(cfIdxFails) == 0) {
				return fmt.Errorf("error updating document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
			}

			newlyDropped := stripFailedFields(updatedFields, scalarFails, cfIdxFails)
			if len(newlyDropped) == 0 {
				// Paperless reported errors but they don't match anything in our
				// current payload — defensive guard against parser/format drift.
				return fmt.Errorf("error updating document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
			}

			partialDroppedFields = append(partialDroppedFields, newlyDropped...)
			log.Warnf("Document %d: paperless-ngx rejected fields %v on attempt %d/%d; retrying without them. Raw response: %s", documentID, newlyDropped, attempt+1, maxRetries+1, string(bodyBytes))
		}

		if !patchSucceeded {
			return fmt.Errorf("error updating document %d: paperless-ngx still rejected the update after %d retries (fields dropped so far: %v)", documentID, maxRetries, partialDroppedFields)
		}

		if len(partialDroppedFields) > 0 && firstPartial == nil {
			firstPartial = &PartialUpdateError{
				DocumentID:    documentID,
				DroppedFields: partialDroppedFields,
			}
		}

		// Check if we need to remove auto/manual tags in a separate update
		// This happens when tags changed but resulted in an empty array (which we can't send)
		if _, hadTagChange := originalFields["tags"]; hadTagChange {
			if _, tagsSent := updatedFields["tags"]; !tagsSent {
				// We detected a tag change but didn't send it because it would be empty
				// Now we need to remove the auto/manual tag using the bulk edit API
				log.Infof("Document %d: Removing auto/manual tag in separate update", documentID)

				// Get the current document state to see what tags it has now
				currentDoc, err := client.GetDocument(ctx, documentID)
				if err != nil {
					log.Warnf("Failed to get current document state for tag removal: %v", err)
				} else {
					// Remove auto/manual tags from current tags
					var remainingTagIDs []int
					var remainingTagNames []string
					for _, tagName := range currentDoc.Tags {
						if !strings.EqualFold(tagName, autoTag) && !strings.EqualFold(tagName, manualTag) && !strings.EqualFold(tagName, autoOcrTag) {
							if tagID, exists := availableTags[tagName]; exists {
								remainingTagIDs = append(remainingTagIDs, tagID)
								remainingTagNames = append(remainingTagNames, tagName)
							}
						}
					}

					// Always send the tag update to remove auto/manual tags, even if it results in an empty array
					// This is required - the auto/manual tag MUST be removed
					// Ensure we send an empty array [] instead of null
					if remainingTagIDs == nil {
						remainingTagIDs = []int{}
					}
					tagUpdateFields := map[string]interface{}{
						"tags": remainingTagIDs,
					}
					tagJsonData, err := json.Marshal(tagUpdateFields)
					if err == nil {
						tagPath := fmt.Sprintf("api/documents/%d/", documentID)
						tagResp, err := client.Do(ctx, "PATCH", tagPath, bytes.NewBuffer(tagJsonData))
						if err == nil {
							defer tagResp.Body.Close()
							if tagResp.StatusCode == http.StatusOK {
								log.Infof("Document %d: Successfully removed auto/manual tag", documentID)
								// Record this tag change with tag names for both PreviousValue and NewValue
								mod := ModificationHistory{
									DocumentID:    uint(documentID),
									ModField:      "tags",
									PreviousValue: fmt.Sprintf("%v", originalDoc.Tags),
									NewValue:      fmt.Sprintf("%v", remainingTagNames),
								}
								if err := InsertModification(db, &mod); err != nil {
									log.Warnf("Error inserting tag modification record: %v", err)
								}
							} else {
								bodyBytes, _ := io.ReadAll(tagResp.Body)
								log.Warnf("Failed to remove auto/manual tag: %d, %s", tagResp.StatusCode, string(bodyBytes))
							}
						}
					}
				}
			}
		}

		for field, value := range originalFields {
			// Skip tags if we handled it separately above
			if field == "tags" {
				if _, tagsSent := updatedFields["tags"]; !tagsSent {
					continue // Already handled in separate update above
				}
			}
			log.Printf("Document %d: Updated %s from %v to %v", documentID, field, value, updatedFields[field])
			mod := ModificationHistory{
				DocumentID:    uint(documentID),
				ModField:      field,
				PreviousValue: fmt.Sprintf("%v", value),
				NewValue:      fmt.Sprintf("%v", updatedFields[field]),
			}
			if err := InsertModification(db, &mod); err != nil {
				return fmt.Errorf("error inserting modification record for document %d: %w", documentID, err)
			}
		}
		log.Printf("Document %d updated successfully.", documentID)
	}
	if firstPartial != nil {
		return firstPartial
	}
	return nil
}

// parsePaperlessValidationErrors interprets paperless-ngx's 400 response body
// and classifies the validation errors into three buckets:
//   - scalarFields: top-level fields (other than custom_fields and tags) that
//     paperless rejected. These can be removed from the PATCH and retried.
//   - customFieldIndices: indices into the custom_fields array whose entries
//     paperless rejected. These entries can be removed and retried.
//   - unrecoverable: true if the response contains errors paperless-gpt cannot
//     safely drop (currently: any failure that references the "tags" field,
//     because the tag update is what breaks the auto-processing loop). The
//     caller must treat this as a hard failure.
//
// Returns (nil, nil, false) if the body cannot be parsed as the expected
// shape; the caller treats this as a hard failure too.
//
// Example paperless-ngx 400 body this parser handles:
//
//	{"created_date":["Date has wrong format..."],
//	 "custom_fields":[{},{},{},{},{},{"non_field_errors":["..."]},{},{}]}
func parsePaperlessValidationErrors(body []byte) (scalarFields map[string]bool, customFieldIndices []int, unrecoverable bool) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, false
	}
	scalarFields = make(map[string]bool)
	for key, val := range raw {
		switch key {
		case "tags":
			// Tag updates are load-bearing for the loop-break behaviour; we
			// must not drop them silently. Treat as unrecoverable so the
			// caller falls back to recoverFromFailedUpdate.
			unrecoverable = true
			return
		case "custom_fields":
			arr, ok := val.([]any)
			if !ok {
				// Unexpected shape — bail to hard-failure path.
				unrecoverable = true
				return
			}
			for i, entry := range arr {
				obj, ok := entry.(map[string]any)
				if !ok || len(obj) == 0 {
					continue
				}
				customFieldIndices = append(customFieldIndices, i)
			}
		default:
			scalarFields[key] = true
		}
	}
	if len(scalarFields) == 0 && len(customFieldIndices) == 0 {
		return nil, nil, false
	}
	return scalarFields, customFieldIndices, false
}

// stripFailedFields removes the fields named in scalarFields and the
// custom_fields entries at the given indices from updatedFields, in-place.
// Returns human-readable names of fields actually stripped, for logging.
func stripFailedFields(updatedFields map[string]interface{}, scalarFields map[string]bool, customFieldIndices []int) []string {
	var dropped []string
	for field := range scalarFields {
		if _, present := updatedFields[field]; present {
			delete(updatedFields, field)
			dropped = append(dropped, field)
		}
	}
	if len(customFieldIndices) > 0 {
		if cf, ok := updatedFields["custom_fields"].([]CustomFieldResponse); ok {
			// Remove in descending index order so later indices remain valid
			// as we splice the slice.
			sortedIdx := slices.Clone(customFieldIndices)
			sort.Sort(sort.Reverse(sort.IntSlice(sortedIdx)))
			for _, idx := range sortedIdx {
				if idx < 0 || idx >= len(cf) {
					continue
				}
				dropped = append(dropped, fmt.Sprintf("custom_fields[%d](field_id=%d)", idx, cf[idx].Field))
				cf = append(cf[:idx], cf[idx+1:]...)
			}
			if len(cf) == 0 {
				delete(updatedFields, "custom_fields")
			} else {
				updatedFields["custom_fields"] = cf
			}
		}
	}
	return dropped
}

// DownloadDocumentAsImages downloads the PDF file of the specified document and converts it to images
// If limitPages > 0, only the first N pages will be processed
// Returns the image paths and the total number of pages in the original document
func (client *PaperlessClient) DownloadDocumentAsImages(ctx context.Context, documentID int, limitPages int) ([]string, int, error) {
	// Create a directory named after the document ID
	docDir := filepath.Join(client.GetCacheFolder(), fmt.Sprintf("document-%d", documentID))
	if _, err := os.Stat(docDir); os.IsNotExist(err) {
		err = os.MkdirAll(docDir, 0755)
		if err != nil {
			return nil, 0, err
		}
	}

	// Proceed with downloading the document to get the total page count
	path := fmt.Sprintf("api/documents/%d/download/", documentID)
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("error downloading document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
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
			const minDPI = 72 // Minimum DPI to ensure readable text

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
			dpiSide := float64(imageMaxPixelDimension*72) / math.Max(wPts, hPts)
			dpiArea := math.Sqrt(float64(imageMaxTotalPixels) * 72 * 72 / (wPts * hPts))

			// Use the more restrictive of the two limits
			dpi := math.Min(dpiSide, dpiArea)

			// Ensure DPI stays within acceptable bounds
			dpi = math.Min(dpi, float64(imageMaxRenderDPI))
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
			for q := 85; buf.Len() > imageMaxFileBytes && q >= 60; q -= 5 {
				buf.Reset()
				if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: q}); err != nil {
					return err
				}
			}

			// If quality reduction wasn't enough, resize the image as last resort
			if buf.Len() > imageMaxFileBytes {
				// Calculate precise scale factor needed to meet file size target
				scale := math.Sqrt(float64(imageMaxFileBytes) / float64(buf.Len()))

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

			log.Infof("Document %d page %d: final image dimensions %dx%d, size %d bytes, DPI %.0f", documentID, n, img.Bounds().Dx(), img.Bounds().Dy(), buf.Len(), dpi)

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

// DownloadDocumentAsPDF downloads the PDF file of the specified document and splits it into individual PDFs if needed
// If limitPages > 0, only the first N pages will be processed
// Returns the PDF paths, original PDF data, and the total number of pages in the original document
func (client *PaperlessClient) DownloadDocumentAsPDF(ctx context.Context, documentID int, limitPages int, split bool) ([]string, []byte, int, error) {
	// Create a directory named after the document ID
	docDir := filepath.Join(client.GetCacheFolder(), fmt.Sprintf("document-%d-pdf", documentID))
	if _, err := os.Stat(docDir); os.IsNotExist(err) {
		err = os.MkdirAll(docDir, 0755)
		if err != nil {
			return nil, nil, 0, err
		}
	}

	// Proceed with downloading the document
	path := fmt.Sprintf("api/documents/%d/download/?original=true", documentID)
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, nil, 0, fmt.Errorf("error downloading document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
	}

	pdfData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, 0, err
	}

	// Save the original PDF
	originalPDFPath := filepath.Join(docDir, "original.pdf")
	err = os.WriteFile(originalPDFPath, pdfData, 0644)
	if err != nil {
		return nil, nil, 0, err
	}

	// Get the number of pages in the PDF
	tmpFile, err := os.CreateTemp("", "document-*.pdf")
	if err != nil {
		return nil, nil, 0, err
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(pdfData)
	if err != nil {
		return nil, nil, 0, err
	}
	tmpFile.Close()

	doc, err := fitz.New(tmpFile.Name())
	if err != nil {
		return nil, nil, 0, err
	}
	defer doc.Close()

	totalPages := doc.NumPage()
	pagesToProcess := totalPages

	if limitPages > 0 && limitPages < totalPages {
		pagesToProcess = limitPages
	}

	// If skipping splitting is requested, return early with empty paths array
	if !split {
		return []string{}, pdfData, totalPages, nil
	}

	// Continue with splitting logic only if we're not skipping it
	// Check if PDFs already exist (check for legacy formats for backward compatibility)
	var pdfPaths []string
	for n := 0; n < pagesToProcess; n++ {
		// Standardized format (preferred): original_001.pdf
		pdfPathStandard := filepath.Join(docDir, fmt.Sprintf("original_%03d.pdf", n+1))
		// Legacy format: original_1.pdf (from older versions or pdfcpu default output)
		pdfPathLegacy := filepath.Join(docDir, fmt.Sprintf("original_%d.pdf", n+1))

		if _, err := os.Stat(pdfPathStandard); err == nil {
			// Standardized format exists
			pdfPaths = append(pdfPaths, pdfPathStandard)
		} else if _, err := os.Stat(pdfPathLegacy); err == nil {
			// Legacy format exists
			pdfPaths = append(pdfPaths, pdfPathLegacy)
		}
	}

	// If all PDFs exist, return them
	if len(pdfPaths) == pagesToProcess {
		return pdfPaths, pdfData, totalPages, nil
	}

	// Clear existing PDFs to ensure consistency when regenerating
	files, err := filepath.Glob(filepath.Join(docDir, "original_*.pdf"))
	if err == nil {
		for _, file := range files {
			os.Remove(file)
		}
	}

	// Use pdfcpu to split the PDF. When a page limit applies, trim the
	// source down to just the pages we need first so an oversized document
	// doesn't cost the same split work regardless of OCR_LIMIT_PAGES - the
	// unlimited split extracted (and wrote to disk) every page up front and
	// only used the first pagesToProcess afterward.
	splitSourcePath := originalPDFPath
	if pagesToProcess < totalPages {
		trimDir, err := os.MkdirTemp("", "pgpt-trim-*")
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating temp dir for page-limited trim: %w", err)
		}
		defer os.RemoveAll(trimDir)

		// Keep the "original.pdf" basename so pdfcpu's split output naming
		// (derived from the input file's basename) still produces
		// original_1.pdf, original_2.pdf, ... in docDir below.
		splitSourcePath = filepath.Join(trimDir, "original.pdf")
		selection := []string{fmt.Sprintf("1-%d", pagesToProcess)}
		if err := api.TrimFile(originalPDFPath, splitSourcePath, selection, nil); err != nil {
			return nil, nil, 0, fmt.Errorf("error trimming PDF to page limit: %w", err)
		}
	}

	err = api.SplitFile(splitSourcePath, docDir, 1, nil)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error splitting PDF: %w", err)
	}

	// pdfcpu creates files with names like "original_1.pdf", "original_2.pdf", etc. (without leading zeros)
	// Rename them to our standardized format (original_001.pdf, original_002.pdf, etc.)
	pdfPaths = []string{}
	for n := 0; n < pagesToProcess; n++ {
		// pdfcpu default output format
		legacyFilePath := filepath.Join(docDir, fmt.Sprintf("original_%d.pdf", n+1))
		// Our standardized format
		standardFilePath := filepath.Join(docDir, fmt.Sprintf("original_%03d.pdf", n+1))

		// Check if the legacy file exists and rename it to standard format
		if _, err := os.Stat(legacyFilePath); err == nil {
			err = os.Rename(legacyFilePath, standardFilePath)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("error renaming PDF to standard format: %w", err)
			}
			pdfPaths = append(pdfPaths, standardFilePath)
		} else {
			return nil, nil, 0, fmt.Errorf("expected split PDF not found: %s", legacyFilePath)
		}
	}

	// Sort the PDF paths to ensure they are in order
	sort.SliceStable(pdfPaths, func(i, j int) bool {
		// Extract the number from the filename (e.g., "original_001.pdf" -> 1, "original_1.pdf" -> 1)
		iBasename := filepath.Base(pdfPaths[i])
		jBasename := filepath.Base(pdfPaths[j])

		iParts := strings.Split(strings.TrimSuffix(iBasename, ".pdf"), "_")
		jParts := strings.Split(strings.TrimSuffix(jBasename, ".pdf"), "_")

		if len(iParts) < 2 || len(jParts) < 2 {
			return pdfPaths[i] < pdfPaths[j] // fallback to string comparison
		}

		// Parse the page numbers (handles both "001" and "1" formats)
		ni, errI := strconv.Atoi(iParts[1])
		nj, errJ := strconv.Atoi(jParts[1])

		if errI != nil || errJ != nil {
			return pdfPaths[i] < pdfPaths[j] // fallback to string comparison
		}

		return ni < nj
	})

	return pdfPaths, pdfData, totalPages, nil
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

// GetAllDocumentTypes retrieves all document types from the Paperless-NGX API
func (client *PaperlessClient) GetAllDocumentTypes(ctx context.Context) ([]DocumentType, error) {
	var allDocumentTypes []DocumentType
	path := "api/document_types/?page_size=1000" // Assuming a reasonable limit

	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error fetching document types: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	var response struct {
		Results []DocumentType `json:"results"`
	}

	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return nil, err
	}

	allDocumentTypes = append(allDocumentTypes, response.Results...)

	return allDocumentTypes, nil
}

// GetCustomFields retrieves all custom fields from the Paperless-NGX API
func (client *PaperlessClient) GetCustomFields(ctx context.Context) ([]CustomField, error) {
	var customFields []CustomField
	path := "api/custom_fields/"

	for path != "" {
		resp, err := client.Do(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("error fetching custom fields: %d, %s", resp.StatusCode, string(bodyBytes))
		}

		var response struct {
			Results []CustomField `json:"results"`
			Next    string        `json:"next"`
		}

		err = json.NewDecoder(resp.Body).Decode(&response)
		if err != nil {
			return nil, err
		}

		customFields = append(customFields, response.Results...)

		if response.Next != "" {
			nextURL, err := url.Parse(response.Next)
			if err != nil {
				return nil, fmt.Errorf("failed to parse next URL for custom fields: %w", err)
			}
			path = nextURL.Path + "?" + nextURL.RawQuery
		} else {
			path = ""
		}
	}

	return customFields, nil
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

// EnsureTagExists checks whether a tag with the given name exists in paperless-ngx
// and creates it if not. Used for paperless-gpt's internal marker tags (e.g. failTag)
// which should always be available regardless of the CREATE_NEW_TAGS setting,
// because they are not LLM-suggested tags but mechanically applied by paperless-gpt itself.
func (client *PaperlessClient) EnsureTagExists(ctx context.Context, tagName string) error {
	if tagName == "" {
		return nil
	}
	tags, err := client.GetAllTags(ctx)
	if err != nil {
		return fmt.Errorf("error fetching tags: %w", err)
	}
	if _, exists := tags[tagName]; exists {
		return nil
	}
	if _, err := client.CreateTag(ctx, tagName); err != nil {
		return fmt.Errorf("error creating tag %q: %w", tagName, err)
	}
	return nil
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
