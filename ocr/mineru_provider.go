package ocr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	nethtml "golang.org/x/net/html"
)

const (
	mineruDefaultTier    = "standard"
	mineruDefaultTimeout = 600 * time.Second
	mineruPollInterval   = time.Second
	mineruMaxErrBody     = 2048
	// mineruMaxErrRead caps how much of an error (4xx/5xx) body is read; only
	// the first mineruMaxErrBody bytes end up in the error message anyway.
	mineruMaxErrRead = 16 << 10
	// mineruMaxResponse caps a successful response body. structured_content
	// for a long, image-heavy document is the largest payload and stays far
	// below this; anything bigger fails instead of exhausting memory.
	mineruMaxResponse = 256 << 20
	// mineruCancelTimeout bounds the best-effort job cancellation, which runs
	// on a fresh context because the caller's has usually already expired.
	mineruCancelTimeout = 5 * time.Second
)

var mineruValidTiers = map[string]bool{"flash": true, "basic": true, "standard": true, "advanced": true}

// MinerUProvider implements OCR using a self-hosted MinerU 4.x V1 parsing API.
type MinerUProvider struct {
	baseURL      string
	apiKey       string
	tier         string
	timeout      time.Duration // per ProcessImage call, so per page in image/pdf mode
	pollInterval time.Duration
	retryWait    time.Duration
	maxResponse  int64 // limit for successful response bodies, in bytes
	httpClient   *http.Client
}

// newMinerUProvider creates a new MinerU OCR provider.
func newMinerUProvider(config Config) (*MinerUProvider, error) {
	base := normalizeMinerUURL(config.MinerUURL)
	if base == "" {
		return nil, fmt.Errorf("missing required MinerU configuration (MINERU_URL)")
	}
	if err := validateMinerUURL(base); err != nil {
		// Never echo the raw value: it may carry user:password credentials.
		return nil, fmt.Errorf("invalid MINERU_URL %s: %w", redactURL(config.MinerUURL), err)
	}
	tier := strings.ToLower(strings.TrimSpace(config.MinerUTier))
	if tier == "" {
		tier = mineruDefaultTier
	}
	if !mineruValidTiers[tier] {
		return nil, fmt.Errorf("unsupported MinerU tier: %q (supported: flash, basic, standard, advanced)", config.MinerUTier)
	}
	timeout := mineruDefaultTimeout
	if config.MinerUTimeout > 0 {
		timeout = time.Duration(config.MinerUTimeout) * time.Second
	}

	p := &MinerUProvider{
		baseURL:      base,
		apiKey:       strings.TrimSpace(config.MinerUAPIKey),
		tier:         tier,
		timeout:      timeout,
		pollInterval: mineruPollInterval,
		retryWait:    time.Second,
		maxResponse:  mineruMaxResponse,
	}
	p.httpClient = &http.Client{
		// Per-request deadlines come from the overall context.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			// Never forward the API key to a different origin.
			if !sameOrigin(req.URL, p.baseURL) {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
	return p, nil
}

// redactURL removes userinfo, query and fragment (which may carry
// credentials or tokens) from a URL so it is safe to log.
func redactURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "<invalid url>"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

// validateMinerUURL checks that base is an absolute http(s) URL with a host,
// an in-range port and no query string or fragment, since API paths are
// appended to it verbatim. Its errors never include the URL or its parts.
func validateMinerUURL(base string) error {
	u, err := url.Parse(base)
	if err != nil {
		return errors.New("not a valid URL")
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return errors.New("scheme must be http or https")
	}
	if u.Opaque != "" || u.Hostname() == "" {
		return errors.New("missing host")
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return errors.New("port out of range")
		}
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(base, "#") {
		return errors.New("must not contain a query string or fragment")
	}
	return nil
}

func normalizeMinerUURL(raw string) string {
	s := strings.TrimRight(strings.TrimSpace(raw), "/")
	s = strings.TrimSuffix(s, "/v1")
	return strings.TrimRight(s, "/")
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}

func sameOrigin(u *url.URL, base string) bool {
	b, err := url.Parse(base)
	if err != nil || u == nil {
		return false
	}
	return strings.EqualFold(u.Scheme, b.Scheme) &&
		strings.EqualFold(u.Hostname(), b.Hostname()) &&
		effectivePort(u) == effectivePort(b)
}

// describeMinerUError extracts a readable message from an error body.
func describeMinerUError(body []byte) string {
	var env struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && len(env.Error) > 0 && string(env.Error) != "null" {
		return describeErrorValue(env.Error)
	}
	s := strings.TrimSpace(string(body))
	if len(s) > mineruMaxErrBody {
		s = s[:mineruMaxErrBody] + "..."
	}
	return s
}

func describeErrorValue(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil && (e.Code != "" || e.Message != "") {
		if e.Code != "" && e.Message != "" {
			return e.Code + ": " + e.Message
		}
		return e.Code + e.Message
	}
	return string(raw)
}

// mineruHTTPError is a non-2xx response from the MinerU API.
type mineruHTTPError struct {
	stage  string
	status int
	msg    string
}

func (e *mineruHTTPError) Error() string {
	return fmt.Sprintf("mineru %s: HTTP %d: %s", e.stage, e.status, e.msg)
}

// transient reports whether retrying the same request may succeed.
func (e *mineruHTTPError) transient() bool {
	return e.status >= 500 || e.status == http.StatusTooManyRequests
}

// request performs an HTTP call. Only idempotent calls pass retry=true.
// An error body in a 2xx response is also treated as failure.
func (p *MinerUProvider) request(ctx context.Context, stage, method, target string, header map[string]string, body []byte, auth, retry bool) ([]byte, error) {
	attempts := 1
	if retry {
		attempts = 3
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("mineru %s: %w", stage, ctx.Err())
			case <-time.After(p.retryWait):
			}
		}
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, rdr)
		if err != nil {
			return nil, fmt.Errorf("mineru %s: %w", stage, err)
		}
		for k, v := range header {
			req.Header.Set(k, v)
		}
		if auth && p.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("mineru %s: request failed: %w", stage, err)
			if ctx.Err() != nil {
				return nil, lastErr
			}
			continue
		}
		limit := p.maxResponse
		if resp.StatusCode >= 400 {
			limit = mineruMaxErrRead
		}
		// Read one byte past the limit to tell "exactly at" from "over".
		data, rerr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		resp.Body.Close()
		if rerr != nil {
			lastErr = fmt.Errorf("mineru %s: reading response: %w", stage, rerr)
			if ctx.Err() != nil {
				return nil, lastErr
			}
			continue
		}
		if int64(len(data)) > limit {
			if resp.StatusCode < 400 {
				return nil, fmt.Errorf("mineru %s: response body exceeds %d bytes", stage, limit)
			}
			data = data[:limit]
		}
		if resp.StatusCode >= 500 && i < attempts-1 {
			lastErr = &mineruHTTPError{stage: stage, status: resp.StatusCode, msg: describeMinerUError(data)}
			continue
		}
		if resp.StatusCode >= 400 {
			return nil, &mineruHTTPError{stage: stage, status: resp.StatusCode, msg: describeMinerUError(data)}
		}
		var env struct {
			Error json.RawMessage `json:"error"`
		}
		if len(bytes.TrimSpace(data)) > 0 && json.Unmarshal(data, &env) == nil && len(env.Error) > 0 && string(env.Error) != "null" {
			return nil, fmt.Errorf("mineru %s: %s", stage, describeErrorValue(env.Error))
		}
		return data, nil
	}
	return nil, lastErr
}

type mineruFile struct {
	ID string `json:"id"`
}

type mineruOutputFile struct {
	FileID string `json:"file_id"`
}

type mineruUpload struct {
	ID            string            `json:"id"`
	Status        string            `json:"status"`
	UploadURL     string            `json:"upload_url"`
	UploadMethod  string            `json:"upload_method"`
	UploadHeaders map[string]string `json:"upload_headers"`
	File          *mineruFile       `json:"file"`
}

type mineruJobFile struct {
	FileID      string          `json:"file_id"`
	Status      string          `json:"status"`
	Error       json.RawMessage `json:"error"`
	OutputFiles struct {
		StructuredContent *mineruOutputFile `json:"structured_content"`
	} `json:"output_files"`
}

type mineruJob struct {
	JobID  string          `json:"job_id"`
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Files  []mineruJobFile `json:"files"`
	Error  json.RawMessage `json:"error"`
}

func detectMinerUMedia(content []byte) (filename, mime string, err error) {
	switch {
	case bytes.HasPrefix(content, []byte("%PDF")):
		return "document.pdf", "application/pdf", nil
	case bytes.HasPrefix(content, []byte{0xFF, 0xD8, 0xFF}):
		return "page.jpg", "image/jpeg", nil
	case bytes.HasPrefix(content, []byte{0x89, 0x50, 0x4E, 0x47}):
		return "page.png", "image/png", nil
	case bytes.HasPrefix(content, []byte("II*\x00")), bytes.HasPrefix(content, []byte("MM\x00*")):
		return "page.tiff", "image/tiff", nil
	case len(content) >= 12 && bytes.HasPrefix(content, []byte("RIFF")) && bytes.Equal(content[8:12], []byte("WEBP")):
		return "page.webp", "image/webp", nil
	}
	n := len(content)
	if n > 8 {
		n = 8
	}
	return "", "", fmt.Errorf("mineru: unsupported input format (%d bytes, starts with % x; supported: PDF, JPEG, PNG, TIFF, WebP)", len(content), content[:n])
}

func (p *MinerUProvider) resolveUploadURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.IsAbs() {
		return u.String(), nil
	}
	// Match MinerU's own client: a rooted path is appended to the base URL,
	// keeping any path prefix of MINERU_URL.
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return p.baseURL + raw, nil
	}
	b, err := url.Parse(p.baseURL + "/")
	if err != nil {
		return "", err
	}
	return b.ResolveReference(u).String(), nil
}

// ProcessImage sends the document (PDF or image bytes) to MinerU and returns the extracted text.
func (p *MinerUProvider) ProcessImage(ctx context.Context, imageContent []byte, pageNumber int) (*OCRResult, error) {
	logger := log.WithFields(logrus.Fields{
		"provider": "mineru",
		"url":      redactURL(p.baseURL),
		"page":     pageNumber,
	})
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	fileID, err := p.uploadFile(ctx, imageContent, logger)
	if err != nil {
		return nil, err
	}

	jobID, err := p.createJob(ctx, fileID)
	if err != nil {
		return nil, err
	}
	logger.WithField("job_id", jobID).Debug("MinerU parse job created")

	outID, err := p.waitForJob(ctx, jobID)
	if err != nil {
		return nil, err
	}

	data, err := p.request(ctx, "download", http.MethodGet, p.baseURL+"/v1/files/"+url.PathEscape(outID)+"/content", nil, nil, true, true)
	if err != nil {
		return nil, err
	}
	text, pages, err := assembleMinerUText(data)
	if err != nil {
		return nil, fmt.Errorf("mineru decode: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"job_id":   jobID,
		"pages":    pages,
		"chars":    len(text),
		"duration": time.Since(start).String(),
	}).Info("MinerU processing complete")

	return &OCRResult{
		Text: text,
		Metadata: map[string]string{
			"provider":   "mineru",
			"tier":       p.tier,
			"job_id":     jobID,
			"page_count": strconv.Itoa(pages),
		},
	}, nil
}

func (p *MinerUProvider) uploadFile(ctx context.Context, content []byte, logger *logrus.Entry) (string, error) {
	filename, mime, err := detectMinerUMedia(content)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	reqBody, err := json.Marshal(map[string]interface{}{
		"filename":  filename,
		"bytes":     len(content),
		"mime_type": mime,
		"purpose":   "parse",
		"sha256sum": hex.EncodeToString(sum[:]),
	})
	if err != nil {
		return "", fmt.Errorf("mineru upload: %w", err)
	}
	data, err := p.request(ctx, "upload", http.MethodPost, p.baseURL+"/v1/uploads", map[string]string{"Content-Type": "application/json"}, reqBody, true, false)
	if err != nil {
		return "", err
	}
	var up mineruUpload
	if err := json.Unmarshal(data, &up); err != nil {
		return "", fmt.Errorf("mineru upload: invalid response: %w", err)
	}
	if up.Status == "completed" && up.File != nil && up.File.ID != "" {
		logger.Debug("MinerU already has this file (deduplicated)")
		return up.File.ID, nil
	}
	if up.ID == "" || up.UploadURL == "" {
		return "", fmt.Errorf("mineru upload: unexpected response (status %q, no upload_url)", up.Status)
	}
	target, err := p.resolveUploadURL(up.UploadURL)
	if err != nil {
		return "", fmt.Errorf("mineru upload: bad upload_url: %w", err)
	}
	method := strings.ToUpper(up.UploadMethod)
	if method == "" {
		method = http.MethodPut
	}
	tu, _ := url.Parse(target)
	sendKey := sameOrigin(tu, p.baseURL)
	if _, err := p.request(ctx, "upload", method, target, up.UploadHeaders, content, sendKey, true); err != nil {
		return "", err
	}

	data, err = p.request(ctx, "upload complete", http.MethodPost, p.baseURL+"/v1/uploads/"+url.PathEscape(up.ID)+"/complete", nil, nil, true, false)
	if err != nil {
		return "", err
	}
	var done mineruUpload
	if err := json.Unmarshal(data, &done); err != nil {
		return "", fmt.Errorf("mineru upload complete: invalid response: %w", err)
	}
	if done.File == nil || done.File.ID == "" {
		return "", fmt.Errorf("mineru upload complete: no file id in response (status %q)", done.Status)
	}
	return done.File.ID, nil
}

func (p *MinerUProvider) createJob(ctx context.Context, fileID string) (string, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"files": []interface{}{
			map[string]interface{}{"source": map[string]string{"type": "file_id", "file_id": fileID}},
		},
		"output_formats": []string{"structured_content"},
		"tier":           p.tier,
	})
	if err != nil {
		return "", fmt.Errorf("mineru create job: %w", err)
	}
	// Not retried: a retry after a timeout could create a duplicate job.
	data, err := p.request(ctx, "create job", http.MethodPost, p.baseURL+"/v1/parse/jobs", map[string]string{"Content-Type": "application/json"}, reqBody, true, false)
	if err != nil {
		return "", err
	}
	var job mineruJob
	if err := json.Unmarshal(data, &job); err != nil {
		return "", fmt.Errorf("mineru create job: invalid response: %w", err)
	}
	id := job.JobID
	if id == "" {
		id = job.ID
	}
	if id == "" {
		return "", fmt.Errorf("mineru create job: response has no job_id")
	}
	return id, nil
}

// waitForJob polls until the job is terminal and returns the structured_content file id.
// If it gives up before the job settles (timeout, caller cancellation or
// repeated poll failures), it asks MinerU to cancel the job so an abandoned
// parse does not keep the GPU busy ahead of later work.
// Transport errors, 5xx and 429 are retried up to three times in a row; any
// other 4xx (job_not_found, 401, 403) fails at once, as polling again cannot help.
func (p *MinerUProvider) waitForJob(ctx context.Context, jobID string) (outID string, err error) {
	target := p.baseURL + "/v1/parse/jobs/" + url.PathEscape(jobID)
	// settled is set once the server reported a terminal (or unknown) job
	// status or rejected the poll outright; a cancel would be pointless then.
	settled := false
	defer func() {
		if err != nil && !settled {
			p.cancelJob(ctx, jobID)
		}
	}()
	consecutiveErrs := 0
	for {
		data, err := p.request(ctx, "poll", http.MethodGet, target, nil, nil, true, false)
		if err != nil {
			if ctx.Err() != nil {
				return "", err
			}
			var httpErr *mineruHTTPError
			if errors.As(err, &httpErr) && !httpErr.transient() {
				settled = true
				return "", err
			}
			consecutiveErrs++
			if consecutiveErrs >= 3 {
				return "", err
			}
		} else {
			consecutiveErrs = 0
			var job mineruJob
			if err := json.Unmarshal(data, &job); err != nil {
				return "", fmt.Errorf("mineru poll: invalid response: %w", err)
			}
			if job.Status != "queued" && job.Status != "running" {
				settled = true
			}
			switch job.Status {
			case "queued", "running":
			case "completed":
				if len(job.Files) == 0 {
					return "", fmt.Errorf("mineru poll: job %s completed without files", jobID)
				}
				f := job.Files[0]
				if f.Status != "completed" {
					return "", fmt.Errorf("mineru job %s: file status %q%s", jobID, f.Status, errSuffix(f.Error))
				}
				if f.OutputFiles.StructuredContent == nil || f.OutputFiles.StructuredContent.FileID == "" {
					return "", fmt.Errorf("mineru job %s: completed but no structured_content output", jobID)
				}
				return f.OutputFiles.StructuredContent.FileID, nil
			case "failed", "canceled", "partial":
				msg := fmt.Sprintf("mineru job %s: status %q", jobID, job.Status)
				if len(job.Files) > 0 {
					msg += fmt.Sprintf(" (file status %q%s)", job.Files[0].Status, errSuffix(job.Files[0].Error))
				}
				msg += errSuffix(job.Error)
				return "", errors.New(msg)
			default:
				return "", fmt.Errorf("mineru job %s: unexpected status %q%s", jobID, job.Status, errSuffix(job.Error))
			}
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("mineru poll: %w", ctx.Err())
		case <-time.After(p.pollInterval):
		}
	}
}

// cancelJob asks MinerU to cancel a queued or running job. It is best effort:
// it uses a short, fresh deadline (the caller's context has typically expired
// already) and only logs failures, so callers still see the original error.
func (p *MinerUProvider) cancelJob(ctx context.Context, jobID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mineruCancelTimeout)
	defer cancel()
	logger := log.WithFields(logrus.Fields{"provider": "mineru", "job_id": jobID})
	_, err := p.request(ctx, "cancel job", http.MethodDelete, p.baseURL+"/v1/parse/jobs/"+url.PathEscape(jobID), nil, nil, true, false)
	if err != nil {
		logger.WithError(err).Warn("Could not cancel abandoned MinerU job")
		return
	}
	logger.Info("Cancelled abandoned MinerU job")
}

func errSuffix(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return ": " + describeErrorValue(raw)
}

// ---- structured_content text assembly ----

type mineruBlock struct {
	Type      string
	Level     int
	Content   string
	Captions  []string
	Footnotes []string
}

// UnmarshalJSON decodes a block tolerantly: a field with an unexpected JSON
// type is skipped rather than failing the whole document.
func (b *mineruBlock) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		log.WithField("provider", "mineru").Debug("Skipping non-object block")
		return nil
	}
	if t, ok := flexText(raw["type"]); ok {
		b.Type = strings.TrimSpace(t)
	}
	b.Level = flexLevel(raw["level"])
	if c, ok := flexText(raw["content"]); ok {
		b.Content = c
	} else if len(raw["content"]) > 0 && string(raw["content"]) != "null" {
		log.WithField("provider", "mineru").Debugf("Skipping block content of unexpected type: %.60s", raw["content"])
	}
	b.Captions = flexNotes(raw["captions"])
	b.Footnotes = flexNotes(raw["footnotes"])
	return nil
}

// flexLevel accepts a number or numeric string; anything else yields 0.
func flexLevel(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int(f)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return int(f)
		}
	}
	return 0
}

// flexText converts a string, an array of strings/objects, or an object with
// a content/text field into text. ok is false for anything else (incl. null).
func flexText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		var parts []string
		for _, el := range arr {
			if t, ok := flexText(el); ok && strings.TrimSpace(t) != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n"), true
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		for _, k := range []string{"content", "text"} {
			if v, ok := obj[k]; ok {
				if t, ok := flexText(v); ok {
					return t, true
				}
			}
		}
	}
	return "", false
}

// flexNotes decodes caption/footnote entries: an array of strings/objects,
// or a single string/object.
func flexNotes(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		var out []string
		for _, el := range arr {
			if t, ok := flexText(el); ok {
				out = append(out, t)
			}
		}
		return out
	}
	if t, ok := flexText(raw); ok {
		return []string{t}
	}
	return nil
}

type mineruStructured struct {
	Pages []struct {
		Blocks []mineruBlock `json:"blocks"`
	} `json:"pages"`
}

var (
	mineruMDImageData = regexp.MustCompile(`!\[[^\]]*\]\(\s*data:[^)]*\)`)
	mineruImgTag      = regexp.MustCompile(`(?is)<img\b[^>]*>?`)
	mineruDataURI     = regexp.MustCompile(`data:[A-Za-z0-9.+/-]*(?:;[A-Za-z0-9=.+-]+)*;base64,[A-Za-z0-9+/=_-]*`)
	mineruMultiBlank  = regexp.MustCompile(`\n{3,}`)
	mineruAnyTag      = regexp.MustCompile(`(?s)<[^>]*>?`)
)

func assembleMinerUText(data []byte) (string, int, error) {
	var doc mineruStructured
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", 0, err
	}
	var parts []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}
	for _, page := range doc.Pages {
		for _, b := range page.Blocks {
			content := b.Content
			if looksLikeHTMLTable(content) {
				// The HTML parser has already decoded entities.
				content = stripBinaryImages(htmlTableToRows(content))
			} else {
				content = html.UnescapeString(stripBinaryImages(content))
			}
			if strings.TrimSpace(content) != "" && (b.Type == "doc_title" || b.Type == "paragraph_title") {
				level := b.Level
				if level <= 0 {
					level = 2
				}
				if level > 6 {
					level = 6
				}
				content = strings.Repeat("#", level) + " " + strings.TrimSpace(content)
			}
			add(content)
			for _, c := range b.Captions {
				add(html.UnescapeString(stripBinaryImages(c)))
			}
			for _, f := range b.Footnotes {
				add(html.UnescapeString(stripBinaryImages(f)))
			}
		}
	}
	text := strings.Join(parts, "\n\n")
	return mineruMultiBlank.ReplaceAllString(text, "\n\n"), len(doc.Pages), nil
}

func stripBinaryImages(s string) string {
	s = mineruMDImageData.ReplaceAllString(s, "")
	s = mineruImgTag.ReplaceAllString(s, "")
	return mineruDataURI.ReplaceAllString(s, "")
}

func looksLikeHTMLTable(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "<table") || strings.Contains(l, "<tr")
}

// htmlTableToRows converts an HTML table to one "| a | b |" line per row.
// Text before, between and after table elements is kept, in order, as lines.
func htmlTableToRows(s string) string {
	root, err := nethtml.Parse(strings.NewReader(s))
	if err != nil {
		return strings.Join(strings.Fields(mineruAnyTag.ReplaceAllString(s, " ")), " ")
	}
	var lines []string
	var buf strings.Builder
	flush := func() {
		if t := strings.Join(strings.Fields(buf.String()), " "); t != "" {
			lines = append(lines, t)
		}
		buf.Reset()
	}
	var rows func(n *nethtml.Node) int
	rows = func(n *nethtml.Node) int {
		count := 0
		if n.Type == nethtml.ElementNode && n.Data == "tr" {
			var cells []string
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == nethtml.ElementNode && (c.Data == "td" || c.Data == "th") {
					cells = append(cells, strings.Join(strings.Fields(nodeText(c)), " "))
				}
			}
			if len(cells) > 0 {
				lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
				count++
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			count += rows(c)
		}
		return count
	}
	var walk func(n *nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode {
			switch n.Data {
			case "script", "style", "img":
				return
			case "table":
				flush()
				if rows(n) == 0 {
					if t := strings.Join(strings.Fields(nodeText(n)), " "); t != "" {
						lines = append(lines, t)
					}
				}
				return
			}
		}
		if n.Type == nethtml.TextNode {
			buf.WriteString(n.Data)
			buf.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	flush()
	return strings.Join(lines, "\n")
}

func nodeText(n *nethtml.Node) string {
	var sb strings.Builder
	var walk func(n *nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "img") {
			return
		}
		if n.Type == nethtml.TextNode {
			sb.WriteString(n.Data)
			sb.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}
