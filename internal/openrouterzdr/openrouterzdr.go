// Package openrouterzdr implements opt-in Zero Data Retention (ZDR)
// enforcement for OpenRouter.
//
// When OPENROUTER_ENFORCE_ZDR is set to "true", every request sent to
// openrouter.ai through any OpenAI-compatible client in this codebase (the
// main/metadata LLM, the Vision LLM, and the LLM-based OCR provider) gets
//
//	{"provider": {"data_collection": "deny", "zdr": true}}
//
// merged into its JSON body. This tells OpenRouter to route only to upstream
// model providers that offer a Zero Data Retention guarantee (no logging, no
// training on the request/response). See:
// https://openrouter.ai/docs/guides/features/zdr
//
// # Why this exists
//
// Documents processed by paperless-gpt often contain personal or sensitive
// data (letters, invoices, medical correspondence). For deployments that
// route through OpenRouter, there was previously no way to guarantee that
// the upstream model provider doesn't retain that content, short of manually
// verifying every model's provider list before each config change. Enforcing
// ZDR at the HTTP transport level makes the guarantee hold even if an
// operator later switches models or misconfigures something.
//
// # Trade-off (why this is opt-in, not the default)
//
// Restricting routing to ZDR-capable providers can shrink the pool of
// upstream providers for a given model. If a model has only one upstream
// provider willing to offer ZDR, that provider becoming rate-limited or
// unavailable produces a 429/5xx with no fallback. This is expected once
// enabled, not a bug — but it's a real behavior change that shouldn't be
// forced on deployments that haven't opted in. Check
// https://openrouter.ai/<model>/providers before relying on a
// single-provider model with this flag on.
//
// # Scope
//
// Only requests whose destination host is openrouter.ai (or a subdomain of
// it) are affected. Every other endpoint (OpenAI itself, Azure OpenAI,
// self-hosted OpenAI-compatible gateways, etc.) is untouched, so this flag is
// safe to leave set even if OPENAI_BASE_URL later changes to a
// non-OpenRouter endpoint.
//
// # Why this is a separate internal package
//
// paperless-gpt builds three independent OpenAI-compatible clients: the main
// metadata LLM and the Vision LLM (both constructed in package main), and
// the LLM-based OCR provider (constructed in package ocr). All three need
// the same transport wrapping to make the ZDR guarantee hold regardless of
// which client an operator happens to configure with OPENAI_BASE_URL
// pointed at OpenRouter. Living in package main would make it unimportable
// from package ocr; this package is shared by both.
package openrouterzdr

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
)

// Enforced reports whether OPENROUTER_ENFORCE_ZDR is enabled.
// Read once per RoundTrip rather than cached at startup so a change is
// picked up without a restart in tests; the cost is one os.Getenv call per
// outgoing OpenRouter request, which is negligible next to the network call
// it wraps.
func Enforced() bool {
	return strings.ToLower(os.Getenv("OPENROUTER_ENFORCE_ZDR")) == "true"
}

// transport wraps an http.RoundTripper and, when enabled, injects
// OpenRouter's Zero Data Retention provider preference into every request
// body sent to openrouter.ai. Requests to any other host, or all requests
// when disabled, pass through unmodified.
type transport struct {
	next http.RoundTripper
}

// NewTransport wraps next so that, when OPENROUTER_ENFORCE_ZDR=true, requests
// to openrouter.ai get {"provider": {"data_collection": "deny", "zdr": true}}
// merged into their JSON body. Pass http.DefaultTransport (or any other
// RoundTripper) as next.
func NewTransport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &transport{next: next}
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !Enforced() || req.Body == nil || req.URL == nil || !isOpenRouterHost(req.URL.Hostname()) {
		return t.next.RoundTrip(req)
	}

	bodyBytes, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		// Body already partially consumed and unreadable: fail closed by
		// returning the error rather than silently dropping the ZDR
		// guarantee. Callers see a clear transport error instead of a
		// request that quietly lost its data-retention preference.
		req.Body = io.NopCloser(bytes.NewReader(nil))
		return nil, err
	}

	newBody, injectErr := injectZDRPreference(bodyBytes)
	if injectErr != nil {
		// Malformed/non-JSON body: send the original bytes unchanged rather
		// than corrupting the request. This can only happen for a non-JSON
		// payload, which the chat-completions request path in this codebase
		// does not produce.
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
		return t.next.RoundTrip(req)
	}

	req.Body = io.NopCloser(bytes.NewReader(newBody))
	req.ContentLength = int64(len(newBody))
	// Some http.RoundTrippers (notably http.Transport) retry a request after
	// a failed connection reuse by calling GetBody to obtain a fresh Body
	// reader, rather than reusing req.Body directly. Without updating
	// GetBody here, such a retry would read the pre-injection bytes and
	// silently skip the ZDR provider preference on that attempt. See
	// https://github.com/golang/go/blob/go1.25.5/src/net/http/transport.go
	// for the retry path that consults GetBody.
	if req.GetBody != nil {
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(newBody)), nil
		}
	}
	return t.next.RoundTrip(req)
}

// isOpenRouterHost reports whether host (as returned by url.URL.Hostname(),
// i.e. without a port) is openrouter.ai or a subdomain of it. Comparison is
// case-insensitive per RFC 4343. Using an exact/suffix label match instead of
// a raw substring check avoids both false negatives (e.g. "OpenRouter.ai",
// which strings.Contains would miss due to case) and false positives (e.g.
// "openrouter.ai.attacker.example", which a plain substring check would
// wrongly match).
func isOpenRouterHost(host string) bool {
	host = strings.ToLower(host)
	return host == "openrouter.ai" || strings.HasSuffix(host, ".openrouter.ai")
}

// injectZDRPreference parses body as a JSON object and merges in
// "provider": {"data_collection": "deny", "zdr": true}, preserving any
// existing "provider" object's other fields (e.g. an operator-configured
// "order" or "allow_fallbacks").
//
// Both fields are set because they control related but distinct things:
// data_collection excludes providers that store/train on data non-transiently,
// while zdr additionally restricts routing to providers with a documented
// Zero Data Retention policy. Setting only data_collection would still allow
// routing to a provider that doesn't collect data long-term but also doesn't
// carry a formal ZDR guarantee. See
// https://openrouter.ai/docs/guides/features/zdr
func injectZDRPreference(body []byte) ([]byte, error) {
	var payload map[string]interface{}
	// A plain json.Unmarshal into map[string]interface{} decodes all JSON
	// numbers as float64, which loses precision for integers outside
	// float64's 53-bit mantissa (e.g. a large "seed" value). Using
	// json.Decoder with UseNumber preserves those as json.Number (an
	// unparsed string form), which json.Marshal re-encodes as the original
	// numeric literal, so untouched fields round-trip byte-for-byte instead
	// of silently losing precision.
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}

	provider, _ := payload["provider"].(map[string]interface{})
	if provider == nil {
		provider = map[string]interface{}{}
	}
	provider["data_collection"] = "deny"
	provider["zdr"] = true
	payload["provider"] = provider

	return json.Marshal(payload)
}
