package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
)

// splitClient keeps the split-specific API out of metadata and OCR mocks.
type splitClient interface {
	InvalidateSplitPreviews(int) error
	DownloadPDF(context.Context, Document) ([]byte, error)
	ListSplitDocumentIDs(context.Context, string) ([]int, error)
	GetSplitSource(context.Context, int) (splitSource, error)
	SetSplitTags(context.Context, int, []int) error
	SplitDocument(context.Context, int, int, []int, bool) error
	EnsureTagExists(context.Context, string) error
}

type splitSource struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Tags      []int  `json:"tags"`
	PageCount int    `json:"page_count"`
	Checksum  string `json:"original_checksum"`
	MimeType  string `json:"original_mime_type"`
}

type splitHTTPError struct{ Status int }

// Error reports the HTTP status returned by a failed split API request.
func (e *splitHTTPError) Error() string {
	return fmt.Sprintf("paperless split request returned HTTP %d", e.Status)
}

// splitRequest sends an optional JSON body and decodes a 200 response into result
// with an 8 MiB read limit. It returns encoding, transport, status, or decoding errors.
func (client *PaperlessClient) splitRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	response, err := client.Do(ctx, method, path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &splitHTTPError{Status: response.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(result); err != nil {
		return fmt.Errorf("invalid paperless split response: %w", err)
	}
	return nil
}

// ListSplitDocumentIDs collects IDs across all pages of a case-insensitive tag
// query ordered by ID. A request failure returns an error without partial results.
func (client *PaperlessClient) ListSplitDocumentIDs(ctx context.Context, tag string) ([]int, error) {
	var ids []int
	for page := 1; ; page++ {
		var result struct {
			Next    *string `json:"next"`
			Results []struct {
				ID int `json:"id"`
			} `json:"results"`
		}
		path := fmt.Sprintf("api/documents/?tags__name__iexact=%s&page_size=100&page=%d&ordering=id&fields=id", url.QueryEscape(tag), page)
		if err := client.splitRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
			return nil, err
		}
		for _, doc := range result.Results {
			ids = append(ids, doc.ID)
		}
		if result.Next == nil || len(result.Results) == 0 {
			return ids, nil
		}
	}
}

// GetSplitSource fetches document details and metadata, requiring a matching ID,
// PDF MIME type, checksum, and positive page count. It returns the details fetched
// so far alongside any request or validation error.
func (client *PaperlessClient) GetSplitSource(ctx context.Context, id int) (splitSource, error) {
	var source splitSource
	if err := client.splitRequest(ctx, http.MethodGet, fmt.Sprintf("api/documents/%d/", id), nil, &source); err != nil {
		return source, err
	}
	var metadata struct {
		Checksum string `json:"original_checksum"`
		MimeType string `json:"original_mime_type"`
	}
	if err := client.splitRequest(ctx, http.MethodGet, fmt.Sprintf("api/documents/%d/metadata/", id), nil, &metadata); err != nil {
		return source, err
	}
	source.Checksum, source.MimeType = metadata.Checksum, metadata.MimeType
	if source.ID != id || source.PageCount < 1 || source.Checksum == "" || source.MimeType != "application/pdf" {
		return source, fmt.Errorf("split requires a PDF with a checksum and page_count")
	}
	return source, nil
}

// SetSplitTags replaces a document's tags, treating nil as an empty list, and
// returns an error if the request fails or the returned tags differ after sorting.
func (client *PaperlessClient) SetSplitTags(ctx context.Context, id int, tags []int) error {
	if tags == nil {
		tags = []int{}
	}
	var result struct {
		Tags []int `json:"tags"`
	}
	if err := client.splitRequest(ctx, http.MethodPatch, fmt.Sprintf("api/documents/%d/", id), map[string]interface{}{"tags": tags}, &result); err != nil {
		return err
	}
	expected, actual := slices.Clone(tags), slices.Clone(result.Tags)
	slices.Sort(expected)
	slices.Sort(actual)
	if !slices.Equal(expected, actual) {
		return fmt.Errorf("paperless did not retain the requested split tags")
	}
	return nil
}

// SplitDocument submits one document for splitting at one-based after-page
// positions, forwarding deleteOriginals to the bulk-edit API. It returns boundary
// validation, request, or non-OK result errors without waiting for split completion.
func (client *PaperlessClient) SplitDocument(ctx context.Context, id, total int, boundaries []int, deleteOriginals bool) error {
	pages, err := splitPagesString(total, boundaries)
	if err != nil {
		return err
	}
	var result struct {
		Result string `json:"result"`
	}
	err = client.splitRequest(ctx, http.MethodPost, "api/documents/bulk_edit/", map[string]interface{}{
		"documents": []int{id}, "method": "split", "parameters": map[string]interface{}{"pages": pages, "delete_originals": deleteOriginals},
	}, &result)
	if err != nil {
		return err
	}
	if result.Result != "OK" {
		return fmt.Errorf("unexpected split result %q", result.Result)
	}
	return nil
}

// InvalidateSplitPreviews removes cached page thumbnails so regeneration cannot
// reuse an earlier source version, returning glob or removal errors and ignoring
// files that are already missing.
func (client *PaperlessClient) InvalidateSplitPreviews(documentID int) error {
	paths, err := filepath.Glob(filepath.Join(client.GetCacheFolder(), fmt.Sprintf("document-%d", documentID), "preview-page*.jpg"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
