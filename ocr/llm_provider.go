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
	var model llms.Model
	var err error

	switch strings.ToLower(config.VisionLLMProvider) {
	case "openai":
		model, err = createOpenAIClient(config)
	case "ollama":
		model, err = createOllamaClient(config)
	default:
		return nil, fmt.Errorf("unsupported vision LLM provider: %s", config.VisionLLMProvider)
	}

	if err != nil {
		return nil, fmt.Errorf("error creating vision LLM client: %w", err)
	}

	return &LLMProvider{
		provider: config.VisionLLMProvider,
		model:    config.VisionLLMModel,
		llm:      model,
		template: defaultOCRPrompt,
	}, nil
}

// createOpenAIClient creates a new OpenAI vision model client
func createOpenAIClient(config Config) (llms.Model, error) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		return nil, fmt.Errorf("OpenAI API key is not set")
	}
	return openai.New(
		openai.WithModel(config.VisionLLMModel),
		openai.WithToken(os.Getenv("OPENAI_API_KEY")),
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

func (p *LLMProvider) ProcessImage(ctx context.Context, imageContent []byte) (string, error) {
	// Decode image to validate format and get dimensions for logging
	_, _, err := image.Decode(bytes.NewReader(imageContent))
	if err != nil {
		return "", fmt.Errorf("error decoding image: %w", err)
	}

	// Prepare content parts based on provider type
	var parts []llms.ContentPart
	if strings.ToLower(p.provider) != "openai" {
		parts = []llms.ContentPart{
			llms.BinaryPart("image/jpeg", imageContent),
			llms.TextPart(p.template),
		}
	} else {
		base64Image := base64.StdEncoding.EncodeToString(imageContent)
		parts = []llms.ContentPart{
			llms.ImageURLPart(fmt.Sprintf("data:image/jpeg;base64,%s", base64Image)),
			llms.TextPart(p.template),
		}
	}

	// Convert the image to text
	completion, err := p.llm.GenerateContent(ctx, []llms.MessageContent{
		{
			Parts: parts,
			Role:  llms.ChatMessageTypeHuman,
		},
	})
	if err != nil {
		return "", fmt.Errorf("error getting response from LLM: %w", err)
	}

	return completion.Choices[0].Content, nil
}

const defaultOCRPrompt = `Just transcribe the text in this image and preserve the formatting and layout (high quality OCR). Do that for ALL the text in the image. Be thorough and pay attention. This is very important. The image is from a text document so be sure to continue until the bottom of the page. Thanks a lot! You tend to forget about some text in the image so please focus! Use markdown format but without a code block.`
