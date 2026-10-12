package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"paperless-gpt/internal/pdfrender"
)

// SearchDocuments finds documents for the Playground picker. An empty query
// returns the most recently added documents; otherwise paperless-ngx's
// full-text search is used.
func (client *PaperlessClient) SearchDocuments(ctx context.Context, query string, pageSize int) ([]Document, error) {
	var path string
	if strings.TrimSpace(query) == "" {
		path = fmt.Sprintf("api/documents/?ordering=-added&page_size=%d", pageSize)
	} else {
		path = fmt.Sprintf("api/documents/?query=%s&page_size=%d", url.QueryEscape(query), pageSize)
	}

	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed in SearchDocuments: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error searching documents: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	var documentsResponse GetDocumentsApiResponse
	if err := json.Unmarshal(bodyBytes, &documentsResponse); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	allTags, err := client.GetAllTags(ctx)
	if err != nil {
		return nil, err
	}
	allCorrespondents, err := client.GetAllCorrespondents(ctx)
	if err != nil {
		return nil, err
	}

	documents := make([]Document, 0, len(documentsResponse.Results))
	for _, result := range documentsResponse.Results {
		tagNames, invisibleTagIDs := resolveTagNames(result.Tags, allTags)
		if len(invisibleTagIDs) > 0 {
			log.Warnf("Document %d has tag IDs %v that are not visible to the API user; they will be preserved but cannot be managed.", result.ID, invisibleTagIDs)
		}

		correspondentName := ""
		if result.Correspondent != 0 {
			for name, id := range allCorrespondents {
				if result.Correspondent == id {
					correspondentName = name
					break
				}
			}
		}

		documents = append(documents, Document{
			ID:            result.ID,
			Title:         result.Title,
			Content:       result.Content,
			Correspondent: correspondentName,
			Tags:          tagNames,
			TagIDs:        result.Tags,
			CreatedDate:   result.CreatedDate,
		})
	}

	return documents, nil
}

// FindDocumentIDsByReference returns the ids of up to limit documents whose
// content contains reference as a whole word (case-insensitive), e.g. an
// invoice number cited by a reminder.
//
// It uses the plain content__icontains filter rather than full-text search:
// it matches identifiers exactly, behaves the same across paperless-ngx
// versions and does not depend on the search index being up to date. The
// substring match is then narrowed to whole words, so "R123" does not match
// "R1234".
func (client *PaperlessClient) FindDocumentIDsByReference(ctx context.Context, reference string, limit int) ([]int, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, nil
	}

	path := fmt.Sprintf("api/documents/?content__icontains=%s&fields=id,content&page_size=%d", url.QueryEscape(reference), limit)
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed in FindDocumentIDsByReference: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error searching documents by reference: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	var documentsResponse GetDocumentsApiResponse
	if err := json.Unmarshal(bodyBytes, &documentsResponse); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	wholeWord := regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}])` + regexp.QuoteMeta(reference) + `($|[^\p{L}\p{N}])`)
	var ids []int
	for _, result := range documentsResponse.Results {
		if wholeWord.MatchString(result.Content) {
			ids = append(ids, result.ID)
		}
	}
	return ids, nil
}

// GetDocumentPageImage renders one page of a document as a JPEG for the
// Playground's scan-next-to-text view. Rendered pages are cached on disk
// (separately from the OCR pipeline's temporary page images, which get
// deleted after each run).
func (client *PaperlessClient) GetDocumentPageImage(ctx context.Context, documentID int, pageIndex int) ([]byte, error) {
	if pageIndex < 0 {
		return nil, fmt.Errorf("page index must not be negative")
	}

	docDir := filepath.Join(client.GetCacheFolder(), fmt.Sprintf("document-%d", documentID))
	if err := os.MkdirAll(docDir, 0755); err != nil {
		return nil, err
	}
	previewPath := filepath.Join(docDir, fmt.Sprintf("preview-page%03d.jpg", pageIndex))
	if data, err := os.ReadFile(previewPath); err == nil {
		return data, nil
	}

	// Download the PDF and render the requested page.
	path := fmt.Sprintf("api/documents/%d/download/", documentID)
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error downloading document %d: %d, %s", documentID, resp.StatusCode, string(bodyBytes))
	}
	pdfData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	doc, err := pdfrender.Open(ctx, pdfData)
	if err != nil {
		return nil, err
	}
	defer doc.Close()

	if pageIndex >= doc.NumPages() {
		return nil, fmt.Errorf("page %d out of range: document has %d pages", pageIndex+1, doc.NumPages())
	}

	// Render at a DPI that keeps the preview readable but bounded in size.
	widthPts, _, err := doc.PageSize(pageIndex)
	if err != nil {
		return nil, err
	}
	const targetWidth = 1200.0
	dpi := math.Min(150, math.Max(72, targetWidth/(widthPts/72.0)))
	img, err := doc.RenderDPI(pageIndex, dpi)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}

	if err := os.WriteFile(previewPath, buf.Bytes(), 0644); err != nil {
		log.Warnf("Failed to cache page preview for document %d page %d: %v", documentID, pageIndex, err)
	}
	return buf.Bytes(), nil
}
