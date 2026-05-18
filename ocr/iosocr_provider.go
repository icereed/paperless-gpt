package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gardar/ocrchestra/pkg/hocr"
	"github.com/sirupsen/logrus"
)

const (
	defaultIosOcrTimeout = 60
	maxResponseSize      = 1 * 1024 * 1024 // 1MB
)

// IosOcrProvider implements OCR using the iOS OCR Server app
type IosOcrProvider struct {
	serverURL  string
	httpClient *http.Client
	enableHOCR bool
	mu         sync.Mutex
	hocrPages  []hocr.Page
}

// IosOcrBox represents a single recognized word with its bounding box
type IosOcrBox struct {
	Text string  `json:"text"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
	Rect struct {
		TopLeftX     float64 `json:"topLeft_x"`
		TopLeftY     float64 `json:"topLeft_y"`
		TopRightX    float64 `json:"topRight_x"`
		TopRightY    float64 `json:"topRight_y"`
		BottomRightX float64 `json:"bottomRight_x"`
		BottomRightY float64 `json:"bottomRight_y"`
	} `json:"rect"`
}

// IosOcrUploadResponse mirrors the JSON response from the iOS OCR Server
type IosOcrUploadResponse struct {
	Success     bool        `json:"success"`
	Message     string      `json:"message"`
	OcrResult   string      `json:"ocr_result"`
	ImageWidth  int         `json:"image_width"`
	ImageHeight int         `json:"image_height"`
	OcrBoxes    interface{} `json:"ocr_boxes"`
}

// newIosOcrProvider creates a new IosOcrProvider with the given configuration.
func newIosOcrProvider(config Config) (*IosOcrProvider, error) {
	logger := log.WithFields(logrus.Fields{
		"server_url": config.IosOcrServerURL,
	})
	logger.Info("Creating new iOS OCR Server provider")

	if config.IosOcrServerURL == "" {
		return nil, fmt.Errorf("missing required iOS OCR Server URL")
	}

	timeout := defaultIosOcrTimeout
	if config.IosOcrServerTimeout > 0 {
		timeout = config.IosOcrServerTimeout
	}

	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	// Normalize server URL: strip trailing slash for consistent URL building
	serverURL := strings.TrimRight(config.IosOcrServerURL, "/")

	provider := &IosOcrProvider{
		serverURL:  serverURL,
		httpClient: client,
		enableHOCR: config.EnableHOCR,
		hocrPages:  make([]hocr.Page, 0),
	}

	logger.WithField("enable_hocr", config.EnableHOCR).Info("Successfully initialized iOS OCR Server provider")
	return provider, nil
}

func (p *IosOcrProvider) ProcessImage(ctx context.Context, imageContent []byte, pageNumber int) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"provider": "ios_ocr",
		"url":      p.serverURL,
		"page":     pageNumber,
	})
	logger.Debug("Starting iOS OCR Server processing")

	uploadURL := p.serverURL + "/upload"

	// Build multipart form request
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	part, err := writer.CreateFormFile("file", "document.png")
	if err != nil {
		logger.WithError(err).Error("Failed to create form file")
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	_, err = io.Copy(part, bytes.NewReader(imageContent))
	if err != nil {
		logger.WithError(err).Error("Failed to copy image content to form")
		return nil, fmt.Errorf("failed to copy image content to form: %w", err)
	}

	err = writer.Close()
	if err != nil {
		logger.WithError(err).Error("Failed to close multipart writer")
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", uploadURL, &requestBody)
	if err != nil {
		logger.WithError(err).Error("Failed to create HTTP request")
		return nil, fmt.Errorf("error creating iOS OCR request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	logger.WithField("url", uploadURL).Debug("Sending request to iOS OCR Server")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		logger.WithError(err).Error("Failed to send request to iOS OCR Server")
		return nil, fmt.Errorf("error sending request to iOS OCR Server: %w", err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		logger.WithError(err).Error("Failed to read response body")
		return nil, fmt.Errorf("error reading iOS OCR response body: %w", err)
	}
	respSize := len(respBodyBytes)

	if resp.StatusCode != http.StatusOK {
		logger.WithFields(logrus.Fields{
			"status_code":   resp.StatusCode,
			"response_size": respSize,
		}).Error("Received non-OK status from iOS OCR Server")
		return nil, fmt.Errorf("iOS OCR Server returned status %d (response size: %d bytes)", resp.StatusCode, respSize)
	}

	var ocrResp IosOcrUploadResponse
	if err := json.Unmarshal(respBodyBytes, &ocrResp); err != nil {
		logger.WithError(err).WithFields(logrus.Fields{
			"status_code":   resp.StatusCode,
			"response_size": respSize,
		}).Error("Failed to parse iOS OCR JSON response")
		return nil, fmt.Errorf("error parsing iOS OCR JSON response (status: %d, size: %d bytes): %w", resp.StatusCode, respSize, err)
	}

	if !ocrResp.Success {
		logger.WithFields(logrus.Fields{
			"message": ocrResp.Message,
		}).Error("iOS OCR Server returned failure")
		return nil, fmt.Errorf("iOS OCR Server processing failed: %s", ocrResp.Message)
	}

	result := &OCRResult{
		Text: ocrResp.OcrResult,
		Metadata: map[string]string{
			"provider":     "ios_ocr",
			"has_content":  fmt.Sprintf("%t", ocrResp.OcrResult != ""),
			"image_width":  fmt.Sprintf("%d", ocrResp.ImageWidth),
			"image_height": fmt.Sprintf("%d", ocrResp.ImageHeight),
		},
	}

	// Create hOCR page structure if enabled and boxes are available
	if p.enableHOCR && ocrResp.OcrBoxes != nil {
		boxes, err := parseOcrBoxes(ocrResp.OcrBoxes)
		if err != nil {
			logger.WithError(err).Warn("Failed to parse OCR boxes for hOCR generation")
		} else if len(boxes) > 0 {
			hocrPage := buildHOCRPage(boxes, ocrResp.OcrResult, pageNumber, ocrResp.ImageWidth, ocrResp.ImageHeight)
			p.mu.Lock()
			p.hocrPages = append(p.hocrPages, hocrPage)
			p.mu.Unlock()
			result.HOCRPage = &hocrPage
			logger.WithField("page_number", pageNumber).Info("Created hOCR page")
		}
	}

	logger.WithFields(logrus.Fields{
		"content_length": len(result.Text),
		"image_width":    ocrResp.ImageWidth,
		"image_height":   ocrResp.ImageHeight,
	}).Info("Successfully processed image with iOS OCR Server")

	return result, nil
}

// --- HOCRCapable interface implementation ---

// IsHOCREnabled returns whether hOCR generation is enabled
func (p *IosOcrProvider) IsHOCREnabled() bool {
	return p.enableHOCR
}

// GetHOCRPages returns the collected hOCR pages
func (p *IosOcrProvider) GetHOCRPages() []hocr.Page {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]hocr.Page, len(p.hocrPages))
	copy(result, p.hocrPages)
	return result
}

// GetHOCRDocument creates an hOCR document from the collected pages
func (p *IosOcrProvider) GetHOCRDocument() (*hocr.HOCR, error) {
	if !p.enableHOCR {
		return nil, fmt.Errorf("hOCR generation is not enabled")
	}

	p.mu.Lock()
	pages := make([]hocr.Page, len(p.hocrPages))
	copy(pages, p.hocrPages)
	p.mu.Unlock()

	sort.Slice(pages, func(i, j int) bool {
		return pages[i].PageNumber < pages[j].PageNumber
	})

	if len(pages) == 0 {
		return nil, fmt.Errorf("no hOCR pages collected")
	}

	doc := &hocr.HOCR{
		Title:    "iOS OCR Server",
		Language: "unknown",
		Metadata: map[string]string{
			"ocr-system":          "iOS OCR Server (Apple Vision)",
			"ocr-number-of-pages": fmt.Sprintf("%d", len(pages)),
			"ocr-capabilities":    "ocr_page ocr_line ocrx_word",
		},
		Pages: pages,
	}
	return doc, nil
}

// ResetHOCR clears the collected hOCR pages
func (p *IosOcrProvider) ResetHOCR() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hocrPages = make([]hocr.Page, 0)
}

// --- hOCR helper functions ---

// parseOcrBoxes attempts to parse the raw ocr_boxes field into a typed slice
func parseOcrBoxes(raw interface{}) ([]IosOcrBox, error) {
	if raw == nil {
		return nil, fmt.Errorf("ocr_boxes is null")
	}

	jsonData, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ocr_boxes: %w", err)
	}
	var boxes []IosOcrBox
	if err := json.Unmarshal(jsonData, &boxes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ocr_boxes: %w", err)
	}
	return boxes, nil
}

// buildHOCRPage converts a slice of OCR boxes into an hOCR Page structure.
// Words are sorted top-to-bottom, grouped into lines by Y-coordinate proximity,
// and each word becomes an hOCR word with its bounding box.
func buildHOCRPage(boxes []IosOcrBox, fullText string, pageNumber int, imgWidth, imgHeight int) hocr.Page {
	page := hocr.Page{
		ID:         fmt.Sprintf("page_%d", pageNumber),
		PageNumber: pageNumber,
		BBox:       hocr.NewBoundingBox(0, 0, float64(imgWidth), float64(imgHeight)),
		Metadata:   make(map[string]string),
	}

	if len(boxes) == 0 {
		return page
	}

	// Sort boxes top-to-bottom, then left-to-right
	sorted := make([]IosOcrBox, len(boxes))
	copy(sorted, boxes)
	sort.Slice(sorted, func(i, j int) bool {
		if math.Abs(sorted[i].Y-sorted[j].Y) < 1 {
			return sorted[i].X < sorted[j].X
		}
		return sorted[i].Y < sorted[j].Y
	})

	// Group into lines: a new line starts when Y difference > half the current box height
	var lines [][]IosOcrBox
	currentLine := []IosOcrBox{sorted[0]}

	for i := 1; i < len(sorted); i++ {
		prev := sorted[i-1]
		curr := sorted[i]
		if curr.Y-prev.Y > prev.H*0.5 {
			lines = append(lines, currentLine)
			currentLine = []IosOcrBox{curr}
		} else {
			currentLine = append(currentLine, curr)
		}
	}
	lines = append(lines, currentLine)

	// Convert each line group into an hocr.Line
	for lidx, lineBoxes := range lines {
		// Compute line bounding box from word extremes
		minX, minY := math.MaxFloat64, math.MaxFloat64
		maxX, maxY := -math.MaxFloat64, -math.MaxFloat64
		for _, b := range lineBoxes {
			if b.X < minX {
				minX = b.X
			}
			if b.Y < minY {
				minY = b.Y
			}
			if b.X+b.W > maxX {
				maxX = b.X + b.W
			}
			if b.Y+b.H > maxY {
				maxY = b.Y + b.H
			}
		}

		line := hocr.Line{
			ID:       fmt.Sprintf("line_%d_%d", pageNumber, lidx),
			BBox:     hocr.NewBoundingBox(minX, minY, maxX, maxY),
			Metadata: make(map[string]string),
		}

		for widx, b := range lineBoxes {
			word := hocr.Word{
				ID:   fmt.Sprintf("word_%d_%d_%d", pageNumber, lidx, widx),
				Text: b.Text,
				BBox: hocr.NewBoundingBox(b.X, b.Y, b.X+b.W, b.Y+b.H),
			}
			line.Words = append(line.Words, word)
		}

		page.Lines = append(page.Lines, line)
	}

	return page
}
