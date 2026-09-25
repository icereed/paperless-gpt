package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"paperless-gpt/extension"
)

// vocabularyCandidates returns the values offered to the LLM for a field:
// those of the registered Vocabulary if it restricts the field, otherwise the
// given paperless-ngx values unchanged, plus any hints the Vocabulary has.
func vocabularyCandidates(ctx context.Context, req extension.CandidatesRequest, existing []string) ([]string, map[string]string, error) {
	v := extension.VocabularyFor(req.Field)
	if v == nil {
		return existing, nil, nil
	}
	candidates, err := v.Candidates(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("%s vocabulary unavailable: %w", req.Field, err)
	}
	if candidates.Unrestricted {
		return existing, candidates.Hints, nil
	}
	return candidates.Values, candidates.Hints, nil
}

// resolveVocabularyValue checks a proposed value against the Vocabulary
// registered for a field. Without a Vocabulary every value is accepted as-is.
// A Vocabulary error is returned so callers fail closed instead of writing an
// unchecked value.
func resolveVocabularyValue(ctx context.Context, req extension.ResolveRequest) (extension.Resolution, error) {
	v := extension.VocabularyFor(req.Field)
	if v == nil {
		return extension.Resolution{Accepted: true, Value: req.Proposed}, nil
	}
	resolution, err := v.Resolve(ctx, req)
	if err != nil {
		return extension.Resolution{}, fmt.Errorf("%s vocabulary could not check %q: %w", req.Field, req.Proposed, err)
	}
	return resolution, nil
}

// templateUses reports whether a prompt template refers to a data field.
func templateUses(tmpl *template.Template, field string) bool {
	if tmpl == nil || tmpl.Tree == nil || tmpl.Tree.Root == nil {
		return false
	}
	return strings.Contains(tmpl.Tree.Root.String(), "."+field)
}

// documentTypeHintsBlock renders hints for prompts that do not place them
// themselves.
func documentTypeHintsBlock(hints map[string]string) string {
	names := make([]string, 0, len(hints))
	for name := range hints {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("\n\nUse these descriptions to tell the document types apart:\n<document_type_descriptions>\n")
	for _, name := range names {
		fmt.Fprintf(&b, "- %s: %s\n", name, hints[name])
	}
	b.WriteString("</document_type_descriptions>\n")
	return b.String()
}
