package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"os"
	"strings"

	_ "image/jpeg"

	"github.com/sirupsen/logrus"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/mistral"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

// LLMProvider implements OCR using LLM vision models
type LLMProvider struct {
	provider    string
	model       string
	llm         llms.Model
	prompt      string
	maxTokens   int
	temperature *float64
	ollamaTopK  *int
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
	case "tongyi":
		logger.Debug("Initializing Tongyi vision model")
		model, err = createTongyiClient(config)
	case "chatanywhere":
		logger.Debug("Initializing Chatanywhere vision model")
		model, err = createChatanywhereClient(config)
	default:
		return nil, fmt.Errorf("unsupported vision LLM provider: %s", config.VisionLLMProvider)
	}

	if err != nil {
		logger.WithError(err).Error("Failed to create vision LLM client")
		return nil, fmt.Errorf("error creating vision LLM client: %w", err)
	}

	logger.Info("Successfully initialized LLM OCR provider")
	return &LLMProvider{
		provider:    config.VisionLLMProvider,
		model:       config.VisionLLMModel,
		llm:         model,
		prompt:      config.VisionLLMPrompt,
		maxTokens:   config.VisionLLMMaxTokens,
		temperature: config.VisionLLMTemperature,
		ollamaTopK:  config.OllamaOcrTopK,
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

	if providerName == "openai" || providerName == "mistral" || providerName == "tongyi" || providerName == "chatanywhere" {
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

	var callOpts []llms.CallOption
	if p.maxTokens > 0 {
		callOpts = append(callOpts, llms.WithMaxTokens(p.maxTokens))
	}
	if p.temperature != nil {
		callOpts = append(callOpts, llms.WithTemperature(*p.temperature))
	}
	if providerName == "ollama" && p.ollamaTopK != nil {
		callOpts = append(callOpts, llms.WithTopK(*p.ollamaTopK))
	}

	// Convert the image to text
	logger.Debug("Sending request to vision model")
	completion, err := p.llm.GenerateContent(ctx, []llms.MessageContent{
		{
			Parts: parts,
			Role:  llms.ChatMessageTypeHuman,
		},
	}, callOpts...)
	if err != nil {
		logger.WithError(err).Error("Failed to get response from vision model")
		return nil, fmt.Errorf("error getting response from LLM: %w", err)
	}

	text := stripReasoning(completion.Choices[0].Content)
	limitHit := false
	tokenCount := -1

	if p.maxTokens > 0 {
		genInfo := completion.Choices[0].GenerationInfo
		if genInfo != nil && genInfo["TotalTokens"] != nil {
			if v, ok := genInfo["TotalTokens"].(int); ok {
				tokenCount = v
			}
		}
		// Fallback: count tokens using langchaingo (might not be accurate for all models)
		if tokenCount < 0 {
			tokenCount = llms.CountTokens(p.model, text)
		}
		if tokenCount >= p.maxTokens {
			limitHit = true
		}
	}

	result := &OCRResult{
		Text: text,
		Metadata: map[string]string{
			"provider": p.provider,
			"model":    p.model,
		},
		OcrLimitHit:    limitHit,
		GenerationInfo: completion.Choices[0].GenerationInfo,
	}

	logger.WithField("content_length", len(result.Text)).WithFields(completion.Choices[0].GenerationInfo).Info("Successfully processed image")
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
	opts := []ollama.Option{
		ollama.WithModel(config.VisionLLMModel),
		ollama.WithServerURL(host),
	}
	if config.OllamaContextLength > 0 {
		opts = append(opts, ollama.WithRunnerNumCtx(config.OllamaContextLength))
	}
	return ollama.New(opts...)
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

// createTongyiClient creates a new Tongyi vision model client
// Note: This uses the TongyiProvider from the parent package.
// We need to import it or define a bridge function in main.go
func createTongyiClient(config Config) (llms.Model, error) {
	apiKey := os.Getenv("TONGYI_API_KEY")
	endpoint := os.Getenv("TONGYI_ENDPOINT")
	if apiKey == "" {
		return nil, fmt.Errorf("Tongyi API key is not set")
	}
	if endpoint == "" {
		endpoint = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	// We can't directly import main.NewTongyiProvider from ocr package due to circular dependency
	// So we use openai.New with Tongyi's compatible endpoint
	return openai.New(
		openai.WithModel(config.VisionLLMModel),
		openai.WithToken(apiKey),
		openai.WithBaseURL(endpoint),
	)
}

// createChatanywhereClient creates a new Chatanywhere vision model client
func createChatanywhereClient(config Config) (llms.Model, error) {
	apiKey := os.Getenv("CHATANYWHERE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("Chatanywhere API key is not set")
	}
	baseURL := os.Getenv("CHATANYWHERE_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.chatanywhere.tech/v1"
	}
	return openai.New(
		openai.WithModel(config.VisionLLMModel),
		openai.WithToken(apiKey),
		openai.WithBaseURL(baseURL),
	)
}
