package ocr

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// LangChainGo v0.1.15 recognizes GPT-5, but not GPT-6, when omitting the
// temperature field. Its zero default therefore makes GPT-6 metadata and OCR
// requests fail with HTTP 400. Keep this workaround at the shared OpenAI
// client boundary until the dependency supports these models itself.
type gpt6Transport struct {
	base http.RoundTripper
}

func (t *gpt6Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/chat/completions") || req.Body == nil {
		return t.base.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, err
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) == nil {
		var model, effort string
		_ = json.Unmarshal(payload["model"], &model)
		_ = json.Unmarshal(payload["reasoning_effort"], &effort)
		isGPT6 := model == "gpt-6" || strings.HasPrefix(model, "gpt-6-") || strings.HasPrefix(model, "gpt-6.")
		if _, present := payload["temperature"]; present && isGPT6 && effort != "none" {
			delete(payload, "temperature")
			body, err = json.Marshal(payload)
			if err != nil {
				return nil, err
			}
		}
	}

	// Clone the request and give retries/redirects the rewritten body too.
	clone := req.Clone(req.Context())
	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.ContentLength = int64(len(body))
	clone.Header.Del("Content-Length")
	clone.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	return t.base.RoundTrip(clone)
}
