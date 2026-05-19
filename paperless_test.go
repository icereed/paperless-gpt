package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Helper struct to hold common test data and methods
type testEnv struct {
	t             *testing.T
	server        *httptest.Server
	client        *PaperlessClient
	requestCount  int
	mockResponses map[string]http.HandlerFunc
	db            *gorm.DB
}

// newTestEnv initializes a new test environment
func newTestEnv(t *testing.T) *testEnv {
	env := &testEnv{
		t:             t,
		mockResponses: make(map[string]http.HandlerFunc),
	}

	// Initialize test database
	db, err := InitializeTestDB()
	require.NoError(t, err)
	env.db = db

	// Create a mock server with a handler that dispatches based on URL path
	env.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.requestCount++
		handler, exists := env.mockResponses[r.URL.Path]
		if !exists {
			t.Fatalf("Unexpected request URL: %s", r.URL.Path)
		}
		// Set common headers and invoke the handler
		assert.Equal(t, "Token test-token", r.Header.Get("Authorization"))
		handler(w, r)
	}))

	// Initialize the PaperlessClient with the mock server URL
	env.client = NewPaperlessClient(env.server.URL, "test-token")
	env.client.HTTPClient = env.server.Client()

	// Add mock response for /api/correspondents/
	env.setMockResponse("/api/correspondents/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"results": [{"id": 1, "name": "Alpha"}, {"id": 2, "name": "Beta"}]}`))
	})

	// Add mock response for /api/document_types/
	env.setMockResponse("/api/document_types/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"results": []}`))
	})

	// Add mock response for /api/custom_fields/
	env.setMockResponse("/api/custom_fields/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"results": []}`))
	})

	return env
}

func InitializeTestDB() (*gorm.DB, error) {
	// Use in-memory SQLite for testing
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// Migrate schema
	err = db.AutoMigrate(&ModificationHistory{})
	if err != nil {
		return nil, err
	}

	return db, nil
}

// teardown closes the mock server
func (env *testEnv) teardown() {
	env.server.Close()
}

// Helper method to set a mock response for a specific path
func (env *testEnv) setMockResponse(path string, handler http.HandlerFunc) {
	env.mockResponses[path] = handler
}

// TestNewPaperlessClient tests the creation of a new PaperlessClient instance
func TestNewPaperlessClient(t *testing.T) {
	baseURL := "http://example.com"
	apiToken := "test-token"

	client := NewPaperlessClient(baseURL, apiToken)

	assert.Equal(t, "http://example.com", client.BaseURL)
	assert.Equal(t, apiToken, client.APIToken)
	assert.NotNil(t, client.HTTPClient)
}

// TestDo tests the Do method of PaperlessClient
func TestDo(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	// Set mock response for "/test-path"
	env.setMockResponse("/test-path", func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method
		assert.Equal(t, "GET", r.Method)
		// Send a mock response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "success"}`))
	})

	ctx := context.Background()
	resp, err := env.client.Do(ctx, "GET", "/test-path", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, `{"message": "success"}`, string(body))
}

// TestGetAllTags tests the GetAllTags method, including pagination
func TestGetAllTags(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	// Mock data for paginated responses
	page1 := map[string]interface{}{
		"results": []map[string]interface{}{
			{"id": 1, "name": "tag1"},
			{"id": 2, "name": "tag2"},
		},
		"next": fmt.Sprintf("%s/api/tags/?page=2", env.server.URL),
	}
	page2 := map[string]interface{}{
		"results": []map[string]interface{}{
			{"id": 3, "name": "tag3"},
		},
		"next": nil,
	}

	// Set mock responses for pagination
	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("page")
		if query == "2" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(page2)
		} else {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(page1)
		}
	})

	ctx := context.Background()
	tags, err := env.client.GetAllTags(ctx)
	require.NoError(t, err)

	expectedTags := map[string]int{
		"tag1": 1,
		"tag2": 2,
		"tag3": 3,
	}

	assert.Equal(t, expectedTags, tags)
}

// TestGetDocumentCountByTag tests the GetDocumentCountByTag method
func TestGetDocumentCountByTag(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	// Mock data for paginated responses
	data1 := map[string]interface{}{
		"count": 1,
		"results": []map[string]interface{}{
			{"document_count": 5},
		},
	}

	data2 := map[string]interface{}{
		"count":   0,
		"results": []map[string]interface{}{},
	}

	// Set mock responses for pagination
	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("name__iexact")
		if query == "available" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(data1)
		} else {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(data2)
		}
	})

	ctx := context.Background()
	countAvailable, err := env.client.GetDocumentCountByTag(ctx, "available")
	require.NoError(t, err)
	assert.Equal(t, 5, countAvailable)

	countNotAvailable, err := env.client.GetDocumentCountByTag(ctx, "notavailable")
	require.NoError(t, err)
	assert.Equal(t, 0, countNotAvailable)
}

// TestGetDocumentsByTag tests the GetDocumentsByTag method
func TestGetDocumentsByTag(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	// Mock data for documents
	documentsResponse := GetDocumentsApiResponse{
		Results: []GetDocumentApiResponseResult{
			{
				ID:            1,
				Title:         "Document 1",
				Content:       "Content 1",
				Tags:          []int{1, 2},
				Correspondent: 1,
				CreatedDate:   "1999-09-01",
			},
			{
				ID:            2,
				Title:         "Document 2",
				Content:       "Content 2",
				Tags:          []int{2, 3},
				Correspondent: 2,
				CreatedDate:   "1999-09-02",
			},
		},
	}

	// Mock data for tags
	tagsResponse := map[string]interface{}{
		"results": []map[string]interface{}{
			{"id": 1, "name": "tag1"},
			{"id": 2, "name": "tag2"},
			{"id": 3, "name": "tag3"},
		},
		"next": nil,
	}

	// Mock data for tags
	tagsExactResponse := map[string]interface{}{
		"results": []map[string]interface{}{
			{"document_count": 2},
		},
		"count": 1,
	}

	// Set mock responses
	env.setMockResponse("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		// Verify query parameters
		expectedQuery := "tags__name__iexact=tag2&page_size=25"
		assert.Equal(t, expectedQuery, r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(documentsResponse)
	})

	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		// Handle GetDocumentCountByTag call
		if nameFilter := r.URL.Query().Get("name__iexact"); nameFilter != "" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(tagsExactResponse)
		} else {
			// Handle GetAllTags call
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(tagsResponse)
		}
	})

	ctx := context.Background()
	tag := "tag2"
	documents, err := env.client.GetDocumentsByTag(ctx, tag, 25)
	require.NoError(t, err)

	expectedDocuments := []Document{
		{
			ID:            1,
			Title:         "Document 1",
			Content:       "Content 1",
			Tags:          []string{"tag1", "tag2"},
			Correspondent: "Alpha",
			CreatedDate:   "1999-09-01",
		},
		{
			ID:            2,
			Title:         "Document 2",
			Content:       "Content 2",
			Tags:          []string{"tag2", "tag3"},
			Correspondent: "Beta",
			CreatedDate:   "1999-09-02",
		},
	}

	assert.Equal(t, expectedDocuments, documents)
}

// TestGetDocumentsByTagWithEmoji tests the GetDocumentsByTag method with emoji and special characters
func TestGetDocumentsByTagWithEmoji(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	// Mock data for documents
	documentsResponse := GetDocumentsApiResponse{
		Results: []GetDocumentApiResponseResult{
			{
				ID:            1,
				Title:         "AI Document",
				Content:       "Content about AI",
				Tags:          []int{1},
				Correspondent: 1,
				CreatedDate:   "2024-01-01",
			},
		},
	}

	// Mock data for tags
	tagsResponse := map[string]interface{}{
		"results": []map[string]interface{}{
			{"id": 1, "name": "🤖 AI-Queue"},
		},
		"next": nil,
	}

	// Mock data for exact tag match
	tagsExactResponse := map[string]interface{}{
		"results": []map[string]interface{}{
			{"document_count": 1},
		},
		"count": 1,
	}

	// Set mock responses
	env.setMockResponse("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		// Verify query parameters - the tag should be URL-encoded
		expectedQuery := fmt.Sprintf("tags__name__iexact=%s&page_size=25", url.QueryEscape("🤖 AI-Queue"))
		assert.Equal(t, expectedQuery, r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(documentsResponse)
	})

	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		// Handle GetDocumentCountByTag call
		if nameFilter := r.URL.Query().Get("name__iexact"); nameFilter != "" {
			// Verify the decoded value matches our emoji tag
			assert.Equal(t, "🤖 AI-Queue", nameFilter)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(tagsExactResponse)
		} else {
			// Handle GetAllTags call
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(tagsResponse)
		}
	})

	ctx := context.Background()
	tag := "🤖 AI-Queue"
	documents, err := env.client.GetDocumentsByTag(ctx, tag, 25)
	require.NoError(t, err)

	expectedDocuments := []Document{
		{
			ID:            1,
			Title:         "AI Document",
			Content:       "Content about AI",
			Tags:          []string{"🤖 AI-Queue"},
			Correspondent: "Alpha",
			CreatedDate:   "2024-01-01",
		},
	}

	assert.Equal(t, expectedDocuments, documents)
}

// TestDownloadPDF tests the DownloadPDF method
func TestDownloadPDF(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	document := Document{
		ID: 123,
	}

	// Get sample PDF from tests/pdf/sample.pdf
	pdfFile := "tests/pdf/sample.pdf"
	pdfContent, err := os.ReadFile(pdfFile)
	require.NoError(t, err)

	// Set mock response
	downloadPath := fmt.Sprintf("/api/documents/%d/download/", document.ID)
	env.setMockResponse(downloadPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(pdfContent)
	})

	ctx := context.Background()
	data, err := env.client.DownloadPDF(ctx, document)
	require.NoError(t, err)
	assert.Equal(t, pdfContent, data)
}

// TestUpdateDocuments tests the UpdateDocuments method
func TestUpdateDocuments(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	// Mock data for documents to update
	documents := []DocumentSuggestion{
		{
			ID: 1,
			OriginalDocument: Document{
				ID:          1,
				Title:       "Old Title",
				Tags:        []string{"tag1", "tag3", "manual", "removeMe"},
				CreatedDate: "1999-09-01",
			},
			SuggestedTitle:       "New Title",
			SuggestedTags:        []string{"tag2", "tag3"},
			RemoveTags:           []string{"removeMe"},
			SuggestedCreatedDate: "1999-09-02",
		},
	}
	idTag1 := 1
	idTag2 := 2
	idTag3 := 4
	// Mock data for tags
	tagsResponse := map[string]interface{}{
		"results": []map[string]interface{}{
			{"id": idTag1, "name": "tag1"},
			{"id": idTag2, "name": "tag2"},
			{"id": 3, "name": "manual"},
			{"id": idTag3, "name": "tag3"},
			{"id": 5, "name": "removeMe"},
		},
		"next": nil,
	}

	// Set the manual tag
	manualTag = "manual"

	// Set mock responses
	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(tagsResponse)
	})

	updatePath := fmt.Sprintf("/api/documents/%d/", documents[0].ID)
	env.setMockResponse(updatePath, func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method
		assert.Equal(t, "PATCH", r.Method)

		// Read and parse the request body
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		defer r.Body.Close()

		var updatedFields map[string]interface{}
		err = json.Unmarshal(bodyBytes, &updatedFields)
		require.NoError(t, err)

		// Expected updated fields
		expectedFields := map[string]interface{}{
			"title": "New Title",
			// do not keep previous tags since the tag generation will already take care to include old ones:
			"tags":         []interface{}{float64(idTag2), float64(idTag3)},
			"created_date": "1999-09-02",
		}

		assert.Equal(t, expectedFields, updatedFields)

		w.WriteHeader(http.StatusOK)
	})

	ctx := context.Background()
	err := env.client.UpdateDocuments(ctx, documents, env.db, false)
	require.NoError(t, err)
}

// TestUpdateDocuments_RemovingLastTag tests the behavior when removing the last remaining tag
// from a document, which Paperless-NGX REST API does not allow (empty tags array is rejected).
// The test covers two scenarios:
//  1. Document has only the manual tag with other field changes (title) - should update title first,
//     then remove the manual tag in a separate call
//  2. Document has only the manual tag with NO other changes - should skip the update entirely
func TestUpdateDocuments_RemovingLastTag(t *testing.T) {
	// in this scenario, the manualTag is set, but the
	// document processing sends both the auto and manual
	// versions of the tag to be removed. this is why you'll
	// see the autoTag included in the RemoveTags but not in the original document.
	manualTag = "paperless-gpt"
	autoTag = "paperless-gpt-auto"

	tests := []struct {
		name              string
		document          DocumentSuggestion
		expectUpdateCalls int
		validateCalls     func(t *testing.T, calls []map[string]interface{})
	}{
		{
			name: "with_other_field_changes",
			document: DocumentSuggestion{
				ID: 1,
				OriginalDocument: Document{
					ID:          1,
					Title:       "Old Title",
					Tags:        []string{manualTag},
					CreatedDate: "1999-09-01",
				},
				SuggestedTitle: "New Title",
				SuggestedTags:  []string{},
				RemoveTags:     []string{manualTag, autoTag},
			},
			expectUpdateCalls: 2,
			validateCalls: func(t *testing.T, calls []map[string]interface{}) {
				// First call: should update title but NOT tags
				assert.Equal(t, map[string]interface{}{"title": "New Title"}, calls[0],
					"First call should only update title, not tags")

				// Second call: should remove the manual tag with empty array
				tagsValue, tagsPresent := calls[1]["tags"]
				require.True(t, tagsPresent, "Second call must include tags field")
				tagSlice, ok := tagsValue.([]interface{})
				require.True(t, ok, "tags should be an array")
				assert.Empty(t, tagSlice, "tags array should be empty to remove manual tag")
			},
		},
		{
			name: "no_other_changes",
			document: DocumentSuggestion{
				ID: 2,
				OriginalDocument: Document{
					ID:          2,
					Title:       "Same Title",
					Tags:        []string{manualTag},
					CreatedDate: "1999-09-01",
				},
				SuggestedTitle: "",
				SuggestedTags:  []string{},
				RemoveTags:     []string{manualTag, autoTag},
			},
			expectUpdateCalls: 1,
			validateCalls: func(t *testing.T, calls []map[string]interface{}) {
				// Should make one call to remove the manual tag with empty array
				// Even though there are no other field changes, the manual tag MUST be removed
				tagsValue, tagsPresent := calls[0]["tags"]
				require.True(t, tagsPresent, "Must include tags field to remove manual tag")
				tagSlice, ok := tagsValue.([]interface{})
				require.True(t, ok, "tags should be an array")
				assert.Empty(t, tagSlice, "tags array should be empty to remove manual tag")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			defer env.teardown()

			// Mock tags response
			env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"results": []map[string]interface{}{
						{"id": 1, "name": "paperless-gpt"},
					},
					"next": nil,
				})
			})

			// Track update calls (PATCH only, not GET)
			var updateCalls []map[string]interface{}
			updatePath := fmt.Sprintf("/api/documents/%d/", tt.document.ID)

			env.setMockResponse(updatePath, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					// Return document state after first update (still has paperless-gpt tag)
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"id":                 tt.document.ID,
						"title":              "New Title", // Title was updated
						"tags":               []int{1},    // Still has paperless-gpt tag
						"created_date":       tt.document.OriginalDocument.CreatedDate,
						"content":            "",
						"correspondent":      nil,
						"custom_fields":      []interface{}{},
						"original_file_name": "test.pdf",
						"document_type":      nil,
					})
					return
				}

				// Track PATCH calls
				assert.Equal(t, "PATCH", r.Method)
				bodyBytes, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				defer r.Body.Close()

				var updatedFields map[string]interface{}
				err = json.Unmarshal(bodyBytes, &updatedFields)
				require.NoError(t, err)

				updateCalls = append(updateCalls, updatedFields)
				w.WriteHeader(http.StatusOK)
			})

			ctx := context.Background()
			err := env.client.UpdateDocuments(ctx, []DocumentSuggestion{tt.document}, env.db, false)
			require.NoError(t, err)

			assert.Len(t, updateCalls, tt.expectUpdateCalls,
				"Expected %d update calls, got %d", tt.expectUpdateCalls, len(updateCalls))

			if tt.expectUpdateCalls > 0 {
				tt.validateCalls(t, updateCalls)
			}
		})
	}
}

// TestUrlEncode tests the urlEncode function
func TestUrlEncode(t *testing.T) {
	input := "tag:tag1 tag:tag2"
	expected := "tag:tag1+tag:tag2"
	result := urlEncode(input)
	assert.Equal(t, expected, result)
}

// TestDownloadDocumentAsImages tests the DownloadDocumentAsImages method
func TestDownloadDocumentAsImages(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	document := Document{
		ID: 123,
	}

	// Get sample PDF from tests/pdf/sample.pdf
	pdfFile := "tests/pdf/sample.pdf"
	pdfContent, err := os.ReadFile(pdfFile)
	require.NoError(t, err)

	// Set mock response
	downloadPath := fmt.Sprintf("/api/documents/%d/download/", document.ID)
	env.setMockResponse(downloadPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(pdfContent)
	})

	ctx := context.Background()
	imagePaths, totalPages, err := env.client.DownloadDocumentAsImages(ctx, document.ID, 0)
	require.NoError(t, err)

	// Verify that exatly one page was extracted
	assert.Len(t, imagePaths, 1)
	// The path shall end with paperless-gpt/document-123/page000.jpg
	assert.Contains(t, imagePaths[0], "paperless-gpt/document-123/page000.jpg")
	for _, imagePath := range imagePaths {
		_, err := os.Stat(imagePath)
		assert.NoError(t, err)
	}

	// Verify total pages count
	assert.Equal(t, 1, totalPages, "Total pages should be 1")
}

func TestDownloadDocumentAsImages_ManyPages(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	document := Document{
		ID: 321,
	}

	// Get sample PDF from tests/pdf/many-pages.pdf
	pdfFile := "tests/pdf/many-pages.pdf"
	pdfContent, err := os.ReadFile(pdfFile)
	require.NoError(t, err)

	// Set mock response
	downloadPath := fmt.Sprintf("/api/documents/%d/download/", document.ID)
	env.setMockResponse(downloadPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(pdfContent)
	})

	ctx := context.Background()
	env.client.CacheFolder = "tests/tmp"
	// Clean the cache folder
	os.RemoveAll(env.client.CacheFolder)
	imagePaths, totalPages, err := env.client.DownloadDocumentAsImages(ctx, document.ID, 50)
	require.NoError(t, err)

	// Verify that exactly 50 pages were extracted - the original doc contains 52 pages
	assert.Len(t, imagePaths, 50)
	// The path shall end with tests/tmp/document-321/page000.jpg
	for _, imagePath := range imagePaths {
		_, err := os.Stat(imagePath)
		assert.NoError(t, err)
		assert.Contains(t, imagePath, "tests/tmp/document-321/page")
	}

	// Verify total pages count
	assert.Equal(t, 52, totalPages, "Total pages should be 52")
}

// TestDownloadDocumentAsPDF tests the DownloadDocumentAsPDF method
func TestDownloadDocumentAsPDF(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	documentID := 123

	// Get sample PDF from tests/pdf/sample.pdf
	pdfFile := "tests/pdf/sample.pdf"
	pdfContent, err := os.ReadFile(pdfFile)
	require.NoError(t, err)

	// Set mock response
	downloadPath := fmt.Sprintf("/api/documents/%d/download/", documentID)
	env.setMockResponse(downloadPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(pdfContent)
	})

	ctx := context.Background()

	// Test without PDF splitting
	pdfPaths, pdfData, totalPages, err := env.client.DownloadDocumentAsPDF(ctx, documentID, 0, false)
	require.NoError(t, err)
	assert.Empty(t, pdfPaths, "No paths should be returned when split=false")
	assert.Equal(t, pdfContent, pdfData)
	assert.Equal(t, 1, totalPages)

	// Testing with splitting=true would be more complex so we'll skip that for simplicity
}

func TestParsePaperlessValidationErrors(t *testing.T) {
	t.Run("real-world response with created_date + one custom_field", func(t *testing.T) {
		body := []byte(`{"created_date":["Date has wrong format. Use one of these formats instead: YYYY-MM-DD."],"custom_fields":[{},{},{},{},{},{},{},{"non_field_errors":["Date has wrong format. Use one of these formats instead: YYYY-MM-DD."]}]}`)
		scalars, cfIdx, unrecoverable := parsePaperlessValidationErrors(body)
		require.False(t, unrecoverable)
		assert.True(t, scalars["created_date"], "created_date must be reported")
		assert.Equal(t, []int{7}, cfIdx, "custom_fields[7] is the only failing entry")
	})

	t.Run("only custom_field failure", func(t *testing.T) {
		body := []byte(`{"custom_fields":[{},{"non_field_errors":["bad"]},{}]}`)
		scalars, cfIdx, unrecoverable := parsePaperlessValidationErrors(body)
		require.False(t, unrecoverable)
		assert.Empty(t, scalars)
		assert.Equal(t, []int{1}, cfIdx)
	})

	t.Run("only scalar failure", func(t *testing.T) {
		body := []byte(`{"title":["This field may not be blank."]}`)
		scalars, cfIdx, unrecoverable := parsePaperlessValidationErrors(body)
		require.False(t, unrecoverable)
		assert.True(t, scalars["title"])
		assert.Empty(t, cfIdx)
	})

	t.Run("tags failure is unrecoverable", func(t *testing.T) {
		// Tag updates carry the loop-break (auto-tag removal). We must not
		// silently drop them.
		body := []byte(`{"tags":["Invalid pk \"99\" - object does not exist."]}`)
		_, _, unrecoverable := parsePaperlessValidationErrors(body)
		assert.True(t, unrecoverable, "tag errors must be classified as unrecoverable")
	})

	t.Run("garbage body returns nothing-to-strip", func(t *testing.T) {
		scalars, cfIdx, unrecoverable := parsePaperlessValidationErrors([]byte("not json"))
		assert.Nil(t, scalars)
		assert.Nil(t, cfIdx)
		assert.False(t, unrecoverable)
	})

	t.Run("empty response with no errors returns nothing-to-strip", func(t *testing.T) {
		scalars, cfIdx, unrecoverable := parsePaperlessValidationErrors([]byte(`{}`))
		assert.Nil(t, scalars)
		assert.Nil(t, cfIdx)
		assert.False(t, unrecoverable)
	})
}

func TestStripFailedFields(t *testing.T) {
	t.Run("strips scalar field", func(t *testing.T) {
		uf := map[string]interface{}{
			"title":        "BARMER letter",
			"created_date": "2023-01-79",
		}
		dropped := stripFailedFields(uf, map[string]bool{"created_date": true}, nil)
		assert.Equal(t, []string{"created_date"}, dropped)
		_, present := uf["created_date"]
		assert.False(t, present)
		assert.Equal(t, "BARMER letter", uf["title"])
	})

	t.Run("strips custom_fields entries by index, preserves the rest", func(t *testing.T) {
		uf := map[string]interface{}{
			"custom_fields": []CustomFieldResponse{
				{Field: 6, Value: "Adresse"},
				{Field: 7, Value: "2023-01-79"},
				{Field: 8, Value: "M976605823"},
			},
		}
		dropped := stripFailedFields(uf, nil, []int{1})
		require.Len(t, dropped, 1)
		assert.Contains(t, dropped[0], "field_id=7")
		cf := uf["custom_fields"].([]CustomFieldResponse)
		require.Len(t, cf, 2)
		assert.Equal(t, 6, cf[0].Field)
		assert.Equal(t, 8, cf[1].Field, "field 8 must remain after deleting index 1")
	})

	t.Run("strips multiple custom_fields entries (descending order is safe)", func(t *testing.T) {
		uf := map[string]interface{}{
			"custom_fields": []CustomFieldResponse{
				{Field: 1, Value: "a"},
				{Field: 2, Value: "b"},
				{Field: 3, Value: "c"},
				{Field: 4, Value: "d"},
			},
		}
		dropped := stripFailedFields(uf, nil, []int{0, 2})
		require.Len(t, dropped, 2)
		cf := uf["custom_fields"].([]CustomFieldResponse)
		require.Len(t, cf, 2)
		assert.Equal(t, 2, cf[0].Field, "field 2 must remain after deleting indices 0 and 2")
		assert.Equal(t, 4, cf[1].Field, "field 4 must remain after deleting indices 0 and 2")
	})

	t.Run("removes custom_fields key entirely if all entries fail", func(t *testing.T) {
		uf := map[string]interface{}{
			"title": "Letter",
			"custom_fields": []CustomFieldResponse{
				{Field: 7, Value: "2023-01-79"},
			},
		}
		dropped := stripFailedFields(uf, nil, []int{0})
		require.Len(t, dropped, 1)
		_, present := uf["custom_fields"]
		assert.False(t, present, "custom_fields key should be removed when empty")
		assert.Equal(t, "Letter", uf["title"])
	})

	t.Run("ignores out-of-range custom_field indices", func(t *testing.T) {
		uf := map[string]interface{}{
			"custom_fields": []CustomFieldResponse{{Field: 1, Value: "a"}},
		}
		dropped := stripFailedFields(uf, nil, []int{5})
		assert.Empty(t, dropped)
		cf := uf["custom_fields"].([]CustomFieldResponse)
		assert.Len(t, cf, 1, "original entry must remain when index is out of range")
	})

	t.Run("returns empty when scalar field is not in payload", func(t *testing.T) {
		uf := map[string]interface{}{"title": "x"}
		dropped := stripFailedFields(uf, map[string]bool{"created_date": true}, nil)
		assert.Empty(t, dropped, "must not report a drop for a field that was not in the payload")
		assert.Equal(t, "x", uf["title"])
	})
}

func TestCreatedDatePreValidation(t *testing.T) {
	// Verify that UpdateDocuments rejects impossible dates like "2023-01-79"
	// before sending the PATCH, so the bad date never reaches paperless-ngx.
	// The field should appear in partialDroppedFields so the caller applies
	// the fail tag.
	env := setupTest(t)
	defer env.teardown()

	ctx := context.Background()

	setupTestCase(TestCase{
		name: "pre-validate created_date",
		documents: []TestDocument{
			{ID: 1, Title: "Test Doc", Tags: []string{autoTag}},
		},
	}, env)

	patchCalled := false
	var receivedPatch map[string]interface{}
	env.setMockResponse("/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(GetDocumentApiResponse{
				ID: 1, Title: "Test Doc", Tags: []int{1}, Content: "content",
			})
			return
		}
		if r.Method == "PATCH" {
			patchCalled = true
			json.NewDecoder(r.Body).Decode(&receivedPatch)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 1, "title": "Test Doc", "tags": []int{1},
			})
		}
	})

	suggestion := DocumentSuggestion{
		ID:                   1,
		OriginalDocument:     Document{ID: 1, Title: "Test Doc", Tags: []string{autoTag}},
		SuggestedTitle:       "Better Title",
		SuggestedCreatedDate: "2023-01-79", // impossible date
	}

	err := env.client.UpdateDocuments(ctx, []DocumentSuggestion{suggestion}, env.db, false)

	var partial *PartialUpdateError
	require.ErrorAs(t, err, &partial, "UpdateDocuments must return PartialUpdateError when created_date is invalid")
	assert.Contains(t, partial.DroppedFields, "created_date", "created_date must be in DroppedFields")
	require.True(t, patchCalled, "PATCH must still be sent (with the valid fields)")
	if created, ok := receivedPatch["created_date"]; ok {
		t.Errorf("PATCH must not include invalid created_date, but got %v", created)
	}
	assert.Equal(t, "Better Title", receivedPatch["title"], "valid fields must still be sent")
}
