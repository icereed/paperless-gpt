package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"text/template"
	"time"

	"paperless-gpt/ocr"

	"github.com/Masterminds/sprig/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func init() {
	// Disable token limit to avoid warnings
	tokenLimit = 0
}

// mockOCRProvider implements ocr.Provider interface
type mockOCRProvider struct {
	text string
}

func (m *mockOCRProvider) ProcessImage(ctx context.Context, imageData []byte, pageNumber int) (*ocr.OCRResult, error) {
	return &ocr.OCRResult{
		Text:     m.text,
		Metadata: map[string]string{"language": "eng"},
	}, nil
}

// mockDocumentProcessor implements the DocumentProcessor interface for testing
type mockDocumentProcessor struct {
	mockText string
}

func (m *mockDocumentProcessor) ProcessDocumentOCR(ctx context.Context, documentID int, options OCROptions) (*ProcessedDocument, error) {
	return &ProcessedDocument{
		ID:   documentID,
		Text: m.mockText,
	}, nil
}

// mockClient implements the ClientInterface for testing
type mockClient struct {
	*PaperlessClient
	documents        map[int]Document
	tags             map[string]int
	taggedDocuments  map[string][]Document
	updateDocsCalled bool
}

func newMockClient(baseClient *PaperlessClient) *mockClient {
	return &mockClient{
		PaperlessClient: baseClient,
		documents:       make(map[int]Document),
		tags:            make(map[string]int),
		taggedDocuments: make(map[string][]Document),
	}
}

func (m *mockClient) GetDocumentsByTags(ctx context.Context, tags []string, pageSize int) ([]Document, error) {
	if len(tags) == 0 {
		return nil, nil
	}

	// For simplicity, just return documents for the first tag
	return m.taggedDocuments[tags[0]], nil
}

func (m *mockClient) UpdateDocuments(ctx context.Context, documents []DocumentSuggestion, db *gorm.DB, isUndo bool) error {
	m.updateDocsCalled = true
	return nil
}

func (m *mockClient) GetDocument(ctx context.Context, documentID int) (Document, error) {
	if doc, exists := m.documents[documentID]; exists {
		return doc, nil
	}
	return Document{}, fmt.Errorf("document %d not found", documentID)
}

func (m *mockClient) AddDocument(doc Document, tags []string) {
	m.documents[doc.ID] = doc

	// Add document to tagged document lists
	for _, tag := range tags {
		m.taggedDocuments[tag] = append(m.taggedDocuments[tag], doc)
	}
}

func (m *mockClient) AddTag(name string, id int) {
	m.tags[name] = id
}

// OCRTestCase defines test case structure for OCR tests
type OCRTestCase struct {
	name          string
	pdfOCRTagging bool
	documents     []TestDocument
	mockOCRText   string
	expectedCount int
	expectedError string
}

// This our appStub for background processing isolation without real invocation
type appStubBG struct {
	*App
	ocrCalls int
	tagCalls int
}

func (a *appStubBG) isOcrEnabled() bool { return true }
func (a *appStubBG) processAutoOcrTagDocuments(ctx context.Context) (int, error) {
	a.ocrCalls++
	// Return fixed count for background test
	if a.App == nil {
		return 1, nil
	}
	return a.App.processAutoOcrTagDocuments(ctx)
}

func (a *appStubBG) processAutoTagDocuments(ctx context.Context) (int, error) {
	a.tagCalls++
	// Return fixed count for background test
	if a.App == nil {
		return 1, nil
	}
	return a.App.processAutoTagDocuments(ctx)
}

// Setup a Test
func setupTest(t *testing.T) *testEnv {
	// Initialize templates
	var err error
	titleTemplate, err = template.New("title").Funcs(sprig.FuncMap()).Parse("")
	require.NoError(t, err)
	tagTemplate, err = template.New("tag").Funcs(sprig.FuncMap()).Parse("")
	require.NoError(t, err)
	correspondentTemplate, err = template.New("correspondent").Funcs(sprig.FuncMap()).Parse("")
	require.NoError(t, err)
	createdDateTemplate, err = template.New("created_date").Funcs(sprig.FuncMap()).Parse("")
	require.NoError(t, err)

	// Create test environment
	env := newTestEnv(t)

	// Do not defer teardown here — caller must do it
	return env
}

// Setup a single Test-Case
func setupTestCase(tc interface{}, env *testEnv) {
	var documents []TestDocument

	switch t := tc.(type) {
	case *OCRTestCase:
		documents = t.documents
	case TestCase:
		documents = t.documents
	default:
		panic(fmt.Sprintf("unsupported test case type: %T", tc))
	}

	// Mock the GetAllTags response
	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"results": []map[string]interface{}{
				{"id": 1, "name": autoTag},
				{"id": 2, "name": autoOcrTag},
				{"id": 3, "name": "other-tag"},
				{"id": 4, "name": pdfOCRCompleteTag},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	// Mock the GetDocumentsByTags response
	env.setMockResponse("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		response := GetDocumentsApiResponse{
			Results: make([]GetDocumentApiResponseResult, len(documents)),
		}
		for i, doc := range documents {
			tagIds := make([]int, len(doc.Tags))
			for j, tagName := range doc.Tags {
				switch tagName {
				case autoTag:
					tagIds[j] = 1
				case autoOcrTag:
					tagIds[j] = 2
				case pdfOCRCompleteTag:
					tagIds[j] = 4
				default:
					tagIds[j] = 3
				}
			}
			response.Results[i] = GetDocumentApiResponseResult{
				ID:          doc.ID,
				Title:       doc.Title,
				Tags:        tagIds,
				Content:     "Test content",
				CreatedDate: "1999-09-09",
			}
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	// Mock the correspondent creation endpoint
	env.setMockResponse("/api/correspondents/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   3,
				"name": "test response",
			})
		} else {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []map[string]interface{}{
					{"id": 1, "name": "Alpha"},
					{"id": 2, "name": "Beta"},
				},
			})
		}
	})
}

// Test that our Background-Tasks shutdown cleanly and process properly
func TestBackgroundTasks_ShutdownOnContextCancel(t *testing.T) {
	// Initialize required global variables
	autoOcrTag = "paperless-gpt-ocr-auto"
	pdfOCRCompleteTag = "paperless-gpt-ocr-complete"

	// Setup test environment
	env := setupTest(t)
	defer env.teardown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up API mocks
	setupTestCase(&OCRTestCase{
		documents: []TestDocument{
			{
				ID:    1,
				Title: "Test Doc",
				Tags:  []string{autoOcrTag},
			},
		}}, env)

	// Mock document fetch
	env.setMockResponse("/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			response := GetDocumentApiResponse{
				ID:               1,
				Title:            "Test Doc",
				Tags:             []int{2}, // Has autoOcrTag
				Content:          "Original content",
				OriginalFileName: "test.pdf",
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
			return
		}

		// Handle PATCH updates
		if r.Method == "PATCH" {
			var updateReq map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&updateReq); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      1,
				"title":   "Test Doc",
				"content": "test ocr",
				"tags":    updateReq["tags"],
			})
			return
		}
	})

	// Mock document download
	env.setMockResponse("/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("%PDF-1.4\n%test pdf content"))
	})

	// Create stub app without base App for background test
	app := &appStubBG{}
	done := make(chan struct{})

	// Start in test wrapper that closes when background exits
	go func() {
		StartBackgroundTasks(ctx, app)
		close(done)
	}()

	// Let it run a bit to ensure at least one iteration
	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case <-done:
	// success
	case <-time.After(1 * time.Second):
		t.Fatal("background task did not shut down in time")
	}

	assert.Greater(t, app.ocrCalls, 0, "OCR loop should have run at least once")
	assert.Greater(t, app.tagCalls, 0, "Tag loop should have run at least once")
}

// TestApp extends App with testing capabilities
type TestApp struct {
	*App
}

// ProcessDocumentOCR overrides the real implementation to avoid PDF processing during tests
func (app *TestApp) ProcessDocumentOCR(ctx context.Context, documentID int, options OCROptions) (*ProcessedDocument, error) {
	// Create a simple processed document with mock data
	mockText := app.ocrProvider.(*mockOCRProvider).text
	return &ProcessedDocument{
		ID:   documentID,
		Text: mockText,
	}, nil
}

// This method shadows the App.processAutoOcrTagDocuments to avoid calling the original
func (app *TestApp) processAutoOcrTagDocuments(ctx context.Context) (int, error) {
	documents, err := app.Client.GetDocumentsByTags(ctx, []string{autoOcrTag}, 25)
	if err != nil {
		return 0, fmt.Errorf("error fetching documents with autoOcrTag: %w", err)
	}

	if len(documents) == 0 {
		return 0, nil
	}

	successCount := 0
	var errs []error

	for _, document := range documents {
		docLogger := documentLogger(document.ID)
		docLogger.Info("Processing document for OCR")

		// Skip OCR if the document already has the OCR complete tag and tagging is enabled
		if app.pdfOCRTagging {
			hasCompleteTag := false
			for _, tag := range document.Tags {
				if tag == app.pdfOCRCompleteTag {
					hasCompleteTag = true
					break
				}
			}

			if hasCompleteTag {
				docLogger.Infof("Document already has OCR complete tag '%s', skipping OCR processing", app.pdfOCRCompleteTag)

				// Remove only the autoOcrTag to take it out of the processing queue
				// while preserving the OCR complete tag
				err = app.Client.UpdateDocuments(ctx, []DocumentSuggestion{
					{
						ID:               document.ID,
						OriginalDocument: document,
						RemoveTags:       []string{autoOcrTag},
					},
				}, app.Database, false)

				if err != nil {
					docLogger.Errorf("Update to remove autoOcrTag failed: %v", err)
					errs = append(errs, fmt.Errorf("document %d update error: %w", document.ID, err))
					continue
				}

				docLogger.Info("Successfully removed auto OCR tag")
				successCount++
				continue
			}
		}

		// We skip the actual document download and OCR processing in the test
		// Instead, we directly use our mock OCR provider
		mockText := app.ocrProvider.(*mockOCRProvider).text
		processedDoc := &ProcessedDocument{
			ID:   document.ID,
			Text: mockText,
		}
		docLogger.Debug("OCR processing completed")

		documentSuggestion := DocumentSuggestion{
			ID:               document.ID,
			OriginalDocument: document,
			SuggestedContent: processedDoc.Text,
			RemoveTags:       []string{autoOcrTag},
		}

		if (app.pdfOCRTagging) && app.pdfOCRCompleteTag != "" {
			// Add the OCR complete tag if tagging is enabled
			documentSuggestion.SuggestedTags = []string{app.pdfOCRCompleteTag}
			documentSuggestion.KeepOriginalTags = true
			docLogger.Infof("Adding OCR complete tag '%s'", app.pdfOCRCompleteTag)
		}

		err = app.Client.UpdateDocuments(ctx, []DocumentSuggestion{
			documentSuggestion,
		}, app.Database, false)
		if err != nil {
			docLogger.Errorf("Update after OCR failed: %v", err)
			errs = append(errs, fmt.Errorf("document %d update error: %w", document.ID, err))
			continue
		}

		docLogger.Info("Successfully processed document OCR")
		successCount++
	}

	if len(errs) > 0 {
		return successCount, fmt.Errorf("one or more errors occurred: %w", errors.Join(errs...))
	}

	return successCount, nil
}

func TestProcessAutoOcrTagDocuments(t *testing.T) {
	// Initialize required global variables
	autoOcrTag = "paperless-gpt-ocr-auto"
	pdfOCRCompleteTag = "paperless-gpt-ocr-complete"

	testCases := []OCRTestCase{
		{
			name:          "Add OCR complete tag when enabled",
			pdfOCRTagging: true,
			documents: []TestDocument{
				{
					ID:    1,
					Title: "Doc for OCR",
					Tags:  []string{autoOcrTag},
				},
			},
			mockOCRText:   "OCR processed text",
			expectedCount: 1,
			expectedError: "",
		},
		{
			name:          "Skip OCR complete tag when disabled",
			pdfOCRTagging: false,
			documents: []TestDocument{
				{
					ID:    2,
					Title: "Doc for OCR no tag",
					Tags:  []string{autoOcrTag},
				},
			},
			mockOCRText:   "OCR processed text",
			expectedCount: 1,
			expectedError: "",
		},
		{
			name:          "Keep existing tags when processing",
			pdfOCRTagging: true,
			documents: []TestDocument{
				{
					ID:    3,
					Title: "Doc with existing tags",
					Tags:  []string{autoOcrTag, "existing-tag"},
				},
			},
			mockOCRText:   "OCR processed text",
			expectedCount: 1,
			expectedError: "",
		},
	}

	// Setup the Test
	env := setupTest(t)
	defer env.teardown()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create an isolated Context
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			defer cancel()

			// Create mock client
			client := newMockClient(env.client)

			// Add test tags
			client.AddTag(autoOcrTag, 2)
			client.AddTag(pdfOCRCompleteTag, 4)
			client.AddTag("existing-tag", 3)

			// Add test documents
			for _, testDoc := range tc.documents {
				// Convert TestDocument to Document
				doc := Document{
					ID:               testDoc.ID,
					Title:            testDoc.Title,
					Tags:             testDoc.Tags,
					Content:          "Original content",
					OriginalFileName: "test.pdf",
				}
				client.AddDocument(doc, testDoc.Tags)
			}

			// Create mock document processor
			docProcessor := &mockDocumentProcessor{
				mockText: tc.mockOCRText,
			}

			// Create test app with mocks
			app := &App{
				Client:            client,
				Database:          env.db,
				ocrProvider:       &mockOCRProvider{text: tc.mockOCRText},
				docProcessor:      docProcessor,
				pdfOCRTagging:     tc.pdfOCRTagging,
				pdfOCRCompleteTag: pdfOCRCompleteTag,
			}

			// Test the processAutoOcrTagDocuments method
			count, err := app.processAutoOcrTagDocuments(ctx)

			// Verify results
			if tc.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedCount, count)
			}

			// Verify client was called to update documents
			assert.True(t, client.updateDocsCalled, "UpdateDocuments should have been called")
		})
	}
}
