package main

import (
	"fmt"
	"net/http"
)

// HttpTransportWithBearer wraps the default RoundTripper to add the Authorization header.
type HttpTransportWithBearer struct {
	BaseTransport http.RoundTripper
	Token         string
}

// RoundTrip implements the RoundTripper interface to modify the request.
func (t *HttpTransportWithBearer) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid side effects
	reqClone := req.Clone(req.Context())

	// Add the Authorization header
	reqClone.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.Token))

	// Use the base RoundTripper to perform the request
	return t.BaseTransport.RoundTrip(reqClone)
}

func NewHttpClientWithBearerTransport(token string) *http.Client {
	// Create a new HTTP client with the custom transport
	return &http.Client{
		Transport: &HttpTransportWithBearer{
			BaseTransport: http.DefaultTransport,
			Token:         token,
		},
	}
}
