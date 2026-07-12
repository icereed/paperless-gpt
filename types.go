package main

import (
	"context"
	"fmt"
	"time"

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
	Name  string      `json:"name,omitempty"`
}

// CustomFieldSuggestion represents a suggested custom field with its value and name
type CustomFieldSuggestion struct {
	ID    int         `json:"id"`
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

// PermissionSet defines view/change permissions for users and groups
type PermissionSet struct {
	Users  []int `json:"users"`
	Groups []int `json:"groups"`
}

// Permissions holds the full permission structure for a document
type Permissions struct {
	View   PermissionSet `json:"view"`
	Change PermissionSet `json:"change"`
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
	Owner            *int                  `json:"owner"`
	Permissions      *Permissions          `json:"permissions,omitempty"`
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
	DocumentType     int                   `json:"document_type"`
	CustomFields     []CustomFieldResponse `json:"custom_fields"`
	Owner            *int                  `json:"owner"`
	Permissions      *Permissions          `json:"permissions,omitempty"`
}

// GenerateSuggestionsRequest is the request payload for generating suggestions for /generate-suggestions endpoint
type GenerateSuggestionsRequest struct {
	Documents              []Document `json:"documents"`
	GenerateTitles         bool       `json:"generate_titles,omitempty"`
	GenerateTags           bool       `json:"generate_tags,omitempty"`
	GenerateCorrespondents bool       `json:"generate_correspondents,omitempty"`
	GenerateCreatedDate    bool       `json:"generate_created_date,omitempty"`
	GenerateCustomFields   bool       `json:"generate_custom_fields,omitempty"`
	GenerateDocumentTypes  bool       `json:"generate_document_types,omitempty"`
	IsAutoProcessing       bool       `json:"-"` // internal flag; not exposed via API
}

// AnalyzeDocumentsRequest is the request payload for the ad-hoc analysis
type AnalyzeDocumentsRequest struct {
	DocumentIDs []int  `json:"document_ids"`
	Prompt      string `json:"prompt"`
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
	SuggestedDocumentType  string                  `json:"suggested_document_type,omitempty"`
	SuggestedCustomFields  []CustomFieldSuggestion `json:"suggested_custom_fields,omitempty"`
	KeepOriginalTags       bool                    `json:"keep_original_tags,omitempty"`
	RemoveTags             []string                `json:"remove_tags,omitempty"`
	AddTags                []string                `json:"add_tags,omitempty"`
	CustomFieldsWriteMode  string                  `json:"custom_fields_write_mode,omitempty"`
	CustomFieldsEnable     bool                    `json:"custom_fields_enable"`
}

type Correspondent struct {
	Name              string `json:"name"`
	MatchingAlgorithm int    `json:"matching_algorithm"`
	Match             string `json:"match"`
	IsInsensitive     bool   `json:"is_insensitive"`
	// omitempty so nil owners are dropped from the JSON body; paperless-ngx
	// then falls back to the request user (request.user) as the owner of
	// the newly created object. Sending "owner": null overrides that and
	// produces ownerless correspondents — they still appear in the
	// correspondents list, but documents assigned to them are shown as
	// "private" in the UI instead of the correspondent name.
	Owner             *int   `json:"owner,omitempty"`
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

// PendingPermissionRestore represents a queued request to restore owner and permissions
// on a newly uploaded document after its consumption task completes.
type PendingPermissionRestore struct {
	TaskID        string
	OriginalDocID int
	Owner         *int
	Permissions   *Permissions
	CreatedAt     time.Time
}

// OCROptions contains options for the OCR processing
type OCROptions struct {
	UploadPDF                bool   // Whether to upload the generated PDF
	ReplaceOriginal          bool   // Whether to delete the original document after uploading
	CopyMetadata             bool   // Whether to copy metadata from the original document
	PreserveOwnerPermissions bool   // Whether to restore owner and permissions on the uploaded document
	LimitPages               int    // Limit on the number of pages to process (0 = no limit)
	ProcessMode              string // OCR processing mode: "image" (default) or "pdf"
	ExistingContent          string // Existing document text (e.g., from Tesseract) to include in OCR prompt
}

// PartialUpdateError signals that a document update succeeded only after
// paperless-gpt had to drop one or more fields that paperless-ngx rejected as
// invalid. The PATCH eventually succeeded with the surviving fields; the
// document has been written but is incomplete relative to what the LLM
// suggested. Callers should treat this as a successful update but apply the
// fail tag so the user knows the document needs review.
type PartialUpdateError struct {
	DocumentID    int
	DroppedFields []string
}

func (e *PartialUpdateError) Error() string {
	return fmt.Sprintf("document %d updated with %d field(s) dropped due to paperless-ngx validation errors: %v", e.DocumentID, len(e.DroppedFields), e.DroppedFields)
}

// ClientInterface defines the interface for PaperlessClient operations
type ClientInterface interface {
	GetDocumentsByTag(ctx context.Context, tag string, pageSize int) ([]Document, error)
	GetDocumentCountByTag(ctx context.Context, tag string) (int, error)
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
	PatchDocument(ctx context.Context, documentID int, fields map[string]interface{}) error
}

// DocumentProcessor defines the interface for processing documents with OCR
type DocumentProcessor interface {
	ProcessDocumentOCR(ctx context.Context, documentID int, options OCROptions, jobID string) (*ProcessedDocument, error)
}
