package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateDocumentsReturnsFollowUpTagPatchFailure(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	oldAutoOCRTag := autoOcrTag
	autoOcrTag = "pending-ocr"
	t.Cleanup(func() { autoOcrTag = oldAutoOCRTag })

	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[{"id":7,"name":"pending-ocr"}]}`))
	})
	const documentID = 4201
	patchCount := 0
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/", documentID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			require.NoError(t, json.NewEncoder(w).Encode(GetDocumentApiResponse{ID: documentID, Title: "After", Tags: []int{7}}))
		case http.MethodPatch:
			patchCount++
			if patchCount == 1 {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"tag update rejected"}`))
		default:
			t.Errorf("unexpected document method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	err := env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{{
		ID:               documentID,
		OriginalDocument: Document{ID: documentID, Title: "Before", Tags: []string{"pending-ocr"}, TagIDs: []int{7}},
		SuggestedTitle:   "After",
		RemoveTags:       []string{"pending-ocr"},
	}}, env.db, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 400")
	assert.Equal(t, 2, patchCount, "main update succeeds, then failed follow-up tag update must be surfaced")
}
