package ocr

import "time"

// AzureDocumentResult represents the root response from Azure Document Intelligence
type AzureDocumentResult struct {
	Status              string             `json:"status"`
	CreatedDateTime     time.Time          `json:"createdDateTime"`
	LastUpdatedDateTime time.Time          `json:"lastUpdatedDateTime"`
	AnalyzeResult       AzureAnalyzeResult `json:"analyzeResult"`
}

// AzureAnalyzeResult represents the analyze result part of the Azure Document Intelligence response
type AzureAnalyzeResult struct {
	APIVersion      string           `json:"apiVersion"`
	ModelID         string           `json:"modelId"`
	StringIndexType string           `json:"stringIndexType"`
	Content         string           `json:"content"`
	Pages           []AzurePage      `json:"pages"`
	Paragraphs      []AzureParagraph `json:"paragraphs"`
	Styles          []interface{}    `json:"styles"`
	ContentFormat   string           `json:"contentFormat"`
}

// AzurePage represents a single page in the document
type AzurePage struct {
	PageNumber int         `json:"pageNumber"`
	Angle      float64     `json:"angle"`
	Width      int         `json:"width"`
	Height     int         `json:"height"`
	Unit       string      `json:"unit"`
	Words      []AzureWord `json:"words"`
	Lines      []AzureLine `json:"lines"`
	Spans      []AzureSpan `json:"spans"`
}

// AzureWord represents a single word with its properties
type AzureWord struct {
	Content    string    `json:"content"`
	Polygon    []int     `json:"polygon"`
	Confidence float64   `json:"confidence"`
	Span       AzureSpan `json:"span"`
}

// AzureLine represents a line of text
type AzureLine struct {
	Content string      `json:"content"`
	Polygon []int       `json:"polygon"`
	Spans   []AzureSpan `json:"spans"`
}

// AzureSpan represents a span of text with offset and length
type AzureSpan struct {
	Offset int `json:"offset"`
	Length int `json:"length"`
}

// AzureParagraph represents a paragraph of text
type AzureParagraph struct {
	Content         string             `json:"content"`
	Spans           []AzureSpan        `json:"spans"`
	BoundingRegions []AzureBoundingBox `json:"boundingRegions"`
}

// AzureBoundingBox represents the location of content on a page
type AzureBoundingBox struct {
	PageNumber int   `json:"pageNumber"`
	Polygon    []int `json:"polygon"`
}

// AzureStyle represents style information for text segments - changed to interface{} as per input
type AzureStyle interface{}
