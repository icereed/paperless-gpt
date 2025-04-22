package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/sirupsen/logrus"
)

const (
	apiVersion                 = "2024-11-30"
	defaultModelID             = "prebuilt-read"
	defaultTimeout             = 120
	pollingInterval            = 2 * time.Second
	defaultOutputContentFormat = "text"
)

// AzureProvider implements OCR using Azure Document Intelligence
type AzureProvider struct {
	endpoint            string
	apiKey              string
	modelID             string
	timeout             time.Duration
	httpClient          *retryablehttp.Client
	outputContentFormat string
}

// Request body for Azure Document Intelligence
type analyzeRequest struct {
	Base64Source string `json:"base64Source"`
}

func newAzureProvider(config Config) (*AzureProvider, error) {
	logger := log.WithFields(logrus.Fields{
		"endpoint": config.AzureEndpoint,
		"model_id": config.AzureModelID,
	})
	logger.Info("Creating new Azure Document Intelligence provider")

	// Validate required configuration
	if config.AzureEndpoint == "" || config.AzureAPIKey == "" {
		logger.Error("Missing required configuration")
		return nil, fmt.Errorf("missing required Azure Document Intelligence configuration")
	}

	// Set defaults and create provider
	modelID := defaultModelID
	if config.AzureModelID != "" {
		modelID = config.AzureModelID
	}

	// Set default output content format
	outputContentFormat := defaultOutputContentFormat
	if config.AzureOutputContentFormat != "" {
		outputContentFormat = config.AzureOutputContentFormat
	}

	timeout := defaultTimeout
	if config.AzureTimeout > 0 {
		timeout = config.AzureTimeout
	}

	// Configure retryablehttp client
	client := retryablehttp.NewClient()
	client.RetryMax = 3
	client.RetryWaitMin = 1 * time.Second
	client.RetryWaitMax = 5 * time.Second
	client.Logger = logger

	provider := &AzureProvider{
		endpoint:            config.AzureEndpoint,
		apiKey:              config.AzureAPIKey,
		modelID:             modelID,
		timeout:             time.Duration(timeout) * time.Second,
		httpClient:          client,
		outputContentFormat: outputContentFormat,
	}

	logger.Info("Successfully initialized Azure Document Intelligence provider")
	return provider, nil
}

func (p *AzureProvider) ProcessImage(ctx context.Context, imageContent []byte, pageNumber int) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"model_id": p.modelID,
		"page":     pageNumber,
	})
	logger.Debug("Starting Azure Document Intelligence processing")

	// Detect MIME type
	mtype := mimetype.Detect(imageContent)
	logger.WithField("mime_type", mtype.String()).Debug("Detected file type")

	if !isImageMIMEType(mtype.String()) {
		logger.WithField("mime_type", mtype.String()).Error("Unsupported file type")
		return nil, fmt.Errorf("unsupported file type: %s", mtype.String())
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	// Submit document for analysis
	operationLocation, err := p.submitDocument(ctx, imageContent)
	if err != nil {
		return nil, fmt.Errorf("error submitting document: %w", err)
	}

	// Poll for results
	result, err := p.pollForResults(ctx, operationLocation)
	if err != nil {
		return nil, fmt.Errorf("error polling for results: %w", err)
	}

	// Convert to OCR result
	ocrResult := &OCRResult{
		Text: result.AnalyzeResult.Content,
		Metadata: map[string]string{
			"provider":    "azure_docai",
			"page_count":  fmt.Sprintf("%d", len(result.AnalyzeResult.Pages)),
			"api_version": result.AnalyzeResult.APIVersion,
		},
	}

	logger.WithFields(logrus.Fields{
		"content_length": len(ocrResult.Text),
		"page_count":     len(result.AnalyzeResult.Pages),
	}).Info("Successfully processed document")
	return ocrResult, nil
}

func (p *AzureProvider) submitDocument(ctx context.Context, imageContent []byte) (string, error) {
	outputFormatParam := ""
	if p.outputContentFormat != "text" {
		outputFormatParam = fmt.Sprintf("&outputContentFormat=%s", p.outputContentFormat)
	}
	requestURL := fmt.Sprintf("%s/documentintelligence/documentModels/%s:analyze?api-version=%s%s",
		p.endpoint, p.modelID, apiVersion, outputFormatParam)

	// Prepare request body
	requestBody := analyzeRequest{
		Base64Source: base64.StdEncoding.EncodeToString(imageContent),
	}
	requestBodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling request body: %w", err)
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, "POST", requestURL, bytes.NewBuffer(requestBodyBytes))
	if err != nil {
		return "", fmt.Errorf("error creating HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ocp-Apim-Subscription-Key", p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	operationLocation := resp.Header.Get("Operation-Location")
	if operationLocation == "" {
		return "", fmt.Errorf("no Operation-Location header in response")
	}

	return operationLocation, nil
}

func (p *AzureProvider) pollForResults(ctx context.Context, operationLocation string) (*AzureDocumentResult, error) {
	logger := log.WithField("operation_location", operationLocation)
	logger.Debug("Starting to poll for results")

	ticker := time.NewTicker(pollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("operation timed out after %v: %w", p.timeout, ctx.Err())
		case <-ticker.C:
			req, err := retryablehttp.NewRequestWithContext(ctx, "GET", operationLocation, nil)
			if err != nil {
				return nil, fmt.Errorf("error creating poll request: %w", err)
			}
			req.Header.Set("Ocp-Apim-Subscription-Key", p.apiKey)

			resp, err := p.httpClient.Do(req)
			if err != nil {
				return nil, fmt.Errorf("error polling for results: %w", err)
			}

			var result AzureDocumentResult
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				resp.Body.Close()
				logger.WithError(err).Error("Failed to decode response")
				return nil, fmt.Errorf("error decoding response: %w", err)
			}
			defer resp.Body.Close()

			logger.WithFields(logrus.Fields{
				"status_code":    resp.StatusCode,
				"content_length": len(result.AnalyzeResult.Content),
				"page_count":     len(result.AnalyzeResult.Pages),
				"status":         result.Status,
			}).Debug("Poll response received")

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("unexpected status code %d while polling", resp.StatusCode)
			}

			switch result.Status {
			case "succeeded":
				return &result, nil
			case "failed":
				return nil, fmt.Errorf("document processing failed")
			case "running":
			// Continue polling
			default:
				return nil, fmt.Errorf("unexpected status: %s", result.Status)
			}
		}
	}
}
