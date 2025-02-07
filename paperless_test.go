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

// TestGetDocumentsByTags tests the GetDocumentsByTags method
func TestGetDocumentsByTags(t *testing.T) {
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
			},
			{
				ID:            2,
				Title:         "Document 2",
				Content:       "Content 2",
				Tags:          []int{2, 3},
				Correspondent: 2,
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

	// Set mock responses
	env.setMockResponse("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		// Verify query parameters
		expectedQuery := "tags__name__iexact=tag1&tags__name__iexact=tag2&page_size=25"
		assert.Equal(t, expectedQuery, r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(documentsResponse)
	})

	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(tagsResponse)
	})

	ctx := context.Background()
	tags := []string{"tag1", "tag2"}
	documents, err := env.client.GetDocumentsByTags(ctx, tags, 25)
	require.NoError(t, err)

	expectedDocuments := []Document{
		{
			ID:            1,
			Title:         "Document 1",
			Content:       "Content 1",
			Tags:          []string{"tag1", "tag2"},
			Correspondent: "Alpha",
		},
		{
			ID:            2,
			Title:         "Document 2",
			Content:       "Content 2",
			Tags:          []string{"tag2", "tag3"},
			Correspondent: "Beta",
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
				ID:    1,
				Title: "Old Title",
				Tags:  []string{"tag1", "tag3", "manual", "removeMe"},
			},
			SuggestedTitle: "New Title",
			SuggestedTags:  []string{"tag2", "tag3"},
			RemoveTags:     []string{"removeMe"},
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
			"tags": []interface{}{float64(idTag2), float64(idTag3)},
		}

		assert.Equal(t, expectedFields, updatedFields)

		w.WriteHeader(http.StatusOK)
	})

	ctx := context.Background()
	err := env.client.UpdateDocuments(ctx, documents, env.db, false)
	require.NoError(t, err)
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
	imagePaths, err := env.client.DownloadDocumentAsImages(ctx, document.ID, 0)
	require.NoError(t, err)

	// Verify that exatly one page was extracted
	assert.Len(t, imagePaths, 1)
	// The path shall end with paperless-gpt/document-123/page000.jpg
	assert.Contains(t, imagePaths[0], "paperless-gpt/document-123/page000.jpg")
	for _, imagePath := range imagePaths {
		_, err := os.Stat(imagePath)
		assert.NoError(t, err)
	}
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
	imagePaths, err := env.client.DownloadDocumentAsImages(ctx, document.ID, 50)
	require.NoError(t, err)

	// Verify that exatly 50 pages were extracted - the original doc contains 52 pages
	assert.Len(t, imagePaths, 50)
	// The path shall end with tests/tmp/document-321/page000.jpg
	for _, imagePath := range imagePaths {
		_, err := os.Stat(imagePath)
		assert.NoError(t, err)
		assert.Contains(t, imagePath, "tests/tmp/document-321/page")
	}
}
