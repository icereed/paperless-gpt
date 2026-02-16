package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
