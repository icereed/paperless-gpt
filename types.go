package main

import (
	"time"
)

type GetDocumentsApiResponse struct {
	Count    int         `json:"count"`
	Next     interface{} `json:"next"`
	Previous interface{} `json:"previous"`
	All      []int       `json:"all"`
	Results  []struct {
		ID                  int           `json:"id"`
		Correspondent       interface{}   `json:"correspondent"`
		DocumentType        interface{}   `json:"document_type"`
		StoragePath         interface{}   `json:"storage_path"`
		Title               string        `json:"title"`
		Content             string        `json:"content"`
		Tags                []int         `json:"tags"`
		Created             time.Time     `json:"created"`
		CreatedDate         string        `json:"created_date"`
		Modified            time.Time     `json:"modified"`
		Added               time.Time     `json:"added"`
		ArchiveSerialNumber interface{}   `json:"archive_serial_number"`
		OriginalFileName    string        `json:"original_file_name"`
		ArchivedFileName    string        `json:"archived_file_name"`
		Owner               int           `json:"owner"`
		UserCanChange       bool          `json:"user_can_change"`
		Notes               []interface{} `json:"notes"`
		SearchHit           struct {
			Score          float64 `json:"score"`
			Highlights     string  `json:"highlights"`
			NoteHighlights string  `json:"note_highlights"`
			Rank           int     `json:"rank"`
		} `json:"__search_hit__"`
	} `json:"results"`
}

// Document is a stripped down version of the document object from paperless-ngx.
// Response payload for /documents endpoint and part of request payload for /generate-suggestions endpoint
type Document struct {
	ID      int      `json:"id"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

// GenerateSuggestionsRequest is the request payload for generating suggestions for /generate-suggestions endpoint
type GenerateSuggestionsRequest struct {
	Documents      []Document `json:"documents"`
	GenerateTitles bool       `json:"generate_titles,omitempty"`
	GenerateTags   bool       `json:"generate_tags,omitempty"`
}

// DocumentSuggestion is the response payload for /generate-suggestions endpoint and the request payload for /update-documents endpoint (as an array)
type DocumentSuggestion struct {
	ID               int      `json:"id"`
	OriginalDocument Document `json:"original_document"`
	SuggestedTitle   string   `json:"suggested_title,omitempty"`
	SuggestedTags    []string `json:"suggested_tags,omitempty"`
	SuggestedContent string   `json:"suggested_content,omitempty"`
}
