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
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

// LLMProvider implements OCR using LLM vision models
type LLMProvider struct {
	provider string
	model    string
	llm      llms.Model
	template string // OCR prompt template
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
		template: defaultOCRPrompt,
	}, nil
}

func (p *LLMProvider) ProcessImage(ctx context.Context, imageContent []byte) (string, error) {
	logger := log.WithFields(logrus.Fields{
		"provider": p.provider,
		"model":    p.model,
	})
	logger.Debug("Starting OCR processing")

	// Log the image dimensions
	img, _, err := image.Decode(bytes.NewReader(imageContent))
	if err != nil {
		logger.WithError(err).Error("Failed to decode image")
		return "", fmt.Errorf("error decoding image: %w", err)
	}
	bounds := img.Bounds()
	logger.WithFields(logrus.Fields{
		"width":  bounds.Dx(),
		"height": bounds.Dy(),
	}).Debug("Image dimensions")

	// Prepare content parts based on provider type
	var parts []llms.ContentPart
	if strings.ToLower(p.provider) != "openai" {
		logger.Debug("Using binary image format for non-OpenAI provider")
		parts = []llms.ContentPart{
			llms.BinaryPart("image/jpeg", imageContent),
			llms.TextPart(p.template),
		}
	} else {
		logger.Debug("Using base64 image format for OpenAI provider")
		base64Image := base64.StdEncoding.EncodeToString(imageContent)
		parts = []llms.ContentPart{
			llms.ImageURLPart(fmt.Sprintf("data:image/jpeg;base64,%s", base64Image)),
			llms.TextPart(p.template),
		}
	}

	// Convert the image to text
	logger.Debug("Sending request to vision model")
	completion, err := p.llm.GenerateContent(ctx, []llms.MessageContent{
		{
			Parts: parts,
			Role:  llms.ChatMessageTypeHuman,
		},
	})
	if err != nil {
		logger.WithError(err).Error("Failed to get response from vision model")
		return "", fmt.Errorf("error getting response from LLM: %w", err)
	}

	logger.WithField("content_length", len(completion.Choices[0].Content)).Info("Successfully processed image")
	return completion.Choices[0].Content, nil
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

const defaultOCRPrompt = `Just transcribe the text in this image and preserve the formatting and layout (high quality OCR). Do that for ALL the text in the image. Be thorough and pay attention. This is very important. The image is from a text document so be sure to continue until the bottom of the page. Thanks a lot! You tend to forget about some text in the image so please focus! Use markdown format but without a code block.`
