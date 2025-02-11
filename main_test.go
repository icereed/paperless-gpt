package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"text/template"

	"github.com/Masterminds/sprig/v3"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessAutoTagDocuments(t *testing.T) {
	// Initialize required global variables
	autoTag = "paperless-gpt-auto"
	autoOcrTag = "paperless-gpt-ocr-auto"

	// Initialize templates
	var err error
	titleTemplate, err = template.New("title").Funcs(sprig.FuncMap()).Parse(defaultTitleTemplate)
	require.NoError(t, err)
	tagTemplate, err = template.New("tag").Funcs(sprig.FuncMap()).Parse(defaultTagTemplate)
	require.NoError(t, err)
	correspondentTemplate, err = template.New("correspondent").Funcs(sprig.FuncMap()).Parse(defaultCorrespondentTemplate)
	require.NoError(t, err)

	// Create test environment
	env := newTestEnv(t)
	defer env.teardown()

	// Set up test cases
	testCases := []struct {
		name           string
		documents      []Document
		expectedCount  int
		expectedError  string
		updateResponse int // HTTP status code for update response
	}{
		{
			name: "Skip document with autoOcrTag",
			documents: []Document{
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
			documents:      []Document{},
			expectedCount:  0,
			updateResponse: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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
						ID:      doc.ID,
						Title:   doc.Title,
						Tags:    tagIds,
						Content: "Test content",
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

			// Mock the document update responses
			for _, doc := range tc.documents {
				if !slices.Contains(doc.Tags, autoOcrTag) {
					updatePath := fmt.Sprintf("/api/documents/%d/", doc.ID)
					env.setMockResponse(updatePath, func(w http.ResponseWriter, r *http.Request) {
						w.WriteHeader(tc.updateResponse)
						json.NewEncoder(w).Encode(map[string]interface{}{
							"id":    doc.ID,
							"title": "Updated " + doc.Title,
							"tags":  []int{1, 3}, // Mock updated tag IDs
						})
					})
				}
			}

			// Run the test
			count, err := app.processAutoTagDocuments()

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
