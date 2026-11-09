package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/sirupsen/logrus"
	"paperless-gpt/internal/textsanitize"
	"paperless-gpt/sanitize"
)

// Request defaults from the official MiniCPM-OCR vLLM deployment guide:
// the decoding parameter set is the validated one, "4x" downsampling keeps
// text-dense pages readable, and enable_thinking must stay off or the
// transcription ends up in reasoning_content while content comes back empty.
const (
	minicpmDefaultModel             = "minicpm-ocr"
	minicpmDefaultPrompt            = "Text Recognition:"
	minicpmDefaultMaxTokens         = 8192
	minicpmDefaultTimeoutSeconds    = 180
	minicpmDefaultMaxSliceNums      = 9
	minicpmDefaultDownsampleMode    = "4x"
	minicpmDefaultTemperature       = 0.01
	minicpmDefaultTopP              = 0.001
	minicpmDefaultTopK              = 1
	minicpmDefaultRepetitionPenalty = 1.0
	minicpmDefaultNoRepeatNgramSize = 100
	minicpmDefaultMaxRetries        = 8
	minicpmDefaultBackoffMax        = 90 * time.Second
)

// MiniCPMOCRProvider implements the OCR Provider interface against a
// self-hosted MiniCPM-OCR service (vLLM/SGLang/llama.cpp/Ollama, any
// OpenAI-compatible /v1/chat/completions endpoint).
type MiniCPMOCRProvider struct {
	endpoint       string
	apiKey         string
	model          string
	prompt         string
	maxTokens      int
	timeout        time.Duration
	maxSliceNums   int
	downsampleMode string
}

// minicpmChatRequest is the chat-completions body. The mm_processor_kwargs
// and chat_template_kwargs fields are vLLM extensions: the server must be
// started with --trust-request-mm-kwargs or it rejects the request.
type minicpmChatRequest struct {
	Model              string                 `json:"model"`
	Messages           []minicpmMessage       `json:"messages"`
	MaxTokens          int                    `json:"max_tokens"`
	Temperature        float64                `json:"temperature"`
	TopP               float64                `json:"top_p"`
	TopK               int                    `json:"top_k"`
	RepetitionPenalty  float64                `json:"repetition_penalty"`
	NoRepeatNgramSize  int                    `json:"no_repeat_ngram_size"`
	ChatTemplateKwargs map[string]bool        `json:"chat_template_kwargs"`
	MMProcessorKwargs  map[string]interface{} `json:"mm_processor_kwargs"`
}

type minicpmMessage struct {
	Role    string        `json:"role"`
	Content []minicpmPart `json:"content"`
}

// The image part must come before the text part; the model's own client sends
// this order and the region prompts assume it.
type minicpmPart struct {
	Type     string           `json:"type"`
	ImageURL *minicpmImageURL `json:"image_url,omitempty"`
	Text     string           `json:"text,omitempty"`
}

type minicpmImageURL struct {
	URL string `json:"url"`
}

type minicpmChatResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// newMiniCPMProvider creates a new MiniCPM OCR provider instance,
// applying defaults for everything but the endpoint.
func newMiniCPMProvider(config Config) (Provider, error) {
	if config.MiniCPMEndpoint == "" {
		return nil, fmt.Errorf("MiniCPM OCR endpoint is not configured (MINICPM_OCR_ENDPOINT)")
	}

	p := &MiniCPMOCRProvider{
		endpoint: strings.TrimRight(config.MiniCPMEndpoint, "/"),
		apiKey:   config.MiniCPMAPIKey,
	}
	if p.endpoint == "" {
		return nil, fmt.Errorf("MiniCPM OCR endpoint is not configured (MINICPM_OCR_ENDPOINT)")
	}

	p.model = strings.TrimSpace(config.MiniCPMModel)
	if p.model == "" {
		p.model = minicpmDefaultModel
	}

	p.prompt = config.MiniCPMPrompt
	if strings.TrimSpace(p.prompt) == "" {
		p.prompt = minicpmDefaultPrompt
	}

	p.maxTokens = config.MiniCPMMaxTokens
	if p.maxTokens <= 0 {
		p.maxTokens = minicpmDefaultMaxTokens
	}

	timeoutSeconds := config.MiniCPMTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = minicpmDefaultTimeoutSeconds
	}
	p.timeout = time.Duration(timeoutSeconds) * time.Second

	p.maxSliceNums = config.MiniCPMMaxSliceNums
	if p.maxSliceNums <= 0 {
		p.maxSliceNums = minicpmDefaultMaxSliceNums
	}

	p.downsampleMode = strings.TrimSpace(config.MiniCPMDownsampleMode)
	if p.downsampleMode == "" {
		p.downsampleMode = minicpmDefaultDownsampleMode
	}

	log.WithFields(logrus.Fields{
		"model":           p.model,
		"max_slice_nums":  p.maxSliceNums,
		"downsample_mode": p.downsampleMode,
	}).Info("Using MiniCPM OCR provider")
	return p, nil
}

// ProcessImage sends one page image to the MiniCPM-OCR service and returns
// the recognized text. Transient upstream errors (429/5xx) are retried with
// the same VISION_LLM_* retry settings the LLM OCR provider uses.
func (p *MiniCPMOCRProvider) ProcessImage(ctx context.Context, imageContent []byte, pageNumber int) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"provider": "minicpm",
		"model":    p.model,
		"page":     pageNumber,
	})
	logger.Debug("Starting MiniCPM OCR processing")

	mtype := mimetype.Detect(imageContent)
	contentType := mtype.String()
	if !strings.HasPrefix(contentType, "image/") {
		return nil, fmt.Errorf("MiniCPM OCR expected an image, got %s", contentType)
	}

	request := minicpmChatRequest{
		Model: p.model,
		Messages: []minicpmMessage{{
			Role: "user",
			Content: []minicpmPart{
				{Type: "image_url", ImageURL: &minicpmImageURL{
					URL: "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(imageContent),
				}},
				{Type: "text", Text: sanitize.Sanitize(p.prompt)},
			},
		}},
		MaxTokens:          p.maxTokens,
		Temperature:        minicpmDefaultTemperature,
		TopP:               minicpmDefaultTopP,
		TopK:               minicpmDefaultTopK,
		RepetitionPenalty:  minicpmDefaultRepetitionPenalty,
		NoRepeatNgramSize:  minicpmDefaultNoRepeatNgramSize,
		ChatTemplateKwargs: map[string]bool{"enable_thinking": false},
		MMProcessorKwargs: map[string]interface{}{
			"downsample_mode": p.downsampleMode,
			"max_slice_nums":  p.maxSliceNums,
		},
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("error marshaling MiniCPM OCR request: %w", err)
	}

	maxRetries := minicpmDefaultMaxRetries
	if v, convErr := strconv.Atoi(os.Getenv("VISION_LLM_MAX_RETRIES")); convErr == nil && v >= 0 {
		maxRetries = v
	}
	backoffMax := minicpmDefaultBackoffMax
	if d, parseErr := time.ParseDuration(os.Getenv("VISION_LLM_BACKOFF_MAX_WAIT")); parseErr == nil && d > 0 {
		backoffMax = d
	}

	var result *OCRResult
	for attempt := 0; ; attempt++ {
		result, err = p.sendRequest(ctx, body, logger)
		if err == nil {
			break
		}
		if !isMiniCPMTransientError(err) || attempt >= maxRetries {
			logger.WithError(err).Error("Failed to get response from MiniCPM OCR")
			return nil, err
		}
		backoff := time.Duration(1<<uint(attempt)) * time.Second
		if backoff <= 0 || backoff > backoffMax {
			backoff = backoffMax
		}
		backoff = retryAfterDelay(err, backoff)
		if backoff > maxProviderRetryDelay {
			return nil, fmt.Errorf("provider retry delay %s exceeds limit", backoff)
		}
		logger.WithError(err).Warnf("Transient MiniCPM OCR error, retrying in %s (attempt %d/%d)", backoff, attempt+1, maxRetries)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}

	return result, nil
}

// sendRequest performs one HTTP round trip and decodes the response.
func (p *MiniCPMOCRProvider) sendRequest(ctx context.Context, body []byte, logger *logrus.Entry) (*OCRResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("error creating MiniCPM OCR request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// vLLM deployments without --api-key reject a bogus bearer token,
	// so only send one when the operator configured it.
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending MiniCPM OCR request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("error reading MiniCPM OCR response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("MiniCPM OCR API error (status %d): %s", resp.StatusCode, truncateForLog(respBody, 512))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, &miniCPMTransientError{err: err}
		}
		return nil, err
	}

	var parsed minicpmChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("error decoding MiniCPM OCR response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("MiniCPM OCR API error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("MiniCPM OCR response contains no choices")
	}

	choice := parsed.Choices[0]
	// content may be a plain string or an array of parts; the model returns a
	// plain string, but tolerate the array form some gateways emit.
	text := decodeMiniCPMContent(choice.Message.Content)

	text = textsanitize.StripCodeFences(textsanitize.StripReasoning(text))
	text = strings.TrimSpace(text)

	limitHit := false
	switch choice.FinishReason {
	case "stop", "":
		// Complete (or legacy servers that omit the field).
	case "length":
		// The page was cut off by max_tokens; keep what was recognized and
		// flag it so callers can raise MINICPM_OCR_MAX_TOKENS.
		limitHit = true
		logger.WithField("max_tokens", p.maxTokens).Warn("MiniCPM OCR hit the max token limit; output may be truncated")
	default:
		return nil, fmt.Errorf("MiniCPM OCR finished with %q before completing the page", choice.FinishReason)
	}

	if text == "" {
		logger.WithField("finish_reason", choice.FinishReason).Warn("MiniCPM OCR returned no text for this page; keeping it blank")
	}

	return &OCRResult{
		Text: text,
		Metadata: map[string]string{
			"provider": "minicpm",
			"model":    p.model,
		},
		OcrLimitHit: limitHit,
	}, nil
}

// miniCPMTransientError marks 429/5xx responses as retryable.
type miniCPMTransientError struct {
	err error
}

func (e *miniCPMTransientError) Error() string { return e.err.Error() }
func (e *miniCPMTransientError) Unwrap() error { return e.err }

func isMiniCPMTransientError(err error) bool {
	var transient *miniCPMTransientError
	return errors.As(err, &transient)
}

// decodeMiniCPMContent extracts the text from either the string form of
// message.content or the multimodal parts array.
func decodeMiniCPMContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		chunks := make([]string, 0, len(parts))
		for _, part := range parts {
			if part.Text != "" {
				chunks = append(chunks, part.Text)
			}
		}
		return strings.Join(chunks, "")
	}
	return ""
}

func truncateForLog(data []byte, limit int) string {
	if len(data) <= limit {
		return string(data)
	}
	return string(data[:limit]) + "…"
}
