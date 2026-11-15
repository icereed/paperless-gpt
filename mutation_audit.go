package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const maxMutationAuditBytes = 64 << 20

var (
	mutationAuditMu      sync.Mutex
	documentIDInPath     = regexp.MustCompile(`/api/documents/([0-9]+)(?:/|$)`)
	mutationAuditFileErr = errors.New("OCR_AUDIT_PATH is not configured")
)

type mutationAuditEvent struct {
	SchemaVersion int               `json:"schema_version"`
	EventID       string            `json:"event_id"`
	Time          time.Time         `json:"time"`
	Sequence      int64             `json:"sequence"`
	Actor         string            `json:"actor"`
	Component     string            `json:"component"`
	CorrelationID string            `json:"correlation_id,omitempty"`
	Target        string            `json:"target"`
	Action        string            `json:"action"`
	Inputs        map[string]string `json:"inputs,omitempty"`
	CodeVersion   string            `json:"code_version"`
	ConfigVersion string            `json:"config_version"`
	EngineVersion string            `json:"engine_version,omitempty"`
	Reason        string            `json:"reason"`
	Phase         string            `json:"phase"`
	Outcome       string            `json:"outcome,omitempty"`
	Error         string            `json:"error,omitempty"`
	HTTPStatus    int               `json:"http_status,omitempty"`
}

func mutationAuditPath() string { return strings.TrimSpace(os.Getenv("OCR_AUDIT_PATH")) }

func (client *PaperlessClient) doAuditedRequest(req *http.Request) (*http.Response, string, error) {
	if !isPaperlessMutation(req.Method) {
		resp, err := client.HTTPClient.Do(req)
		return resp, "", err
	}

	auditPath := mutationAuditPath()
	if auditPath == "" {
		return nil, "", mutationAuditFileErr
	}
	eventID, err := newMutationAuditID()
	if err != nil {
		return nil, "", fmt.Errorf("create mutation audit event ID: %w", err)
	}
	outcomeID, err := newMutationAuditID()
	if err != nil {
		return nil, "", fmt.Errorf("create mutation audit outcome ID: %w", err)
	}
	action := paperlessMutationAction(req.Method, req.URL.Path)
	inputs, err := mutationInputs(req)
	if err != nil {
		return nil, "", fmt.Errorf("hash Paperless mutation inputs: %w", err)
	}
	intent := newMutationAuditEvent(eventID, req.URL.Path, action, inputs, "intent")
	intent.Reason = "paperless API mutation request"
	intent.Outcome = "intent_persisted"
	if err := appendMutationAudit(auditPath, &intent); err != nil {
		return nil, "", fmt.Errorf("persist Paperless mutation audit intent: %w", err)
	}

	resp, requestErr := client.HTTPClient.Do(req)
	outcome := newMutationAuditEvent(outcomeID, req.URL.Path, action, inputs, "outcome")
	outcome.CorrelationID = eventID
	outcome.Reason = "paperless API mutation request result"
	if requestErr != nil {
		outcome.Outcome = "transport_error"
		outcome.Error = "request failed before an HTTP response"
	} else {
		outcome.HTTPStatus = resp.StatusCode
		switch {
		case resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices:
			outcome.Outcome = "rejected"
			outcome.Error = "Paperless returned a non-success HTTP status"
		case strings.HasPrefix(req.URL.Path, "/api/") && strings.Contains(resp.Header.Get("Content-Type"), "text/html"):
			outcome.Outcome = "unconfirmed"
			outcome.Error = "Paperless returned HTML instead of an API response; mutation outcome is unknown"
		default:
			outcome.Outcome = "success"
		}
	}
	if auditErr := appendMutationAudit(auditPath, &outcome); auditErr != nil {
		if resp != nil {
			resp.Body.Close()
		}
		if requestErr != nil {
			return nil, eventID, fmt.Errorf("Paperless request failed and audit outcome could not be persisted: %w", auditErr)
		}
		return nil, eventID, fmt.Errorf("Paperless request returned HTTP %d but audit outcome could not be persisted: %w", resp.StatusCode, auditErr)
	}
	return resp, eventID, requestErr
}

func isPaperlessMutation(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func paperlessMutationAction(method, path string) string {
	method = strings.ToUpper(method)
	switch {
	case method == http.MethodPatch && strings.HasPrefix(path, "/api/documents/"):
		return "document_update"
	case method == http.MethodPost && strings.HasSuffix(path, "/update_version/"):
		return "document_version_upload"
	case method == http.MethodPost && strings.HasSuffix(path, "/post_document/"):
		return "document_upload"
	case method == http.MethodPost && strings.HasPrefix(path, "/api/tags/"):
		return "tag_create"
	case method == http.MethodDelete && strings.HasPrefix(path, "/api/documents/"):
		return "document_delete"
	default:
		return strings.ToLower(method) + "_request"
	}
}

func mutationInputs(req *http.Request) (map[string]string, error) {
	inputs := make(map[string]string, 2)
	if match := documentIDInPath.FindStringSubmatch(req.URL.Path); len(match) == 2 {
		inputs["document_id"] = match[1]
	}
	if req.Body != nil {
		bodyHash, err := mutationBodyHashForRequest(req)
		if err != nil {
			return nil, err
		}
		inputs["body_sha256"] = bodyHash
	}
	if len(inputs) == 0 {
		return nil, nil
	}
	return inputs, nil
}

func newMutationAuditEvent(eventID, target, action string, inputs map[string]string, phase string) mutationAuditEvent {
	provider := strings.TrimSpace(os.Getenv("OCR_PROVIDER"))
	if provider == "" {
		provider = "llm"
	}
	configVersion := provider
	engine := ""
	if provider == "ocrmypdf" {
		engine = strings.TrimSpace(os.Getenv("OCRMY_PDF_ENGINE"))
		if engine == "" {
			engine = "paddleocr"
		}
		language := strings.TrimSpace(os.Getenv("OCRMY_PDF_LANGUAGE"))
		if language == "" {
			language = "eng"
		}
		configVersion += "/" + engine + "/" + language
	}
	return mutationAuditEvent{
		SchemaVersion: 1,
		EventID:       eventID,
		Time:          time.Now().UTC(),
		Actor:         "paperless-gpt",
		Component:     "paperless-client",
		CorrelationID: eventID,
		Target:        target,
		Action:        action,
		Inputs:        inputs,
		CodeVersion:   version,
		ConfigVersion: configVersion,
		EngineVersion: strings.TrimSpace(os.Getenv("OCRMY_PDF_ENGINE_VERSION")),
		Phase:         phase,
		Outcome:       phase,
	}
}

func appendMutationAudit(path string, event *mutationAuditEvent) error {
	if strings.TrimSpace(path) == "" {
		return mutationAuditFileErr
	}
	mutationAuditMu.Lock()
	defer mutationAuditMu.Unlock()

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat audit file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("audit path must be a regular file")
	}
	file, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("open audit file: %w", err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat open audit file: %w", err)
	}
	if !stat.Mode().IsRegular() {
		return fmt.Errorf("audit path must be a regular file")
	}
	if stat.Size() > 0 {
		var tail [1]byte
		if _, err := file.ReadAt(tail[:], stat.Size()-1); err != nil {
			return fmt.Errorf("read audit record boundary: %w", err)
		}
		if tail[0] != '\n' {
			return fmt.Errorf("audit file ends with an incomplete record; operator reconciliation required")
		}
	}
	event.Sequence = stat.Size()
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode audit event: %w", err)
	}
	line = append(line, '\n')
	if stat.Size()+int64(len(line)) > maxMutationAuditBytes {
		return fmt.Errorf("audit file limit reached (%d bytes)", maxMutationAuditBytes)
	}
	written, err := file.Write(line)
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	if written != len(line) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("fsync audit event: %w", err)
	}
	return nil
}

func newMutationAuditID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func mutationBodyHash(body io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (client *PaperlessClient) recordMutationNoOp(target, action string, documentID int, _ string) error {
	path := mutationAuditPath()
	if path == "" {
		return mutationAuditFileErr
	}
	eventID, err := newMutationAuditID()
	if err != nil {
		return fmt.Errorf("create mutation audit event ID: %w", err)
	}
	inputs := map[string]string{"document_id": fmt.Sprint(documentID)}
	event := newMutationAuditEvent(eventID, target, action, inputs, "no_op")
	event.Outcome = "no_change"
	event.Reason = "no Paperless mutation was required"
	return appendMutationAudit(path, &event)
}

func (client *PaperlessClient) recordAsyncMutationOutcome(target, action, documentID, taskID, outcome, errorCode string) error {
	path := mutationAuditPath()
	if path == "" {
		return mutationAuditFileErr
	}
	eventID, err := newMutationAuditID()
	if err != nil {
		return fmt.Errorf("create mutation audit event ID: %w", err)
	}
	inputs := map[string]string{"document_id": documentID, "task_id": taskID}
	event := newMutationAuditEvent(eventID, target, action, inputs, "outcome")
	event.CorrelationID = taskID
	event.Outcome = outcome
	event.Error = errorCode
	event.Reason = "Paperless asynchronous mutation result"
	return appendMutationAudit(path, &event)
}

func mutationBodyHashForRequest(req *http.Request) (string, error) {
	if req.Body == nil {
		return "", nil
	}
	if req.GetBody == nil {
		return "", fmt.Errorf("request body cannot be replayed for hashing")
	}
	body, err := req.GetBody()
	if err != nil {
		return "", err
	}
	defer body.Close()
	return mutationBodyHash(body)
}
