package main

import (
	"context"

	"gorm.io/gorm"
)

// GetDocumentsApiResponse is the response payload for /documents endpoint.
// But we are only interested in a subset of the fields.
type GetDocumentsApiResponse struct {
	Count int `json:"count"`
	// Next     interface{} `json:"next"`
	// Previous interface{} `json:"previous"`
	All     []int                          `json:"all"`
	Results []GetDocumentApiResponseResult `json:"results"`
}

// GetDocumentApiResponseResult is a part of the response payload for /documents endpoint.
// But we are only interested in a subset of the fields.
type GetDocumentApiResponseResult struct {
	ID            int `json:"id"`
	Correspondent int `json:"correspondent"`
	// DocumentType        interface{}   `json:"document_type"`
	// StoragePath         interface{}   `json:"storage_path"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Tags    []int  `json:"tags"`
	// Created             time.Time     `json:"created"`
	CreatedDate string `json:"created_date"`
	// Modified            time.Time     `json:"modified"`
	// Added               time.Time     `json:"added"`
	// ArchiveSerialNumber interface{}   `json:"archive_serial_number"`
	// OriginalFileName    string        `json:"original_file_name"`
	// ArchivedFileName    string        `json:"archived_file_name"`
	// Owner               int           `json:"owner"`
	// UserCanChange       bool          `json:"user_can_change"`
	Notes []interface{} `json:"notes"`
	// SearchHit struct {
	// 	Score          float64 `json:"score"`
	// 	Highlights     string  `json:"highlights"`
	// 	NoteHighlights string  `json:"note_highlights"`
	// 	Rank           int     `json:"rank"`
	// } `json:"__search_hit__"`
}

// CustomFieldResponse represents a custom field with its value for a document
type CustomFieldResponse struct {
	Field int         `json:"field"`
	Value interface{} `json:"value"`
}

// CustomFieldSuggestion represents a suggested custom field with its value and name
type CustomFieldSuggestion struct {
	ID    int         `json:"id"`
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

// GetDocumentApiResponse is the response payload for /documents/{id} endpoint.
// But we are only interested in a subset of the fields.
type GetDocumentApiResponse struct {
	ID               int                   `json:"id"`
	Correspondent    int                   `json:"correspondent"`
	DocumentType     int                   `json:"document_type"`
	Title            string                `json:"title"`
	Content          string                `json:"content"`
	Tags             []int                 `json:"tags"`
	CreatedDate      string                `json:"created_date"`
	OriginalFileName string                `json:"original_file_name"`
	Notes            []interface{}         `json:"notes"`
	CustomFields     []CustomFieldResponse `json:"custom_fields"`
}

// Document is a stripped down version of the document object from paperless-ngx.
// Response payload for /documents endpoint and part of request payload for /generate-suggestions endpoint
type Document struct {
	ID               int                   `json:"id"`
	Title            string                `json:"title"`
	Content          string                `json:"content"`
	Tags             []string              `json:"tags"`
	Correspondent    string                `json:"correspondent"`
	CreatedDate      string                `json:"created_date"`
	OriginalFileName string                `json:"original_file_name"`
	DocumentTypeName string                `json:"document_type_name"`
	CustomFields     []CustomFieldResponse `json:"custom_fields"`
	Notes            []interface{}         `json:"notes"`
}

// GenerateSuggestionsRequest is the request payload for generating suggestions for /generate-suggestions endpoint
type GenerateSuggestionsRequest struct {
	Documents              []Document `json:"documents"`
	GenerateTitles         bool       `json:"generate_titles,omitempty"`
	GenerateTags           bool       `json:"generate_tags,omitempty"`
	GenerateCorrespondents bool       `json:"generate_correspondents,omitempty"`
	GenerateCreatedDate    bool       `json:"generate_created_date,omitempty"`
	GenerateCustomFields   bool       `json:"generate_custom_fields,omitempty"`
	GenerateSummary        bool       `json:"generate_summary,omitempty"`
}

// Settings defines the structure for server-side UI settings
type Settings struct {
	CustomFieldsEnable      bool   `json:"custom_fields_enable"`
	CustomFieldsSelectedIDs []int  `json:"custom_fields_selected_ids"`
	CustomFieldsWriteMode   string `json:"custom_fields_write_mode"` // "append" or "replace"
}

// DocumentSuggestion is the response payload for /generate-suggestions endpoint and the request payload for /update-documents endpoint (as an array)
type DocumentSuggestion struct {
	ID                     int                     `json:"id"`
	OriginalDocument       Document                `json:"original_document"`
	SuggestedTitle         string                  `json:"suggested_title,omitempty"`
	SuggestedTags          []string                `json:"suggested_tags,omitempty"`
	SuggestedContent       string                  `json:"suggested_content,omitempty"`
	SuggestedCorrespondent string                  `json:"suggested_correspondent,omitempty"`
	SuggestedCreatedDate   string                  `json:"suggested_created_date,omitempty"`
	SuggestedCustomFields  []CustomFieldSuggestion `json:"suggested_custom_fields,omitempty"`
	SuggestedSummary       string                  `json:"suggested_summary,omitempty"`
	KeepOriginalTags       bool                    `json:"keep_original_tags,omitempty"`
	RemoveTags             []string                `json:"remove_tags,omitempty"`
	CustomFieldsWriteMode  string                  `json:"custom_fields_write_mode,omitempty"`
	CustomFieldsEnable     bool                    `json:"custom_fields_enable"`
}

type Correspondent struct {
	Name              string `json:"name"`
	MatchingAlgorithm int    `json:"matching_algorithm"`
	Match             string `json:"match"`
	IsInsensitive     bool   `json:"is_insensitive"`
	Owner             *int   `json:"owner"`
	SetPermissions    struct {
		View struct {
			Users  []int `json:"users"`
			Groups []int `json:"groups"`
		} `json:"view"`
		Change struct {
			Users  []int `json:"users"`
			Groups []int `json:"groups"`
		} `json:"change"`
	} `json:"set_permissions"`
}

// OCROptions contains options for the OCR processing
type OCROptions struct {
	UploadPDF       bool   // Whether to upload the generated PDF
	ReplaceOriginal bool   // Whether to delete the original document after uploading
	CopyMetadata    bool   // Whether to copy metadata from the original document
	LimitPages      int    // Limit on the number of pages to process (0 = no limit)
	ProcessMode     string // OCR processing mode: "image" (default) or "pdf"
}

// ClientInterface defines the interface for PaperlessClient operations
type ClientInterface interface {
	GetDocumentsByTags(ctx context.Context, tags []string, pageSize int) ([]Document, error)
	UpdateDocuments(ctx context.Context, documents []DocumentSuggestion, db *gorm.DB, isUndo bool) error
	GetDocument(ctx context.Context, documentID int) (Document, error)
	GetAllTags(ctx context.Context) (map[string]int, error)
	GetAllCorrespondents(ctx context.Context) (map[string]int, error)
	GetAllDocumentTypes(ctx context.Context) ([]DocumentType, error)
	GetCustomFields(ctx context.Context) ([]CustomField, error)
	CreateTag(ctx context.Context, tagName string) (int, error)
	DownloadDocumentAsImages(ctx context.Context, documentID int, pageLimit int) ([]string, int, error)
	DownloadDocumentAsPDF(ctx context.Context, documentID int, limitPages int, split bool) ([]string, []byte, int, error)
	UploadDocument(ctx context.Context, data []byte, filename string, metadata map[string]interface{}) (string, error)
	GetTaskStatus(ctx context.Context, taskID string) (map[string]interface{}, error)
	DeleteDocument(ctx context.Context, documentID int) error
}

// DocumentProcessor defines the interface for processing documents with OCR
type DocumentProcessor interface {
	ProcessDocumentOCR(ctx context.Context, documentID int, options OCROptions) (*ProcessedDocument, error)
}
