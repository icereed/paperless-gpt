package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstantiateCorrespondentMatchingAlgorithm tests that the instantiateCorrespondent 
// function creates a correspondent with a valid matching algorithm value
func TestInstantiateCorrespondentMatchingAlgorithm(t *testing.T) {
	// Test that the matching algorithm is set to a valid value (1 = Auto)
	correspondent := instantiateCorrespondent("Test Correspondent")
	
	// Assert the core fields are set correctly
	assert.Equal(t, "Test Correspondent", correspondent.Name)
	assert.Equal(t, 1, correspondent.MatchingAlgorithm, "MatchingAlgorithm should be 1 (Auto) instead of 0")
	assert.Equal(t, "", correspondent.Match)
	assert.Equal(t, true, correspondent.IsInsensitive)
	assert.Nil(t, correspondent.Owner)
}

// TestCreateOrGetCorrespondentWithValidMatchingAlgorithm tests the full flow of creating a correspondent
// with the corrected matching algorithm value
func TestCreateOrGetCorrespondentWithValidMatchingAlgorithm(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	correspondentName := "New Test Correspondent"
	
	// Set mock response for getting existing correspondents (none found)
	env.setMockResponse("/api/correspondents/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"results": []}`))
	})

	// Mock the POST request for creating a new correspondent 
	// This should succeed with the corrected matching_algorithm value
	env.setMockResponse("/api/correspondents/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			// Read and verify the request body
			bodyBytes, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			defer r.Body.Close()

			var requestBody Correspondent
			err = json.Unmarshal(bodyBytes, &requestBody)
			require.NoError(t, err)

			// Verify the matching algorithm is set to 1 (not 0)
			assert.Equal(t, correspondentName, requestBody.Name)
			assert.Equal(t, 1, requestBody.MatchingAlgorithm, "Request should have matching_algorithm=1")
			
			// Return a successful creation response
			response := map[string]interface{}{
				"id":                 999,
				"name":               correspondentName,
				"matching_algorithm": 1,
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(response)
		} else {
			// Handle GET request (list existing correspondents)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"results": []}`))
		}
	})

	ctx := context.Background()
	correspondent := instantiateCorrespondent(correspondentName)
	
	// This should succeed with the fix (matching_algorithm=1)
	id, err := env.client.CreateOrGetCorrespondent(ctx, correspondent)
	require.NoError(t, err)
	assert.Equal(t, 999, id)
}