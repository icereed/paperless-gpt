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
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/sirupsen/logrus"
)

const (
	miniCPMProviderName = "minicpm_ocr"
	// Served model name from the official vLLM / SDK quickstart
	// (`--served-model-name minicpm-ocr`).
	miniCPMDefaultModel = "minicpm-ocr"
	// SDK engine.request_timeout_sec.
	miniCPMDefaultTimeoutSeconds = 300
	// SDK default prompt-map budget. The single-region API example uses 2048;
	// a whole rendered page is larger than one crop, so the default block
	// budget is the safer starting point. Raise MINICPM_OCR_MAX_TOKENS when a
	// page comes back with finish_reason=length.
	miniCPMDefaultMaxTokens = 8192

	// Official prompt for a text block, paragraph, title, or page
	// (MiniCPM-OCR recognition guide). Block prompts such as
	// "Table Recognition:" and "Formula Recognition:" belong to the SDK's
	// layout-and-crop pipeline, which this provider does not run.
	miniCPMPagePrompt = "Text Recognition:"

	// Official visual token compression. Sent on each request so a plain
	// `vllm serve --trust-request-mm-kwargs` does not also need
	// `--mm-processor-kwargs` at startup.
	miniCPMDefaultDownsample = "4x"

	// Sampling parameters from the SDK decode config and the official
	// image-recognition example. They keep OCR output close to greedy.
	miniCPMTemperature       = 0.01
	miniCPMTopP              = 0.001
	miniCPMTopK              = 1
	miniCPMRepetitionPenalty = 1.0
	miniCPMNoRepeatNgramSize = 100
)

// MiniCPMOCRProvider recognizes each rendered page by posting it to a
// MiniCPM-OCR 1.0 server's OpenAI-compatible chat completions endpoint
// (vLLM, SGLang, llama.cpp, or Ollama).
//
// MiniCPM-OCR's full document pipeline also runs PP-DocLayoutV3 and sends
// each crop with a block prompt. paperless-gpt already renders one image per
// page, so this provider sends that image with the official page prompt.
type MiniCPMOCRProvider struct {
	chatURL        string
	apiKey         string
	model          string
	prompt         string
	maxTokens      int
	downsampleMode string
	maxSliceNums   *int
	sendMMKwargs   bool
	httpClient     *http.Client
}

type miniCPMImageURL struct {
	URL string `json:"url"`
}

type miniCPMContentPart struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *miniCPMImageURL `json:"image_url,omitempty"`
}

type miniCPMMessage struct {
	Role    string               `json:"role"`
	Content []miniCPMContentPart `json:"content"`
}

type miniCPMChatTemplateKwargs struct {
	// Always serialized. Thinking must stay off: with it enabled the
	// transcription can land in reasoning_content, which this provider does
	// not read.
	EnableThinking bool `json:"enable_thinking"`
}

type miniCPMMMProcessorKwargs struct {
	DownsampleMode string `json:"downsample_mode,omitempty"`
	MaxSliceNums   *int   `json:"max_slice_nums,omitempty"`
}

type miniCPMChatRequest struct {
	Model              string                    `json:"model"`
	Messages           []miniCPMMessage          `json:"messages"`
	MaxTokens          int                       `json:"max_tokens"`
	Temperature        float64                   `json:"temperature"`
	TopP               float64                   `json:"top_p"`
	TopK               int                       `json:"top_k"`
	RepetitionPenalty  float64                   `json:"repetition_penalty"`
	NoRepeatNgramSize  int                       `json:"no_repeat_ngram_size"`
	Stream             bool                      `json:"stream"`
	ChatTemplateKwargs miniCPMChatTemplateKwargs `json:"chat_template_kwargs"`
	MMProcessorKwargs  *miniCPMMMProcessorKwargs `json:"mm_processor_kwargs,omitempty"`
}

// miniCPMStatusError is a non-2xx chat completion. retryAfter is set from a
// Retry-After header so retryAfterDelay can extend the backoff.
type miniCPMStatusError struct {
	status     int
	body       string
	retryAfter time.Duration
}

// Error reports the HTTP status and, when the server sent Retry-After, the
// delay in the form retryAfterDelay already parses.
func (e *miniCPMStatusError) Error() string {
	msg := fmt.Sprintf("MiniCPM-OCR request failed with status %d: %s", e.status, e.body)
	if e.retryAfter > 0 {
		msg += fmt.Sprintf("; retry-after-ms:%d", e.retryAfter.Milliseconds())
	}
	return msg
}

type miniCPMChatResponse struct {
	Error   json.RawMessage `json:"error"`
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
			Refusal string          `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
}

// newMiniCPMOCRProvider builds a provider from config, applying defaults for
// the model, timeout, token budget, page prompt, and downsample mode.
func newMiniCPMOCRProvider(config Config) (*MiniCPMOCRProvider, error) {
	chatURL, err := miniCPMChatCompletionsURL(config.MiniCPMBaseURL)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(config.MiniCPMModel)
	if model == "" {
		model = miniCPMDefaultModel
	}
	timeout := config.MiniCPMTimeout
	if timeout <= 0 {
		timeout = miniCPMDefaultTimeoutSeconds
	}
	maxTokens := config.MiniCPMMaxTokens
	if maxTokens <= 0 {
		maxTokens = miniCPMDefaultMaxTokens
	}
	prompt := strings.TrimSpace(config.MiniCPMPrompt)
	if prompt == "" {
		prompt = miniCPMPagePrompt
	}
	downsample, sendMM := miniCPMDownsampleMode(config.MiniCPMDownsampleMode)

	logger := log.WithFields(logrus.Fields{
		"provider":   miniCPMProviderName,
		"model":      model,
		"timeout_s":  timeout,
		"max_tokens": maxTokens,
		"mm_kwargs":  sendMM,
		"downsample": downsample,
	})
	logger.Info("Creating new MiniCPM-OCR provider")

	return &MiniCPMOCRProvider{
		chatURL:        chatURL,
		apiKey:         strings.TrimSpace(config.MiniCPMAPIKey),
		model:          model,
		prompt:         prompt,
		maxTokens:      maxTokens,
		downsampleMode: downsample,
		maxSliceNums:   config.MiniCPMMaxSliceNums,
		sendMMKwargs:   sendMM,
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}, nil
}

// miniCPMDownsampleMode returns the downsample mode to send and whether
// mm_processor_kwargs should be included. "off" omits the object entirely.
func miniCPMDownsampleMode(raw string) (string, bool) {
	mode := strings.TrimSpace(raw)
	if strings.EqualFold(mode, "off") {
		return "", false
	}
	if mode == "" {
		return miniCPMDefaultDownsample, true
	}
	return mode, true
}

// miniCPMChatCompletionsURL accepts the server root (http://host:8000), an
// OpenAI base that already includes /v1, or a full chat completions URL.
// Only the path is rewritten, so a query string stays intact.
func miniCPMChatCompletionsURL(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("missing required MiniCPM-OCR base URL (MINICPM_OCR_BASE_URL)")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("MINICPM_OCR_BASE_URL must be an http(s) URL")
	}
	path := strings.TrimRight(u.Path, "/")
	lowerPath := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lowerPath, "/chat/completions"):
	case strings.HasSuffix(lowerPath, "/v1"):
		path += "/chat/completions"
	default:
		path += "/v1/chat/completions"
	}
	u.Path = path
	u.RawPath = ""
	return u.String(), nil
}

// ProcessImage sends one rendered page to the MiniCPM-OCR chat endpoint and
// returns the recognized text. A length finish reason sets OcrLimitHit.
func (p *MiniCPMOCRProvider) ProcessImage(ctx context.Context, imageContent []byte, pageNumber int) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"provider":    miniCPMProviderName,
		"model":       p.model,
		"page_number": pageNumber,
		"data_size":   len(imageContent),
	})
	logger.Info("Processing image with MiniCPM-OCR provider")

	mimeType, err := miniCPMImageMIME(imageContent)
	if err != nil {
		logger.WithError(err).Error("Unsupported page content")
		return nil, err
	}

	reqBody := p.buildChatRequest(imageContent, mimeType)
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to encode MiniCPM-OCR request: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"mime_type":     mimeType,
		"base64_length": len(reqBody.Messages[0].Content[0].ImageURL.URL),
		"prompt":        p.prompt,
	}).Debug("Sending MiniCPM-OCR chat completion")

	body, err := p.postWithRetry(ctx, payload, logger)
	if err != nil {
		return nil, err
	}

	text, finishReason, genInfo, err := parseMiniCPMChatResponse(body)
	if err != nil {
		logger.WithError(err).WithField("response_body", truncateForLog(string(body), 500)).Error("MiniCPM-OCR response rejected")
		return nil, err
	}

	limitHit := finishReason == "length"
	if limitHit {
		logger.Warn("MiniCPM-OCR hit the output token limit; the page text may be truncated. Raise MINICPM_OCR_MAX_TOKENS.")
	}
	if strings.TrimSpace(text) == "" {
		logger.WithField("finish_reason", finishReason).Warn("MiniCPM-OCR returned no text for this page; keeping it blank")
	}

	logger.WithFields(logrus.Fields{
		"content_length": len(text),
		"finish_reason":  finishReason,
	}).Info("Successfully extracted text")

	return &OCRResult{
		Text: text,
		Metadata: map[string]string{
			"provider":      miniCPMProviderName,
			"model":         p.model,
			"mime_type":     mimeType,
			"page":          fmt.Sprintf("%d", pageNumber),
			"finish_reason": finishReason,
		},
		OcrLimitHit:    limitHit,
		GenerationInfo: genInfo,
	}, nil
}

// buildChatRequest encodes the page as a data URL, image first, then the prompt.
// mm_processor_kwargs is omitted when downsample mode is off.
func (p *MiniCPMOCRProvider) buildChatRequest(imageContent []byte, mimeType string) miniCPMChatRequest {
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(imageContent)
	req := miniCPMChatRequest{
		Model: p.model,
		Messages: []miniCPMMessage{{
			Role: "user",
			// Image first, then the prompt, matching the official example.
			Content: []miniCPMContentPart{
				{Type: "image_url", ImageURL: &miniCPMImageURL{URL: dataURL}},
				{Type: "text", Text: p.prompt},
			},
		}},
		MaxTokens:         p.maxTokens,
		Temperature:       miniCPMTemperature,
		TopP:              miniCPMTopP,
		TopK:              miniCPMTopK,
		RepetitionPenalty: miniCPMRepetitionPenalty,
		NoRepeatNgramSize: miniCPMNoRepeatNgramSize,
		Stream:            false,
		ChatTemplateKwargs: miniCPMChatTemplateKwargs{
			EnableThinking: false,
		},
	}
	if p.sendMMKwargs {
		req.MMProcessorKwargs = &miniCPMMMProcessorKwargs{
			DownsampleMode: p.downsampleMode,
			MaxSliceNums:   p.maxSliceNums,
		}
	}
	return req
}

// postWithRetry posts one chat completion, retrying 429, 5xx, and transient
// network errors with the same VISION_LLM_MAX_RETRIES / VISION_LLM_BACKOFF_MAX_WAIT
// budget the LLM OCR provider uses. Other 4xx responses are not retried.
// Retry-After extends the backoff via retryAfterDelay.
func (p *MiniCPMOCRProvider) postWithRetry(ctx context.Context, payload []byte, logger *logrus.Entry) ([]byte, error) {
	maxRetries := 8
	if v, convErr := strconv.Atoi(os.Getenv("VISION_LLM_MAX_RETRIES")); convErr == nil && v >= 0 {
		maxRetries = v
	}
	backoffMax := 90 * time.Second
	if d, parseErr := time.ParseDuration(os.Getenv("VISION_LLM_BACKOFF_MAX_WAIT")); parseErr == nil && d > 0 {
		backoffMax = d
	}

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		body, err := p.postOnce(ctx, payload)
		if err == nil {
			return body, nil
		}
		if !miniCPMTransient(err) || attempt >= maxRetries {
			logger.WithError(err).Error("MiniCPM-OCR request failed")
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
		logger.WithError(err).Warnf("Transient MiniCPM-OCR error, retrying in %s (attempt %d/%d)", backoff, attempt+1, maxRetries)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
}

// postOnce performs a single chat completion. Non-2xx responses become a
// miniCPMStatusError. Network errors omit the request URL so credentials in
// it cannot leak. A canceled context is returned as ctx.Err().
func (p *MiniCPMOCRProvider) postOnce(ctx context.Context, payload []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.chatURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create MiniCPM-OCR request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			if errors.Is(urlErr.Err, context.Canceled) {
				return nil, context.Canceled
			}
			// Drop the URL so a credential in MINICPM_OCR_BASE_URL cannot leak
			// through a dial error. The inner error is still a network failure.
			return nil, fmt.Errorf("MiniCPM-OCR request failed: %v", urlErr.Err)
		}
		return nil, fmt.Errorf("MiniCPM-OCR request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("failed to read MiniCPM-OCR response: %w", err)
	}
	if resp.StatusCode == http.StatusOK {
		return body, nil
	}
	statusErr := &miniCPMStatusError{
		status: resp.StatusCode,
		body:   truncateForLog(string(body), 500),
	}
	if delay, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok && delay > 0 {
		statusErr.retryAfter = delay
	}
	return nil, statusErr
}

// miniCPMTransient reports whether err is worth retrying. HTTP 429 and 5xx
// are transient. Other 4xx responses and context cancellation are not.
// Remaining errors are treated as transient network failures.
func miniCPMTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var statusErr *miniCPMStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status == http.StatusTooManyRequests || statusErr.status >= 500
	}
	return true
}

// miniCPMImageMIME returns the data-URL media type for a page image.
// PDFs and empty input are rejected. Unrecognized bytes are labeled JPEG,
// matching the image-mode renderer.
func miniCPMImageMIME(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("empty image content")
	}
	detected := mimetype.Detect(data).String()
	if detected == "application/pdf" || bytes.HasPrefix(data, []byte("%PDF")) {
		return "", fmt.Errorf("MiniCPM-OCR accepts page images, not PDFs (set OCR_PROCESS_MODE=image); got %s", detected)
	}
	if strings.HasPrefix(detected, "image/") {
		return detected, nil
	}
	// The image OCR pipeline re-encodes every page as JPEG and labels it
	// image/jpeg. Bytes the sniffer does not recognize follow that label.
	return "image/jpeg", nil
}

// parseMiniCPMChatResponse reads the assistant text, finish reason, and token
// usage from a chat completion body. Refusals and API error objects are errors.
func parseMiniCPMChatResponse(body []byte) (string, string, map[string]interface{}, error) {
	var parsed miniCPMChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", nil, fmt.Errorf("failed to parse MiniCPM-OCR response: %w", err)
	}
	if miniCPMErrorPresent(parsed.Error) {
		return "", "", nil, fmt.Errorf("MiniCPM-OCR error: %s", truncateForLog(string(parsed.Error), 500))
	}
	if len(parsed.Choices) == 0 {
		return "", "", nil, fmt.Errorf("MiniCPM-OCR response contained no choices")
	}

	if parsed.Choices[0].Message.Refusal != "" {
		return "", "", nil, fmt.Errorf("MiniCPM-OCR refused the request")
	}
	text, err := miniCPMMessageText(parsed.Choices[0].Message.Content)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to read MiniCPM-OCR message content: %w", err)
	}
	text = strings.TrimSpace(text)

	genInfo := map[string]interface{}{
		"finish_reason": parsed.Choices[0].FinishReason,
	}
	if parsed.Usage.PromptTokens != 0 || parsed.Usage.CompletionTokens != 0 || parsed.Usage.TotalTokens != 0 {
		genInfo["prompt_tokens"] = parsed.Usage.PromptTokens
		genInfo["completion_tokens"] = parsed.Usage.CompletionTokens
		genInfo["total_tokens"] = parsed.Usage.TotalTokens
	}
	return text, parsed.Choices[0].FinishReason, genInfo, nil
}

// miniCPMErrorPresent reports whether the response included a non-null error.
func miniCPMErrorPresent(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

// miniCPMMessageText accepts either a string or an array of content parts.
// Text parts are joined. An array with no text part is an error, not a blank page.
func miniCPMMessageText(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		textParts := 0
		for _, part := range parts {
			// A missing type with a text field is still text. Anything else
			// (refusal, image, and so on) is not a transcription.
			if part.Type != "" && part.Type != "text" {
				continue
			}
			textParts++
			if part.Text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(part.Text)
		}
		if b.Len() == 0 && textParts == 0 && len(parts) > 0 {
			return "", fmt.Errorf("unsupported content parts")
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("unsupported content shape: %s", truncateForLog(trimmed, 200))
}

// truncateForLog shortens s to limit bytes and marks the cut, for logs and errors.
func truncateForLog(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "... [truncated]"
}
