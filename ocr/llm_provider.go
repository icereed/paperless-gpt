package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"regexp"
	"strings"

	_ "image/jpeg"

	"github.com/sirupsen/logrus"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/mistral"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

// OCRResponse represents the structured JSON response from LLM OCR
type OCRResponse struct {
	IntroComment  *string `json:"intro_comment,omitempty"`  // Optional initial thoughts about the document
	Content       string  `json:"content"`                  // The actual transcribed text content
	FinishComment *string `json:"finish_comment,omitempty"` // Optional final observations
}

// LLMProvider implements OCR using LLM vision models
type LLMProvider struct {
	provider string
	model    string
	llm      llms.Model
	prompt   string // OCR prompt template
}

func newLLMProvider(config Config) (*LLMProvider, error) {
	logger := log.WithFields(logrus.Fields{
		"provider": config.VisionLLMProvider,
		"model":    config.VisionLLMModel,
	})
	logger.Info("Creating new LLM OCR provider")

	var model llms.Model
	var err error

	switch strings.ToLower(config.VisionLLMProvider) {
	case "openai":
		logger.Debug("Initializing OpenAI vision model")
		model, err = createOpenAIClient(config)
	case "ollama":
		logger.Debug("Initializing Ollama vision model")
		model, err = createOllamaClient(config)
	case "mistral":
		logger.Debug("Initializing Mistral vision model")
		model, err = createMistralClient(config)
	default:
		return nil, fmt.Errorf("unsupported vision LLM provider: %s", config.VisionLLMProvider)
	}

	if err != nil {
		logger.WithError(err).Error("Failed to create vision LLM client")
		return nil, fmt.Errorf("error creating vision LLM client: %w", err)
	}

	logger.Info("Successfully initialized LLM OCR provider")
	return &LLMProvider{
		provider: config.VisionLLMProvider,
		model:    config.VisionLLMModel,
		llm:      model,
		prompt:   config.VisionLLMPrompt,
	}, nil
}

func (p *LLMProvider) ProcessImage(ctx context.Context, imageContent []byte, pageNumber int) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"provider": p.provider,
		"model":    p.model,
		"page":     pageNumber,
	})
	logger.Debug("Starting LLM OCR processing")

	// Log the image dimensions
	img, _, err := image.Decode(bytes.NewReader(imageContent))
	if err != nil {
		logger.WithError(err).Error("Failed to decode image")
		return nil, fmt.Errorf("error decoding image: %w", err)
	}
	bounds := img.Bounds()
	logger.WithFields(logrus.Fields{
		"width":  bounds.Dx(),
		"height": bounds.Dy(),
	}).Debug("Image dimensions")

	logger.Debugf("Prompt: %s", p.prompt)

	// Prepare content parts based on provider type
	var parts []llms.ContentPart

	var imagePart llms.ContentPart
	providerName := strings.ToLower(p.provider)

	if providerName == "openai" || providerName == "mistral" {
		logger.Info("Using OpenAI image format")
		imagePart = llms.ImageURLPart("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(imageContent))
	} else {
		logger.Info("Using binary image format")
		imagePart = llms.BinaryPart("image/jpeg", imageContent)
	}

	parts = []llms.ContentPart{
		imagePart,
		llms.TextPart(p.prompt),
	}

	messages := []llms.MessageContent{
		{
			Parts: parts,
			Role:  llms.ChatMessageTypeHuman,
		},
	}

	var options []llms.CallOption
	if isStructuredOutputEnabled() {
		options = append(options, llms.WithJSONMode())
		logger.Debug("Using structured output (JSON mode)")
	}

	// Convert the image to text
	logger.Debug("Sending request to vision model")
	completion, err := p.llm.GenerateContent(ctx, messages, options...)
	if err != nil {
		logger.WithError(err).Error("Failed to get response from vision model")
		return nil, fmt.Errorf("error getting response from LLM: %w", err)
	}

	response := strings.TrimSpace(completion.Choices[0].Content)
	var extractedContent string

	// Check if structured output is enabled
	useStructured := isStructuredOutputEnabled()

	// Parse structured response if enabled
	if useStructured {
		var ocrResp OCRResponse
		if err := parseStructuredResponse(response, &ocrResp); err == nil {
			extractedContent = ocrResp.Content
			logger.Debug("Successfully parsed structured OCR response")
		} else {
			logger.WithError(err).Warn("Failed to parse structured OCR response, falling back to full response")
			extractedContent = response
		}
	} else {
		extractedContent = response
	}

	// Apply reasoning removal in all cases
	extractedContent = stripReasoning(extractedContent)

	result := &OCRResult{
		Text: extractedContent,
		Metadata: map[string]string{
			"provider": p.provider,
			"model":    p.model,
		},
	}
	logger.WithField("content_length", len(result.Text)).Info("Successfully processed image")
	return result, nil
}

// createOpenAIClient creates a new OpenAI vision model client
func createOpenAIClient(config Config) (llms.Model, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key is not set")
	}
	return openai.New(
		openai.WithModel(config.VisionLLMModel),
		openai.WithToken(apiKey),
	)
}

// createOllamaClient creates a new Ollama vision model client
func createOllamaClient(config Config) (llms.Model, error) {
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "http://127.0.0.1:11434"
	}
	return ollama.New(
		ollama.WithModel(config.VisionLLMModel),
		ollama.WithServerURL(host),
	)
}

// createMistralClient creates a new Mistral vision model client
func createMistralClient(config Config) (llms.Model, error) {
	apiKey := os.Getenv("MISTRAL_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("Mistral API key is not set")
	}
	return mistral.New(
		mistral.WithModel(config.VisionLLMModel),
		mistral.WithAPIKey(apiKey),
	)
}

// isStructuredOutputEnabled checks if structured output should be used
func isStructuredOutputEnabled() bool {
	return os.Getenv("OLLAMA_STRUCTURED_OUTPUT") == "true" || os.Getenv("STRUCTURED_OUTPUT_ENABLED") == "true"
}

// parseStructuredResponse attempts to parse JSON response, falls back to text if needed
func parseStructuredResponse(response string, target interface{}) error {
	if err := json.Unmarshal([]byte(response), target); err != nil {
		return fmt.Errorf("failed to parse structured response: %w", err)
	}
	return nil
}

// stripReasoning removes reasoning patterns from LLM responses.
// This handles various reasoning formats including XML-style tags, 
// reasoning prefixes, and other common patterns using regex for robust parsing.
func stripReasoning(content string) string {
	// Remove <think> and </think> XML-style tags (case insensitive, multiline)
	// This regex matches opening and closing think tags with any content in between,
	// including newlines, and removes the entire block
	thinkRegex := regexp.MustCompile(`(?i)<think>.*?</think>`)
	content = thinkRegex.ReplaceAllString(content, "")

	// Remove reasoning patterns at the beginning of lines
	// Common reasoning prefixes that should be stripped
	reasoningPatterns := []string{
		`(?i)^\s*Let me think about this.*$`,
		`(?i)^\s*Let me analyze.*$`,
		`(?i)^\s*I think.*$`,
		`(?i)^\s*I believe.*$`,
		`(?i)^\s*In my opinion.*$`,
		`(?i)^\s*It seems.*$`,
		`(?i)^\s*Looking at this.*$`,
		`(?i)^\s*Based on my analysis.*$`,
		`(?i)^\s*After analyzing.*$`,
		`(?i)^\s*Upon review.*$`,
		`(?i)^\s*My reasoning is.*$`,
		`(?i)^\s*The reasoning behind.*$`,
		`(?i)^\s*Here's my thinking.*$`,
		`(?i)^\s*My thought process.*$`,
	}

	// Apply each reasoning pattern to remove matching lines
	for _, pattern := range reasoningPatterns {
		regex := regexp.MustCompile(pattern)
		content = regex.ReplaceAllString(content, "")
	}

	// Clean up multiple consecutive newlines and trim whitespace
	content = regexp.MustCompile(`\n\s*\n`).ReplaceAllString(content, "\n")
	content = strings.TrimSpace(content)
	
	return content
}
