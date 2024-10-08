package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHttpClientWithBearerTransport tests the addition of the Authorization header.
func TestHttpClientWithBearerTransport(t *testing.T) {
	// Define the expected Bearer token
	token := "test_bearer_token"

	// Set up a test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Retrieve the Authorization header from the request
		authHeader := r.Header.Get("Authorization")
		expectedHeader := fmt.Sprintf("Bearer %s", token)

		// Check if the Authorization header matches the expected value
		if authHeader != expectedHeader {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Return a success response
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "Success")
	}))
	defer testServer.Close()

	// Create an HTTP client with the custom transport
	client := NewHttpClientWithBearerTransport(token)

	// Create a new HTTP request to the test server
	req, err := http.NewRequest("GET", testServer.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Perform the request using the custom client
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check if the status code is 200 OK
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code 200 OK, got %d", resp.StatusCode)
	}
}
