// Package extension is the seam through which optional modules extend
// paperless-gpt without patching its core.
//
// An extension registers itself from an init function; paperless-gpt links it
// in with a blank import. With nothing registered, every hook is a no-op and
// paperless-gpt behaves exactly as without this package.
//
// The first hook is Vocabulary: a controlled set of values a Suggestion may
// carry for one metadata field, enforced both when a Suggestion is generated
// and when it is applied.
package extension

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

// Field identifies a metadata field of a Suggestion.
type Field string

// FieldCorrespondent is the correspondent of a document.
const FieldCorrespondent Field = "correspondent"

// CandidatesRequest describes the document a Suggestion is generated for,
// so a Vocabulary can narrow its candidates to the document at hand.
type CandidatesRequest struct {
	Field      Field
	DocumentID int
	Title      string
	Content    string
}

// Candidates is a Vocabulary's answer to CandidatesRequest.
type Candidates struct {
	// Unrestricted means the Vocabulary does not constrain the field right
	// now (e.g. no value list is configured yet): the paperless-ngx values
	// are used and every value is accepted.
	Unrestricted bool
	// Values are offered to the LLM instead of the paperless-ngx values.
	Values []string
}

// ResolveRequest asks a Vocabulary to decide on one proposed value.
type ResolveRequest struct {
	Field      Field
	DocumentID int // 0 when the value is not tied to a document
	Proposed   string
	// Stage says where the value comes from, so a vocabulary can tell an
	// invented LLM answer from a value a person entered.
	Stage Stage
}

// Stage is the point in the suggestion flow where a value is checked.
type Stage string

const (
	// StageGenerate: the LLM just proposed the value.
	StageGenerate Stage = "generate"
	// StageApply: the value is about to be written to paperless-ngx; it may
	// have been edited during Review or sent directly to the API.
	StageApply Stage = "apply"
)

// Resolution is a Vocabulary's decision on one proposed value.
type Resolution struct {
	// Accepted reports whether the proposed value may be used.
	Accepted bool
	// Value is the canonical form to use when Accepted. An accepted empty
	// Value means "no value" (e.g. the LLM answered "Unknown").
	Value string
	// Reason explains the decision for logs and the UI. Required when the
	// value is rejected.
	Reason string
}

// Vocabulary constrains the values a Suggestion may carry for one field.
//
// Implementations must be safe for concurrent use. An error means the
// vocabulary cannot decide (e.g. its source never loaded); callers fail
// closed rather than falling back to unconstrained behaviour.
type Vocabulary interface {
	// Candidates returns the values offered to the LLM for this field.
	Candidates(ctx context.Context, req CandidatesRequest) (Candidates, error)
	// Resolve maps a proposed value to its canonical form or rejects it.
	Resolve(ctx context.Context, req ResolveRequest) (Resolution, error)
}

// EnvVar documents one environment variable an extension reads, so it shows
// up in the Active Configuration view next to the core settings.
type EnvVar struct {
	Name        string
	Category    string
	Secret      bool
	Default     string
	Description string
}

// Extension is an optional module linked into paperless-gpt.
type Extension interface {
	// Name identifies the extension in logs.
	Name() string
	// EnvVars lists the environment variables the extension reads.
	EnvVars() []EnvVar
	// Start runs once at startup, after logging is configured and before
	// any document is processed. An error aborts startup.
	Start(ctx context.Context) error
}

// HTTPExtension is an Extension that serves its own HTTP endpoints (e.g. a
// JSON API for its UI). paperless-gpt mounts Handler at
// "/extensions/<Name()>/" and strips that prefix; Name must therefore be
// URL-safe. UI pages are contributed at build time through the web app's
// extension entry point, not over HTTP.
type HTTPExtension interface {
	Extension
	Handler() http.Handler
}

var (
	mu           sync.RWMutex
	extensions   []Extension
	vocabularies = map[Field]Vocabulary{}
)

// Register adds an extension. It is meant to be called from init.
func Register(e Extension) {
	mu.Lock()
	defer mu.Unlock()
	extensions = append(extensions, e)
}

// RegisterVocabulary installs the Vocabulary for a field. Registering a
// second Vocabulary for the same field panics: two competing sources of
// truth would make enforcement ambiguous. A nil Vocabulary panics too, as it
// would silently leave the field unconstrained.
func RegisterVocabulary(field Field, v Vocabulary) {
	if v == nil {
		panic(fmt.Sprintf("extension: nil vocabulary registered for field %q", field))
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := vocabularies[field]; exists {
		panic(fmt.Sprintf("extension: vocabulary for field %q registered twice", field))
	}
	vocabularies[field] = v
}

// VocabularyFor returns the Vocabulary registered for a field, or nil.
func VocabularyFor(field Field) Vocabulary {
	mu.RLock()
	defer mu.RUnlock()
	return vocabularies[field]
}

// Extensions returns the registered extensions in registration order.
func Extensions() []Extension {
	mu.RLock()
	defer mu.RUnlock()
	return append([]Extension(nil), extensions...)
}

// HTTPExtensions returns the registered extensions that serve HTTP.
func HTTPExtensions() []HTTPExtension {
	var result []HTTPExtension
	for _, e := range Extensions() {
		if h, ok := e.(HTTPExtension); ok {
			result = append(result, h)
		}
	}
	return result
}

// EnvVars returns the environment variables of all registered extensions.
func EnvVars() []EnvVar {
	var vars []EnvVar
	for _, e := range Extensions() {
		vars = append(vars, e.EnvVars()...)
	}
	return vars
}

// Start starts every registered extension in registration order and stops
// at the first error.
func Start(ctx context.Context) error {
	for _, e := range Extensions() {
		if err := e.Start(ctx); err != nil {
			return fmt.Errorf("extension %s: %w", e.Name(), err)
		}
	}
	return nil
}

// Reset removes all registrations. It exists for tests.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	extensions = nil
	vocabularies = map[Field]Vocabulary{}
}
