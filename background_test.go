package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This our appStub for background processing isolation without real invocation
type appStubBG struct {
	*App
	ocrCalls int
	tagCalls int
}

func (a *appStubBG) isOcrEnabled() bool { return true }
func (a *appStubBG) processAutoOcrTagDocuments(ctx context.Context) (int, error) {
	a.ocrCalls++
	return 0, nil
}
func (a *appStubBG) processAutoTagDocuments(ctx context.Context) (int, error) {
	a.tagCalls++
	return 0, nil
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
func setupTestCase(tc TestCase, env *testEnv) {
	// Mock the GetAllTags response
	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"results": []map[string]interface{}{
				{"id": 1, "name": autoTag},
				{"id": 2, "name": autoOcrTag},
				{"id": 3, "name": "other-tag"},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	// Mock the GetDocumentsByTags response
	env.setMockResponse("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		response := GetDocumentsApiResponse{
			Results: make([]GetDocumentApiResponseResult, len(tc.documents)),
		}
		for i, doc := range tc.documents {
			tagIds := make([]int, len(doc.Tags))
			for j, tagName := range doc.Tags {
				switch tagName {
				case autoTag:
					tagIds[j] = 1
				case autoOcrTag:
					tagIds[j] = 2
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
			// Mock successful correspondent creation
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   3,
				"name": "test response",
			})
		} else {
			// Mock GET response for existing correspondents
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := &appStubBG{}
	done := make(chan struct{})

	// Start in test wrapper that closes when background exits
	go func() {
		StartBackgroundTasks(ctx, app)
		close(done)
	}()

	// Let it run a bit
	time.Sleep(200 * time.Millisecond)
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

// Test processAutoTagDocument (OCR tag is used to test skipping)
func TestProcessAutoTagDocuments(t *testing.T) {
	// Initialize required global variables
	autoTag = "paperless-gpt-auto"
	autoOcrTag = "paperless-gpt-ocr-auto"

	// Set up test cases
	testCases := []TestCase{
		{
			name: "Skip document with autoOcrTag",
			documents: []TestDocument{
				{
					ID:    1,
					Title: "Doc with OCR tag",
					Tags:  []string{autoTag, autoOcrTag},
				},
				{
					ID:    2,
					Title: "Doc without OCR tag",
					Tags:  []string{autoTag},
				},
				{
					ID:    3,
					Title: "Doc with OCR tag",
					Tags:  []string{autoTag, autoOcrTag},
				},
			},
			expectedCount:  1,
			updateResponse: http.StatusOK,
		},
		{
			name:           "No documents to process",
			documents:      []TestDocument{},
			expectedCount:  0,
			updateResponse: http.StatusOK,
		},
		{
			name: "Two fail but continues processing on update failure",
			documents: []TestDocument{
				{ID: 7, Title: "Good One", Tags: []string{autoTag}},
				{ID: 8, Title: "Update Fails 1", Tags: []string{autoTag}, FailUpdate: true},
				{ID: 9, Title: "Good Two", Tags: []string{autoTag}},
				{ID: 10, Title: "Update Fails 2", Tags: []string{autoTag}, FailUpdate: true},
			},
			expectedCount:  2,             // Only 7 and 9 succeed
			expectedError:  "document 10", // error should mention at least one of the failed docs
			updateResponse: http.StatusOK, // By default it should be OK
		},
	}

	// Setup the Test
	env := setupTest(t)
	defer env.teardown()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("Running Test-Case %s", tc.name)

			// Setup the individual Test-Case
			setupTestCase(tc, env)

			// Create an isolated Context per Test-Case with Timeout, for proper cleanup
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			defer cancel()

			// Create test app
			app := &App{
				Client:   env.client,
				Database: env.db,
				LLM:      &mockLLM{}, // Use mock LLM from app_llm_test.go
			}

			// Set auto-generate flags
			autoGenerateTitle = "true"
			autoGenerateTags = "true"
			autoGenerateCorrespondents = "true"
			autoGenerateCreatedDate = "true"

			// Handle the invidual documents
			for _, doc := range tc.documents {
				updatePath := fmt.Sprintf("/api/documents/%d/", doc.ID)

				// Wrap everything in a closure to safely capture values
				func(docID int, docTitle string, failUpdate bool, updateStatus int) {
					// PATCH /api/documents/{id}/
					env.setMockResponse(updatePath, func(w http.ResponseWriter, r *http.Request) {

						if failUpdate {
							t.Logf("Simulating update failure for document %d", docID)
							w.WriteHeader(http.StatusInternalServerError)
							_ = json.NewEncoder(w).Encode(map[string]interface{}{
								"detail": fmt.Sprintf("Simulated update failure for document %d", docID),
							})
							return
						}

						t.Logf("Simulating successful update for document %d", docID)
						w.WriteHeader(updateStatus)
						_ = json.NewEncoder(w).Encode(map[string]interface{}{
							"id":           docID,
							"title":        "Updated " + docTitle,
							"tags":         []int{1, 3},
							"created_date": "1999-09-19",
						})
					})

				}(doc.ID, doc.Title, doc.FailUpdate, tc.updateResponse)
			}

			// Test the processAutoTagDocuments method
			count, err := app.processAutoTagDocuments(ctx)

			// Verify results
			if tc.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedCount, count)
			}
		})
	}
}
